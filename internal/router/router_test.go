package router

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
)

// ---- fakes ----

// item is one thing a fake stream yields.
type item struct {
	text  string
	err   error
	delay time.Duration
}

type fake struct {
	name string

	mu     sync.Mutex
	calls  int
	models []string

	// complete / stream get the 1-based call number.
	complete func(ctx context.Context, n int) (*api.ChatResponse, error)
	stream   func(ctx context.Context, n int) ([]item, error)
}

func (f *fake) Name() string { return f.name }

func (f *fake) next(model string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.models = append(f.models, model)
	return f.calls
}

func (f *fake) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fake) Complete(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	n := f.next(req.Model)
	if f.complete == nil {
		return ok(f.name), nil
	}
	return f.complete(ctx, n)
}

func (f *fake) Stream(ctx context.Context, req *api.ChatRequest) (providers.Stream, error) {
	n := f.next(req.Model)
	items := []item{{text: f.name}}
	if f.stream != nil {
		var err error
		if items, err = f.stream(ctx, n); err != nil {
			return nil, err
		}
	}
	return &fakeStream{ctx: ctx, items: items}, nil
}

type fakeStream struct {
	ctx    context.Context
	items  []item
	closed bool
}

func (s *fakeStream) Recv() (*api.ChatChunk, error) {
	if len(s.items) == 0 {
		return nil, io.EOF
	}
	it := s.items[0]
	s.items = s.items[1:]
	if it.delay > 0 {
		select {
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		case <-time.After(it.delay):
		}
	}
	if it.err != nil {
		return nil, it.err
	}
	return &api.ChatChunk{Choices: []api.ChunkChoice{{Delta: api.Delta{Content: it.text}}}}, nil
}

func (s *fakeStream) Close() error { s.closed = true; return nil }

func ok(name string) *api.ChatResponse {
	return &api.ChatResponse{Choices: []api.Choice{{Message: api.Message{Role: "assistant", Content: api.Content(name)}}}}
}

func perr(status int) error {
	return &providers.Error{Provider: "fake", Status: status, Message: http.StatusText(status)}
}

func always(err error) func(context.Context, int) (*api.ChatResponse, error) {
	return func(context.Context, int) (*api.ChatResponse, error) { return nil, err }
}

// newTestRouter returns a router whose sleeps are recorded, not waited.
func newTestRouter(ps ...providers.Provider) (*Router, *[]time.Duration) {
	rt := New(ps...)
	var slept []time.Duration
	var mu sync.Mutex
	rt.sleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		slept = append(slept, d)
		mu.Unlock()
		return ctx.Err()
	}
	rt.jitter = func() float64 { return 0.5 }
	return rt, &slept
}

func route(retries int, names ...string) Route {
	r := Route{Retries: retries, RequestTimeoutMS: 1000}
	for _, n := range names {
		r.Targets = append(r.Targets, Target{Provider: n, Model: n + "-model"})
	}
	return r
}

var req = &api.ChatRequest{Model: "ignored", Messages: []api.Message{{Role: "user", Content: "hi"}}}

func content(resp *api.ChatResponse) string { return string(resp.Choices[0].Message.Content) }

// ---- Complete ----

func TestFallbackAfterRetries(t *testing.T) {
	a := &fake{name: "a", complete: always(perr(503))}
	b := &fake{name: "b"}
	rt, slept := newTestRouter(a, b)

	resp, res, err := rt.Complete(context.Background(), route(2, "a", "b"), req)
	if err != nil {
		t.Fatal(err)
	}
	if content(resp) != "b" || res.Provider != "b" || res.Model != "b-model" {
		t.Fatalf("served by %q (%+v)", content(resp), res)
	}
	if a.Calls() != 3 || b.Calls() != 1 {
		t.Fatalf("calls a=%d b=%d, want 3 and 1", a.Calls(), b.Calls())
	}
	if len(res.Attempts) != 4 || res.Attempts[0].Status != 503 || res.Attempts[3].Status != 200 {
		t.Fatalf("attempts = %+v", res.Attempts)
	}
	if len(*slept) != 2 {
		t.Fatalf("want 2 backoff sleeps, got %v", *slept)
	}
	// Each target gets its own model name.
	if a.models[0] != "a-model" || b.models[0] != "b-model" {
		t.Fatalf("models a=%v b=%v", a.models, b.models)
	}
}

