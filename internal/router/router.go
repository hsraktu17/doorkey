// Package router runs a request over a Route: retries with backoff, per-attempt
// timeouts, and fallback to the next target.
//
// The one rule for streams: retries and fallback only happen before the first
// chunk is handed to the caller. Once a stream is returned, the router is
// committed to that provider; a later failure is the caller's to report.
package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
)

// Backoff defaults.
const (
	DefaultBaseDelay     = 200 * time.Millisecond
	DefaultMaxDelay      = 2 * time.Second
	DefaultMaxRetryAfter = 10 * time.Second
)

// Router holds the registered providers and the retry policy.
type Router struct {
	providers map[string]providers.Provider

	BaseDelay     time.Duration // first retry delay, doubled each retry
	MaxDelay      time.Duration // cap on the computed delay
	MaxRetryAfter time.Duration // a longer Retry-After falls back instead of waiting

	sleep  func(context.Context, time.Duration) error
	jitter func() float64 // in [0,1)
}

// New returns a Router over the given providers, keyed by Name().
func New(ps ...providers.Provider) *Router {
	m := make(map[string]providers.Provider, len(ps))
	for _, p := range ps {
		m[p.Name()] = p
	}
	return &Router{
		providers:     m,
		BaseDelay:     DefaultBaseDelay,
		MaxDelay:      DefaultMaxDelay,
		MaxRetryAfter: DefaultMaxRetryAfter,
		sleep:         sleepCtx,
		jitter:        rand.Float64,
	}
}

// Provider returns a registered provider by name.
func (rt *Router) Provider(name string) (providers.Provider, bool) {
	p, ok := rt.providers[name]
	return p, ok
}

// Attempt records one try against one target.
type Attempt struct {
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Status   int           `json:"status,omitempty"` // 0 for network errors
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration_ns"`
}

// Result says which target served the request and what was tried first.
type Result struct {
	Provider string // empty when every target failed
	Model    string
	Attempts []Attempt // failed attempts, then the successful one
}

// Complete runs a non-streaming request over the route.
func (rt *Router) Complete(ctx context.Context, route Route, req *api.ChatRequest) (*api.ChatResponse, Result, error) {
	var resp *api.ChatResponse
	res, release, err := rt.run(ctx, route, req, func(ctx context.Context, p providers.Provider, req *api.ChatRequest) (func(), error) {
		out, err := p.Complete(ctx, req)
		if err != nil {
			return nil, err
		}
		resp = out
		return nil, nil
	})
	if release != nil {
		release()
	}
	if err != nil {
		return nil, res, err
	}
	return resp, res, nil
}

// Stream runs a streaming request over the route. The returned stream has
// already produced its first chunk successfully; the caller must Close it.
func (rt *Router) Stream(ctx context.Context, route Route, req *api.ChatRequest) (providers.Stream, Result, error) {
	var out *committedStream
	res, release, err := rt.run(ctx, route, req, func(ctx context.Context, p providers.Provider, req *api.ChatRequest) (func(), error) {
		s, err := p.Stream(ctx, req)
		if err != nil {
			return nil, err
		}
		// Read the first chunk before committing, so a provider that fails
		// right away can still be retried or skipped.
		first, err := s.Recv()
		if err != nil {
			s.Close()
			if errors.Is(err, io.EOF) {
				err = &providers.Error{Provider: p.Name(), Status: http.StatusBadGateway, Message: "stream ended before any data"}
			}
			return nil, err
		}
		out = &committedStream{first: first, s: s}
		return func() { s.Close() }, nil
	})
	if err != nil {
		return nil, res, err
	}
	out.release = release
	return out, res, nil
}

// attemptFunc makes one call. On success it may return a cleanup func, used
// if the attempt is discarded after the fact (its timeout fired as it
// returned).
type attemptFunc func(ctx context.Context, p providers.Provider, req *api.ChatRequest) (cleanup func(), err error)

