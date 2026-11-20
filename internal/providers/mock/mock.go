// Package mock is a fake provider for tests, demos and benchmarks. It needs
// no key and no network: it echoes the last user message back.
package mock

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
)

// Options configure the mock's behaviour.
type Options struct {
	// FailStatus, when non-zero, makes every call fail with this HTTP status.
	FailStatus int `json:"fail_status"`
	// LatencyMS is waited before responding, and between stream chunks.
	LatencyMS int `json:"latency_ms"`
}

// Client is a mock provider.
type Client struct {
	name string
	opts Options
}

// New returns a mock provider.
func New(name string, opts Options) *Client {
	return &Client{name: name, opts: opts}
}

func (c *Client) Name() string { return c.name }

func (c *Client) fail() error {
	if c.opts.FailStatus == 0 {
		return nil
	}
	return &providers.Error{
		Provider: c.name,
		Status:   c.opts.FailStatus,
		Message:  fmt.Sprintf("mock failure (%d %s)", c.opts.FailStatus, http.StatusText(c.opts.FailStatus)),
	}
}

func (c *Client) wait(ctx context.Context) error {
	if c.opts.LatencyMS <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(time.Duration(c.opts.LatencyMS) * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) reply(req *api.ChatRequest) string {
	last := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			last = string(m.Content)
		}
	}
	return fmt.Sprintf("[%s/%s] you said: %s", c.name, req.Model, last)
}

// countTokens is a rough word count, good enough for a fake.
func countTokens(s string) int { return len(strings.Fields(s)) }

func (c *Client) usage(req *api.ChatRequest, reply string) *api.Usage {
	prompt := 0
	for _, m := range req.Messages {
		prompt += countTokens(string(m.Content))
	}
	completion := countTokens(reply)
	return &api.Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}
}

func (c *Client) Complete(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	if err := c.fail(); err != nil {
		return nil, err
	}
	text := c.reply(req)
	return &api.ChatResponse{
		ID:      fmt.Sprintf("chatcmpl-mock-%d", time.Now().UnixNano()),
		Object:  api.ObjectCompletion,
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []api.Choice{{
			Message:      api.Message{Role: "assistant", Content: api.Content(text)},
			FinishReason: api.FinishStop,
		}},
		Usage: c.usage(req, text),
	}, nil
}

func (c *Client) Stream(ctx context.Context, req *api.ChatRequest) (providers.Stream, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	if err := c.fail(); err != nil {
		return nil, err
	}
	text := c.reply(req)
	now := time.Now()
	s := &stream{ctx: ctx, c: c}
	base := api.ChatChunk{
		ID:      fmt.Sprintf("chatcmpl-mock-%d", now.UnixNano()),
		Object:  api.ObjectChunk,
		Created: now.Unix(),
		Model:   req.Model,
	}
	for i, word := range strings.Fields(text) {
		d := api.Delta{Content: word}
		if i > 0 {
			d.Content = " " + word
		} else {
			d.Role = "assistant"
		}
		ch := base
		ch.Choices = []api.ChunkChoice{{Delta: d}}
		s.chunks = append(s.chunks, &ch)
	}
	stop := api.FinishStop
	fin := base
	fin.Choices = []api.ChunkChoice{{FinishReason: &stop}}
	usage := base
	usage.Choices = []api.ChunkChoice{}
	usage.Usage = c.usage(req, text)
	s.chunks = append(s.chunks, &fin, &usage)
	return s, nil
}

type stream struct {
	ctx    context.Context
	c      *Client
	chunks []*api.ChatChunk
	sent   int
}

func (s *stream) Recv() (*api.ChatChunk, error) {
	if s.sent >= len(s.chunks) {
		return nil, io.EOF
	}
	if s.sent > 0 {
		if err := s.c.wait(s.ctx); err != nil {
			return nil, err
		}
	}
	ch := s.chunks[s.sent]
	s.sent++
	return ch, nil
}

func (s *stream) Close() error { return nil }