func TestRateLimitFallsBackWithoutRetry(t *testing.T) {
	a := &fake{name: "a", complete: always(perr(429))}
	b := &fake{name: "b"}
	rt, slept := newTestRouter(a, b)

	resp, _, err := rt.Complete(context.Background(), route(2, "a", "b"), req)
	if err != nil || content(resp) != "b" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if a.Calls() != 1 || len(*slept) != 0 {
		t.Fatalf("429 with a next target must fall back at once: calls=%d sleeps=%v", a.Calls(), *slept)
	}
}

func TestRateLimitOnLastTargetIsRetried(t *testing.T) {
	a := &fake{name: "a", complete: always(perr(429))}
	b := &fake{name: "b", complete: always(perr(429))}
	rt, slept := newTestRouter(a, b)

	_, _, err := rt.Complete(context.Background(), route(2, "a", "b"), req)
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Status != 429 {
		t.Fatalf("want 429, got %v", err)
	}
	if a.Calls() != 1 || b.Calls() != 3 || len(*slept) != 2 {
		t.Fatalf("calls a=%d b=%d sleeps=%v, want 1, 3 and 2 sleeps", a.Calls(), b.Calls(), *slept)
	}
}

func TestTooLargeFallsBack(t *testing.T) {
	a := &fake{name: "a", complete: always(perr(413))}
	b := &fake{name: "b"}
	rt, _ := newTestRouter(a, b)
	if resp, _, err := rt.Complete(context.Background(), route(2, "a", "b"), req); err != nil || content(resp) != "b" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if a.Calls() != 1 {
		t.Fatalf("413 must not be retried, calls=%d", a.Calls())
	}
}

func TestRetryThenSuccess(t *testing.T) {
	a := &fake{name: "a", complete: func(_ context.Context, n int) (*api.ChatResponse, error) {
		if n == 1 {
			return nil, perr(503)
		}
		return ok("a"), nil
	}}
	b := &fake{name: "b"}
	rt, _ := newTestRouter(a, b)

	resp, _, err := rt.Complete(context.Background(), route(2, "a", "b"), req)
	if err != nil || content(resp) != "a" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if b.Calls() != 0 {
		t.Fatal("must not fall back when a retry succeeds")
	}
}

func TestNoFallbackOnBadRequest(t *testing.T) {
	a := &fake{name: "a", complete: always(perr(400))}
	b := &fake{name: "b"}
	rt, _ := newTestRouter(a, b)

	_, _, err := rt.Complete(context.Background(), route(2, "a", "b"), req)
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Status != 400 {
		t.Fatalf("want 400, got %v", err)
	}
	if a.Calls() != 1 || b.Calls() != 0 {
		t.Fatalf("calls a=%d b=%d, want 1 and 0", a.Calls(), b.Calls())
	}
}

func TestFallbackOnAuthErrorWithoutRetry(t *testing.T) {
	a := &fake{name: "a", complete: always(perr(401))}
	b := &fake{name: "b"}
	rt, slept := newTestRouter(a, b)

	resp, _, err := rt.Complete(context.Background(), route(2, "a", "b"), req)
	if err != nil || content(resp) != "b" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if a.Calls() != 1 || len(*slept) != 0 {
		t.Fatalf("401 must not be retried: calls=%d sleeps=%v", a.Calls(), *slept)
	}
}

