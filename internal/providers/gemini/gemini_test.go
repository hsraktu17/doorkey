package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
)

func intp(v int) *int { return &v }

func TestCompleteTranslatesRequestAndResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-2.5-flash:generateContent" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("key must not be in the URL, query = %q", r.URL.RawQuery)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "test-key" {
			t.Errorf("x-goog-api-key = %q", got)
		}
		var body generateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.SystemInstruction == nil || body.SystemInstruction.Parts[0].Text != "be brief" {
			t.Errorf("system instruction = %+v", body.SystemInstruction)
		}
		if len(body.Contents) != 2 || body.Contents[0].Role != "user" || body.Contents[1].Role != "model" {
			t.Errorf("contents = %+v", body.Contents)
		}
		if body.GenerationConfig == nil || *body.GenerationConfig.MaxOutputTokens != 50 {
			t.Errorf("generation config = %+v", body.GenerationConfig)
		}
		w.Write([]byte(`{
			"candidates":[{"content":{"role":"model","parts":[{"text":"thinking...","thought":true},{"text":"Hi there"}]},"finishReason":"MAX_TOKENS"}],
			"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4,"thoughtsTokenCount":6,"totalTokenCount":20},
			"modelVersion":"gemini-2.5-flash","responseId":"abc"}`))
	}))
	defer srv.Close()

	c := New("gemini", srv.URL+"/v1beta", "test-key", srv.Client())
	resp, err := c.Complete(context.Background(), &api.ChatRequest{
		Model:     "gemini-2.5-flash",
		MaxTokens: intp(50),
		Messages: []api.Message{
			{Role: "system", Content: "be brief"},
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hey"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ch := resp.Choices[0]
	if ch.Message.Content != "Hi there" {
		t.Errorf("content = %q (thought parts must be skipped)", ch.Message.Content)
	}
	if ch.FinishReason != api.FinishLength {
		t.Errorf("finish = %q", ch.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 10 || resp.Usage.TotalTokens != 20 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if resp.ID != "chatcmpl-abc" {
		t.Errorf("id = %q", resp.ID)
	}
}

func TestStreamTranslation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-2.5-flash:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
			t.Errorf("url = %s", r.URL)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}],"usageMetadata":{"promptTokenCount":5,"totalTokenCount":5},"responseId":"r1"}`+"\r\n\r\n")
		io.WriteString(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"lo"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7},"responseId":"r1"}`+"\r\n\r\n")
	}))
	defer srv.Close()

	s, err := New("gemini", srv.URL, "k", srv.Client()).Stream(context.Background(), &api.ChatRequest{
		Model:    "models/gemini-2.5-flash",
		Messages: []api.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var chunks []*api.ChatChunk
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, c)
	}

	// Want: "Hel" (with role), "lo", finish=stop, usage-only chunk.
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks: %+v", len(chunks), chunks)
	}
	if d := chunks[0].Choices[0].Delta; d.Role != "assistant" || d.Content != "Hel" {
		t.Errorf("chunk 0 = %+v", d)
	}
	if d := chunks[1].Choices[0].Delta; d.Role != "" || d.Content != "lo" {
		t.Errorf("chunk 1 = %+v", d)
	}
	if fr := chunks[2].Choices[0].FinishReason; fr == nil || *fr != api.FinishStop {
		t.Errorf("chunk 2 finish = %v", fr)
	}
	if u := chunks[3].Usage; u == nil || u.PromptTokens != 5 || u.CompletionTokens != 2 || len(chunks[3].Choices) != 0 {
		t.Errorf("chunk 3 = %+v", chunks[3])
	}
	for _, c := range chunks {
		if c.ID != "chatcmpl-r1" {
			t.Errorf("chunk id = %q, want one stable id", c.ID)
		}
	}
}

func TestStreamCutOffIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}`+"\n\n")
		// connection closes before any finishReason
	}))
	defer srv.Close()

	s, err := New("gemini", srv.URL, "k", srv.Client()).Stream(context.Background(), &api.ChatRequest{
		Model:    "gemini-2.5-flash",
		Messages: []api.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Recv(); err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	_, err = s.Recv()
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Status != http.StatusBadGateway {
		t.Fatalf("want 502 for a cut-off stream, got %v", err)
	}
}

func TestToolRoleRejected(t *testing.T) {
	c := New("gemini", "http://unused", "k", nil)
	_, err := c.Complete(context.Background(), &api.ChatRequest{
		Model:    "m",
		Messages: []api.Message{{Role: "tool", Content: "x"}},
	})
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Status != http.StatusBadRequest || pe.Retryable() {
		t.Fatalf("want non-retryable 400, got %v", err)
	}
}

func TestInvalidKeyIsReportedAs401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","status":"INVALID_ARGUMENT"}}`)
	}))
	defer srv.Close()

	_, err := New("gemini", srv.URL, "bad", srv.Client()).Complete(context.Background(), &api.ChatRequest{
		Model:    "gemini-2.5-flash",
		Messages: []api.Message{{Role: "user", Content: "hi"}},
	})
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Status != http.StatusUnauthorized {
		t.Fatalf("want 401, got %v", err)
	}
}
