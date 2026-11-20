package router

import (
	"errors"
	"fmt"
	"time"
)

// Target is one provider + model to try. Each target names its own model,
// because the same request needs a different model name on each provider.
type Target struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// Route is an ordered fallback chain: targets are tried in order, each up to
// 1+Retries times, until one succeeds.
type Route struct {
	Targets          []Target `json:"targets"`
	Retries          int      `json:"retries"`            // extra attempts per target
	RequestTimeoutMS int      `json:"request_timeout_ms"` // per attempt; 0 means DefaultTimeout
}

// Limits on route settings.
const (
	MaxRetries     = 5
	DefaultTimeout = 60 * time.Second
)

// timeout returns the per-attempt timeout. For streams it covers the time
// until the first chunk, not the whole stream.
func (r Route) timeout() time.Duration {
	if r.RequestTimeoutMS <= 0 {
		return DefaultTimeout
	}
	return time.Duration(r.RequestTimeoutMS) * time.Millisecond
}

// Validate checks the route's shape and that every target's provider is
// registered with the router.
func (rt *Router) Validate(r Route) error {
	if len(r.Targets) == 0 {
		return errors.New("route has no targets")
	}
	if r.Retries < 0 || r.Retries > MaxRetries {
		return fmt.Errorf("retries must be between 0 and %d, got %d", MaxRetries, r.Retries)
	}
	if r.RequestTimeoutMS < 0 {
		return fmt.Errorf("request_timeout_ms must not be negative, got %d", r.RequestTimeoutMS)
	}
	for i, t := range r.Targets {
		if t.Model == "" {
			return fmt.Errorf("target %d has no model", i)
		}
		if _, ok := rt.providers[t.Provider]; !ok {
			return fmt.Errorf("target %d: unknown provider %q", i, t.Provider)
		}
	}
	return nil
}