func TestNetworkErrorIsRetried(t *testing.T) {
	a := &fake{name: "a", complete: func(_ context.Context, n int) (*api.ChatResponse, error) {
		if n == 1 {
			return nil, errors.New("connection reset by peer")
		}
		return ok("a"), nil
	}}
	rt, _ := newTestRouter(a)
	if _, _, err := rt.Complete(context.Background(), route(1, "a"), req); err != nil {
		t.Fatal(err)
	}
	if a.Calls() != 2 {
		t.Fatalf("calls = %d", a.Calls())
	}
}

func TestAllTargetsFailReturnsLastError(t *testing.T) {
	a := &fake{name: "a", complete: always(perr(429))}
	b := &fake{name: "b", complete: always(perr(503))}
	rt, _ := newTestRouter(a, b)

	_, res, err := rt.Complete(context.Background(), route(1, "a", "b"), req)
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Status != 503 {
		t.Fatalf("want last error 503, got %v", err)
	}
	// a: one 429 then fall back; b: 503 plus one retry.
	if res.Provider != "" || len(res.Attempts) != 3 {
		t.Fatalf("result = %+v", res)
	}
}

func TestRetryAfterIsHonored(t *testing.T) {
	a := &fake{name: "a", complete: func(_ context.Context, n int) (*api.ChatResponse, error) {
		if n == 1 {
			return nil, &providers.Error{Provider: "a", Status: 429, RetryAfter: 3 * time.Second}
		}
		return ok("a"), nil
	}}
	rt, slept := newTestRouter(a)
	if _, _, err := rt.Complete(context.Background(), route(1, "a"), req); err != nil {
		t.Fatal(err)
	}
	if len(*slept) != 1 || (*slept)[0] != 3*time.Second {
		t.Fatalf("slept %v, want [3s]", *slept)
	}
}

func TestLongRetryAfterFallsBackImmediately(t *testing.T) {
	a := &fake{name: "a", complete: always(&providers.Error{Provider: "a", Status: 503, RetryAfter: time.Hour})}
	b := &fake{name: "b"}
	rt, slept := newTestRouter(a, b)

	resp, _, err := rt.Complete(context.Background(), route(3, "a", "b"), req)
	if err != nil || content(resp) != "b" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if a.Calls() != 1 || len(*slept) != 0 {
		t.Fatalf("must not wait an hour: calls=%d sleeps=%v", a.Calls(), *slept)
	}
}

func TestTimeoutFallsBack(t *testing.T) {
	hang := func(ctx context.Context, _ int) (*api.ChatResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	a := &fake{name: "a", complete: hang}
	b := &fake{name: "b"}
	rt, _ := newTestRouter(a, b)
	r := route(0, "a", "b")
	r.RequestTimeoutMS = 20

	resp, res, err := rt.Complete(context.Background(), r, req)
	if err != nil || content(resp) != "b" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if res.Attempts[0].Status != http.StatusGatewayTimeout {
		t.Fatalf("first attempt = %+v, want 504", res.Attempts[0])
	}
}

func TestClientCancelStopsEverything(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &fake{name: "a", complete: func(context.Context, int) (*api.ChatResponse, error) {
		cancel() // the client hangs up mid-request
		return nil, context.Canceled
	}}
	b := &fake{name: "b"}
	rt, _ := newTestRouter(a, b)

	_, _, err := rt.Complete(ctx, route(2, "a", "b"), req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if a.Calls() != 1 || b.Calls() != 0 {
		t.Fatalf("calls a=%d b=%d, want 1 and 0", a.Calls(), b.Calls())
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	rt, _ := newTestRouter()
	rt.BaseDelay, rt.MaxDelay = 100*time.Millisecond, 300*time.Millisecond
	rt.jitter = func() float64 { return 0 } // lower bound: half the delay
	want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 150 * time.Millisecond, 150 * time.Millisecond}
	for i, w := range want {
		if d, _ := rt.delay(i+1, perr(500)); d != w {
			t.Errorf("try %d: delay %v, want %v", i+1, d, w)
		}
	}
}

// ---- Stream ----

func drain(t *testing.T, s providers.Stream) (string, error) {
	t.Helper()
	var text string
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return text, nil
		}
		if err != nil {
			return text, err
		}
		text += c.Choices[0].Delta.Content
	}
}

