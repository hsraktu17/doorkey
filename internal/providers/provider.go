// Package providers defines the interface every LLM provider adapter
// implements, and the typed error the router uses to decide on retries and
// fallback.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
)

// Provider is one upstream LLM API. Adapters take and return OpenAI-format
// types; req.Model is already the provider's own model name.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error)
	Stream(ctx context.Context, req *api.ChatRequest) (Stream, error)
}

// Stream yields OpenAI-format chunks. Recv returns io.EOF when the stream
// has ended cleanly. Close must always be called.
type Stream interface {
	Recv() (*api.ChatChunk, error)
	Close() error
}

// Error is a failure reported by (or about) an upstream provider.
type Error struct {
	Provider   string
	Status     int           // HTTP status from upstream, or a synthetic one
	Message    string        // upstream error message, trimmed
	RetryAfter time.Duration // from the Retry-After header; 0 if absent
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: upstream status %d: %s", e.Provider, e.Status, e.Message)
}

// Retryable reports whether the same request may succeed if sent again.
func (e *Error) Retryable() bool {
	return e.Status == http.StatusRequestTimeout ||
		e.Status == http.StatusTooManyRequests ||
		e.Status >= 500
}

// BadRequest returns an Error for a request the adapter cannot translate.
func BadRequest(provider, format string, args ...any) *Error {
	return &Error{Provider: provider, Status: http.StatusBadRequest, Message: fmt.Sprintf(format, args...)}
}

// maxErrorBody caps how much of an upstream error body is read.
const maxErrorBody = 8 << 10

// PostJSON sends body as JSON and returns the response when the status is
// 2xx. On any other status it closes the body and returns an *Error. The
// caller owns the returned body.
func PostJSON(ctx context.Context, hc *http.Client, provider, url string, headers map[string]string, body any) (*http.Response, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: encode request: %w", provider, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", provider, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", provider, err)
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	defer resp.Body.Close()
	return nil, errorFromResponse(provider, resp)
}

func errorFromResponse(provider string, resp *http.Response) *Error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	return &Error{
		Provider:   provider,
		Status:     resp.StatusCode,
		Message:    errorMessage(raw),
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

// errorMessage pulls "error.message" out of a JSON error body, falling back
// to the raw text.
func errorMessage(raw []byte) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil && body.Error.Message != "" {
		return body.Error.Message
	}
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		return "no error body"
	}
	return msg
}

// parseRetryAfter accepts delay-seconds or an HTTP date.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// NewHTTPClient returns the client adapters use. Per-attempt timeouts come
// from the request context (set by the router), so there is no overall
// client timeout that would cut long streams.
func NewHTTPClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 200
	t.MaxIdleConnsPerHost = 50
	t.IdleConnTimeout = 90 * time.Second
	t.ResponseHeaderTimeout = 60 * time.Second
	return &http.Client{Transport: t}
}