// run is the retry / fallback loop. On success it returns a release func
// that cancels the winning attempt's context; the caller must call it once
// done with the response.
func (rt *Router) run(ctx context.Context, route Route, req *api.ChatRequest, call attemptFunc) (Result, func(), error) {
	var res Result
	if err := rt.Validate(route); err != nil {
		return res, nil, &providers.Error{Provider: "doorkey", Status: http.StatusBadRequest, Message: "invalid route: " + err.Error()}
	}
	timeout := route.timeout()
	var lastErr error

	for ti, t := range route.Targets {
		last := ti == len(route.Targets)-1
		p := rt.providers[t.Provider]
		treq := *req
		treq.Model = t.Model

	tries:
		for try := 0; try <= route.Retries; try++ {
			if try > 0 {
				d, ok := rt.delay(try, lastErr)
				if !ok {
					break tries // Retry-After too long: fall back now
				}
				if err := rt.sleep(ctx, d); err != nil {
					return res, nil, err
				}
			}

			actx, cancel := context.WithCancel(ctx)
			var timedOut atomic.Bool
			timer := time.AfterFunc(timeout, func() {
				timedOut.Store(true)
				cancel()
			})

			start := time.Now()
			cleanup, err := call(actx, p, &treq)
			stopped := timer.Stop()
			dur := time.Since(start)

			if err == nil && !stopped && cleanup != nil {
				// The timeout fired just as a stream opened; its context is
				// (being) cancelled, so the stream is unusable. A completed
				// non-streaming response (no cleanup) is already whole and
				// is kept, so a free-tier request isn't wasted.
				cleanup()
				err = timeoutErr(t.Provider, timeout)
			}
			if err == nil {
				res.Provider, res.Model = t.Provider, t.Model
				res.Attempts = append(res.Attempts, Attempt{Provider: t.Provider, Model: t.Model, Status: http.StatusOK, Duration: dur})
				return res, cancel, nil
			}
			cancel()

			if ctx.Err() != nil {
				// The client went away or the whole request was cancelled.
				return res, nil, ctx.Err()
			}
			if timedOut.Load() {
				err = timeoutErr(t.Provider, timeout)
			}
			res.Attempts = append(res.Attempts, attemptFromErr(t, err, dur))
			lastErr = err

			switch classify(err, last) {
			case actionStop:
				return res, nil, err
			case actionFallback:
				break tries
			case actionRetry:
				// loop
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no target could be tried")
	}
	return res, nil, lastErr
}

type action int

const (
	actionRetry    action = iota // try the same target again (with backoff), then fall back
	actionFallback               // skip to the next target
	actionStop                   // the request itself is bad; give up
)

// classify decides what to do after a failed attempt. last says whether this
// is the route's final target.
func classify(err error, last bool) action {
	var pe *providers.Error
	if !errors.As(err, &pe) {
		return actionRetry // network error, connection reset, etc.
	}
	if pe.Status == http.StatusTooManyRequests && !last {
		// Free tiers rate-limit per minute, so a retry a few hundred ms
		// later almost always gets another 429, and some providers count
		// failed requests against the daily cap. Move on instead. The last
		// target still retries, so single-target routes behave sensibly.
		return actionFallback
	}
	if pe.Retryable() {
		return actionRetry
	}
	switch pe.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusPaymentRequired, http.StatusRequestEntityTooLarge:
		// Bad key, no access, unknown model, out of credit, or request too
		// big for this provider's limits: another provider may still work.
		return actionFallback
	}
	return actionStop
}

// delay returns how long to wait before retry number try (1-based). It
// returns ok=false when the provider asked for a longer wait than
// MaxRetryAfter, meaning: fall back instead of waiting.
func (rt *Router) delay(try int, lastErr error) (time.Duration, bool) {
	d := rt.BaseDelay << (try - 1)
	if d > rt.MaxDelay || d <= 0 {
		d = rt.MaxDelay
	}
	// Equal jitter: half fixed, half random, so retries from many clients
	// spread out without ever dropping to zero.
	d = d/2 + time.Duration(rt.jitter()*float64(d/2))

	var pe *providers.Error
	if errors.As(lastErr, &pe) && pe.RetryAfter > 0 {
		if pe.RetryAfter > rt.MaxRetryAfter {
			return 0, false
		}
		if pe.RetryAfter > d {
			d = pe.RetryAfter
		}
	}
	return d, true
}

func timeoutErr(provider string, d time.Duration) *providers.Error {
	return &providers.Error{Provider: provider, Status: http.StatusGatewayTimeout, Message: fmt.Sprintf("no response within %s", d)}
}

func attemptFromErr(t Target, err error, dur time.Duration) Attempt {
	a := Attempt{Provider: t.Provider, Model: t.Model, Error: err.Error(), Duration: dur}
	var pe *providers.Error
	if errors.As(err, &pe) {
		a.Status = pe.Status
	}
	return a
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// committedStream replays the first chunk the router already read, then
// passes through to the provider's stream.
type committedStream struct {
	first   *api.ChatChunk
	s       providers.Stream
	release func()
	closed  bool
}

func (c *committedStream) Recv() (*api.ChatChunk, error) {
	if c.first != nil {
		ch := c.first
		c.first = nil
		return ch, nil
	}
	return c.s.Recv()
}

func (c *committedStream) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	err := c.s.Close()
	if c.release != nil {
		c.release()
	}
	return err
}