func TestStreamFallsBackBeforeFirstChunk(t *testing.T) {
	a := &fake{name: "a", stream: func(context.Context, int) ([]item, error) {
		return []item{{err: perr(503)}}, nil // opens fine, fails on first read
	}}
	b := &fake{name: "b"}
	rt, _ := newTestRouter(a, b)

	s, res, err := rt.Stream(context.Background(), route(0, "a", "b"), req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if text, err := drain(t, s); err != nil || text != "b" || res.Provider != "b" {
		t.Fatalf("text=%q err=%v res=%+v", text, err, res)
	}
}

func TestStreamEmptyIsAFailure(t *testing.T) {
	a := &fake{name: "a", stream: func(context.Context, int) ([]item, error) { return nil, nil }}
	b := &fake{name: "b"}
	rt, _ := newTestRouter(a, b)

	s, res, err := rt.Stream(context.Background(), route(0, "a", "b"), req)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if res.Provider != "b" || res.Attempts[0].Status != http.StatusBadGateway {
		t.Fatalf("res = %+v", res)
	}
}

func TestStreamNoFallbackAfterCommit(t *testing.T) {
	a := &fake{name: "a", stream: func(context.Context, int) ([]item, error) {
		return []item{{text: "Hel"}, {err: perr(500)}}, nil
	}}
	b := &fake{name: "b"}
	rt, _ := newTestRouter(a, b)

	s, res, err := rt.Stream(context.Background(), route(2, "a", "b"), req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	text, err := drain(t, s)
	if text != "Hel" || err == nil {
		t.Fatalf("want partial text then an error, got %q, %v", text, err)
	}
	if res.Provider != "a" || a.Calls() != 1 || b.Calls() != 0 {
		t.Fatalf("must stay on a once committed: res=%+v a=%d b=%d", res, a.Calls(), b.Calls())
	}
}

func TestStreamTimeoutCoversOnlyFirstChunk(t *testing.T) {
	a := &fake{name: "a", stream: func(context.Context, int) ([]item, error) {
		// First chunk is fast; a later one takes longer than the timeout.
		return []item{{text: "a"}, {text: "b", delay: 80 * time.Millisecond}}, nil
	}}
	rt, _ := newTestRouter(a)
	r := route(0, "a")
	r.RequestTimeoutMS = 30

	s, _, err := rt.Stream(context.Background(), r, req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if text, err := drain(t, s); err != nil || text != "ab" {
		t.Fatalf("text=%q err=%v: the timeout must not cut an active stream", text, err)
	}
}

func TestStreamCloseReleasesProvider(t *testing.T) {
	var inner *fakeStream
	a := &fake{name: "a"}
	rt, _ := newTestRouter(a)
	s, _, err := rt.Stream(context.Background(), route(0, "a"), req)
	if err != nil {
		t.Fatal(err)
	}
	inner = s.(*committedStream).s.(*fakeStream)
	s.Close()
	s.Close() // idempotent
	if !inner.closed {
		t.Fatal("provider stream was not closed")
	}
}

// ---- Validate ----

func TestValidate(t *testing.T) {
	rt, _ := newTestRouter(&fake{name: "a"})
	bad := []Route{
		{},
		{Targets: []Target{{Provider: "nope", Model: "m"}}},
		{Targets: []Target{{Provider: "a"}}},
		{Targets: []Target{{Provider: "a", Model: "m"}}, Retries: 9},
		{Targets: []Target{{Provider: "a", Model: "m"}}, RequestTimeoutMS: -1},
	}
	for i, r := range bad {
		if rt.Validate(r) == nil {
			t.Errorf("route %d should be invalid: %+v", i, r)
		}
	}
	if err := rt.Validate(route(1, "a")); err != nil {
		t.Errorf("valid route rejected: %v", err)
	}
	if _, _, err := rt.Complete(context.Background(), Route{}, req); err == nil {
		t.Error("Complete must reject an invalid route")
	}
}
