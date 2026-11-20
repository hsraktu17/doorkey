package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
)

func request(stream bool) *api.ChatRequest {
	return &api.ChatRequest{
		Model:    "openai/gpt-oss-20b",
		Stream:   stream,
		Messages: []api.Message{{Role: "user", Content: "hi"}},
	}
}

func TestComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != nil {
			t.Errorf("stream should be omitted, got %v", body["stream"])
		}
		w.Write([]byte(`{"id":"c1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	defer srv.Close()

	c := New("groq", srv.URL+"/v1/", "test-key", srv.Client())
	resp, err := c.Complete(context.Background(), request(false))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Message.Content != "hello" || resp.Usage.TotalTokens != 4 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestStreamAsksForUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body api.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.Stream || body.StreamOptions == nil || !body.StreamOptions.IncludeUsage {
			t.Errorf("want stream with include_usage, got %+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": OPENROUTER PROCESSING\n\n")
		io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"},"finish_reason":null}]}`+"\n\n")
		io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":"stop"}]}`+"\n\n")
		io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	s, err := New("openrouter", srv.URL, "k", srv.Client()).Stream(context.Background(), request(true))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var text string
	var usage *api.Usage
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, ch := range c.Choices {
			text += ch.Delta.Content
		}
		if c.Usage != nil {
			usage = c.Usage
		}
	}
	if text != "Hello" {
		t.Fatalf("text = %q", text)
	}
	if usage == nil || usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestErrorStatusAndRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limit reached","type":"rate_limit"}}`))
	}))
	defer srv.Close()

	_, err := New("groq", srv.URL, "k", srv.Client()).Complete(context.Background(), request(false))
	var pe *providers.Error
	if !errors.As(err, &pe) {
		t.Fatalf("want *providers.Error, got %T %v", err, err)
	}
	if pe.Status != 429 || pe.Message != "rate limit reached" || pe.RetryAfter != 7*time.Second || !pe.Retryable() {
		t.Fatalf("unexpected error: %+v", pe)
	}
}

func TestStreamNumericErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `data: {"error":{"code":429,"message":"rate limited"}}`+"\n\n")
	}))
	defer srv.Close()

	s, err := New("openrouter", srv.URL, "k", srv.Client()).Stream(context.Background(), request(true))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.Recv()
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Status != 429 || pe.Message != "rate limited" {
		t.Fatalf("want 429 rate limited, got %v", err)
	}
}

func TestStreamCutOffIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `data: {"id":"c1","choices":[{"index":0,"delta":{"content":"partial"}}]}`+"\n\n")
		// connection closes without [DONE]
	}))
	defer srv.Close()

	s, err := New("groq", srv.URL, "k", srv.Client()).Stream(context.Background(), request(true))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Recv(); err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	if _, err := s.Recv(); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("want error for missing [DONE], got %v", err)
	}
}
