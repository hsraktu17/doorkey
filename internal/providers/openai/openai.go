// Package openai is the adapter for OpenAI-compatible chat APIs. One adapter
// covers Groq, OpenRouter, Hugging Face and OpenAI itself; only the base URL
// and key differ.
package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
	"github.com/hsraktu17/doorkey/internal/sse"
)

// DefaultBaseURL is OpenAI's own API.
const DefaultBaseURL = "https://api.openai.com/v1"

// Client talks to one OpenAI-compatible endpoint.
type Client struct {
	name    string
	baseURL string
	apiKey  string
	http    *http.Client
}

// New returns a Client. An empty baseURL means OpenAI itself.
func New(name, baseURL, apiKey string, hc *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if hc == nil {
		hc = providers.NewHTTPClient()
	}
	return &Client{name: name, baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: hc}
}

func (c *Client) Name() string { return c.name }

func (c *Client) headers() map[string]string {
	h := map[string]string{}
	if c.apiKey != "" {
		h["Authorization"] = "Bearer " + c.apiKey
	}
	return h
}

// Complete sends a non-streaming request.
func (c *Client) Complete(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	body := *req
	body.Stream = false
	body.StreamOptions = nil

	resp, err := providers.PostJSON(ctx, c.http, c.name, c.baseURL+"/chat/completions", c.headers(), &body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out api.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, &providers.Error{Provider: c.name, Status: http.StatusBadGateway, Message: "decode response: " + err.Error()}
	}
	if out.Choices == nil {
		out.Choices = []api.Choice{}
	}
	return &out, nil
}

// Stream sends a streaming request. It always asks for usage in the final
// chunk so the gateway can count tokens.
func (c *Client) Stream(ctx context.Context, req *api.ChatRequest) (providers.Stream, error) {
	body := *req
	body.Stream = true
	body.StreamOptions = &api.StreamOptions{IncludeUsage: true}

	h := c.headers()
	h["Accept"] = "text/event-stream"
	resp, err := providers.PostJSON(ctx, c.http, c.name, c.baseURL+"/chat/completions", h, &body)
	if err != nil {
		return nil, err
	}
	return &stream{name: c.name, body: resp.Body, r: sse.NewReader(resp.Body)}, nil
}

type stream struct {
	name string
	body io.ReadCloser
	r    *sse.Reader
	done bool
}

// streamEvent is a chunk that may instead carry an error.
type streamEvent struct {
	api.ChatChunk
	Error *streamError `json:"error"`
}

// streamError is an in-stream error. Providers disagree on the type of
// "code" (OpenRouter sends a number like 429, OpenAI a string), so it is
// kept raw.
type streamError struct {
	Message string          `json:"message"`
	Code    json.RawMessage `json:"code"`
}

// status returns the HTTP status a numeric code stands for, or 502.
func (e *streamError) status() int {
	var n int
	if json.Unmarshal(e.Code, &n) == nil && n >= 400 && n <= 599 {
		return n
	}
	return http.StatusBadGateway
}

func (s *stream) Recv() (*api.ChatChunk, error) {
	if s.done {
		return nil, io.EOF
	}
	for {
		ev, err := s.r.Next()
		if errors.Is(err, io.EOF) {
			// Upstream closed without [DONE].
			return nil, &providers.Error{Provider: s.name, Status: http.StatusBadGateway, Message: "stream ended without [DONE]"}
		}
		if err != nil {
			return nil, fmt.Errorf("%s: read stream: %w", s.name, err)
		}
		data := strings.TrimSpace(ev.Data)
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			s.done = true
			return nil, io.EOF
		}
		var se streamEvent
		if err := json.Unmarshal([]byte(data), &se); err != nil {
			return nil, &providers.Error{Provider: s.name, Status: http.StatusBadGateway, Message: "decode chunk: " + err.Error()}
		}
		if se.Error != nil {
			return nil, &providers.Error{Provider: s.name, Status: se.Error.status(), Message: se.Error.Message}
		}
		chunk := se.ChatChunk
		if chunk.Choices == nil {
			chunk.Choices = []api.ChunkChoice{}
		}
		return &chunk, nil
	}
}

func (s *stream) Close() error { return s.body.Close() }
