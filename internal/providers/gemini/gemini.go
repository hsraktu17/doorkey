// Package gemini is the adapter for the Google Gemini API
// (generateContent / streamGenerateContent).
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
	"github.com/hsraktu17/doorkey/internal/sse"
)

// DefaultBaseURL is the Gemini Developer API.
const DefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// Client talks to the Gemini API.
type Client struct {
	name    string
	baseURL string
	apiKey  string
	http    *http.Client
	now     func() time.Time
}

// New returns a Client. An empty baseURL means the public Gemini API.
func New(name, baseURL, apiKey string, hc *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if hc == nil {
		hc = providers.NewHTTPClient()
	}
	return &Client{name: name, baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: hc, now: time.Now}
}

func (c *Client) Name() string { return c.name }

// The key goes in a header, never in the URL, so it can't leak into logs.
func (c *Client) headers() map[string]string {
	return map[string]string{"x-goog-api-key": c.apiKey}
}

func (c *Client) url(model, method string) string {
	model = strings.TrimPrefix(model, "models/")
	return c.baseURL + "/models/" + url.PathEscape(model) + ":" + method
}

// ---- Gemini wire types ----

type part struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type generationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type generateRequest struct {
	Contents          []content         `json:"contents"`
	SystemInstruction *content          `json:"systemInstruction,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type candidate struct {
	Content      content `json:"content"`
	FinishReason string  `json:"finishReason"`
}

type generateResponse struct {
	Candidates     []candidate    `json:"candidates"`
	UsageMetadata  *usageMetadata `json:"usageMetadata"`
	ModelVersion   string         `json:"modelVersion"`
	ResponseID     string         `json:"responseId"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ---- translation ----

func (c *Client) toGemini(req *api.ChatRequest) (*generateRequest, error) {
	out := &generateRequest{}
	var system []string
	for _, m := range req.Messages {
		text := string(m.Content)
		switch m.Role {
		case "system", "developer":
			system = append(system, text)
		case "user":
			out.Contents = append(out.Contents, content{Role: "user", Parts: []part{{Text: text}}})
		case "assistant":
			out.Contents = append(out.Contents, content{Role: "model", Parts: []part{{Text: text}}})
		default:
			return nil, providers.BadRequest(c.name, "message role %q is not supported", m.Role)
		}
	}
	if len(out.Contents) == 0 {
		return nil, providers.BadRequest(c.name, "at least one user or assistant message is required")
	}
	if len(system) > 0 {
		out.SystemInstruction = &content{Parts: []part{{Text: strings.Join(system, "\n\n")}}}
	}
	gc := generationConfig{
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		MaxOutputTokens: req.MaxOutputTokens(),
		StopSequences:   req.Stop,
	}
	if gc.Temperature != nil || gc.TopP != nil || gc.MaxOutputTokens != nil || len(gc.StopSequences) > 0 {
		out.GenerationConfig = &gc
	}
	return out, nil
}

func finishReason(r string) string {
	switch r {
	case "STOP", "":
		return api.FinishStop
	case "MAX_TOKENS":
		return api.FinishLength
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
		return api.FinishContentFilter
	default:
		return api.FinishStop
	}
}

func toUsage(u *usageMetadata) *api.Usage {
	if u == nil {
		return nil
	}
	completion := u.CandidatesTokenCount + u.ThoughtsTokenCount // thinking tokens are billed as output
	return &api.Usage{
		PromptTokens:     u.PromptTokenCount,
		CompletionTokens: completion,
		TotalTokens:      u.PromptTokenCount + completion,
	}
}

// candidateText joins the visible text parts, skipping thought summaries.
func candidateText(c candidate) string {
	var sb strings.Builder
	for _, p := range c.Content.Parts {
		if !p.Thought {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

func responseID(id string, now time.Time) string {
	if id != "" {
		return "chatcmpl-" + id
	}
	return fmt.Sprintf("chatcmpl-gemini-%d", now.UnixNano())
}

// ---- Complete ----

func (c *Client) Complete(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	body, err := c.toGemini(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.post(ctx, c.url(req.Model, "generateContent"), c.headers(), body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var g generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		return nil, &providers.Error{Provider: c.name, Status: http.StatusBadGateway, Message: "decode response: " + err.Error()}
	}

	now := c.now()
	out := &api.ChatResponse{
		ID:      responseID(g.ResponseID, now),
		Object:  api.ObjectCompletion,
		Created: now.Unix(),
		Model:   modelName(g.ModelVersion, req.Model),
		Usage:   toUsage(g.UsageMetadata),
	}
	if len(g.Candidates) == 0 {
		// The prompt itself was blocked.
		out.Choices = []api.Choice{{
			Message:      api.Message{Role: "assistant", Content: ""},
			FinishReason: api.FinishContentFilter,
		}}
		return out, nil
	}
	cand := g.Candidates[0]
	out.Choices = []api.Choice{{
		Message:      api.Message{Role: "assistant", Content: api.Content(candidateText(cand))},
		FinishReason: finishReason(cand.FinishReason),
	}}
	return out, nil
}

func modelName(version, requested string) string {
	if version != "" {
		return version
	}
	return requested
}

// ---- Stream ----

func (c *Client) Stream(ctx context.Context, req *api.ChatRequest) (providers.Stream, error) {
	body, err := c.toGemini(req)
	if err != nil {
		return nil, err
	}
	h := c.headers()
	h["Accept"] = "text/event-stream"
	resp, err := c.post(ctx, c.url(req.Model, "streamGenerateContent")+"?alt=sse", h, body)
	if err != nil {
		return nil, err
	}
	now := c.now()
	return &stream{
		name:    c.name,
		body:    resp.Body,
		r:       sse.NewReader(resp.Body),
		model:   req.Model,
		created: now.Unix(),
		id:      responseID("", now),
	}, nil
}

type stream struct {
	name    string
	body    io.ReadCloser
	r       *sse.Reader
	model   string
	created int64
	id      string

	pending  []*api.ChatChunk
	usage    *api.Usage
	sentRole bool
	finished bool // a finish_reason chunk has been queued
	done     bool
}

func (s *stream) chunk(delta api.Delta, finish *string) *api.ChatChunk {
	return &api.ChatChunk{
		ID:      s.id,
		Object:  api.ObjectChunk,
		Created: s.created,
		Model:   s.model,
		Choices: []api.ChunkChoice{{Delta: delta, FinishReason: finish}},
	}
}

func (s *stream) Recv() (*api.ChatChunk, error) {
	for {
		if len(s.pending) > 0 {
			c := s.pending[0]
			s.pending = s.pending[1:]
			return c, nil
		}
		if s.done {
			return nil, io.EOF
		}

		ev, err := s.r.Next()
		if errors.Is(err, io.EOF) {
			// Gemini has no [DONE]; the stream ends when the body closes. A
			// complete stream always carries a finishReason, so ending without
			// one means the connection was cut.
			s.done = true
			if !s.finished {
				return nil, &providers.Error{Provider: s.name, Status: http.StatusBadGateway, Message: "stream ended without finishReason"}
			}
			if s.usage != nil {
				s.pending = append(s.pending, &api.ChatChunk{
					ID: s.id, Object: api.ObjectChunk, Created: s.created, Model: s.model,
					Choices: []api.ChunkChoice{}, Usage: s.usage,
				})
			}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: read stream: %w", s.name, err)
		}
		if strings.TrimSpace(ev.Data) == "" {
			continue
		}

		var g generateResponse
		if err := json.Unmarshal([]byte(ev.Data), &g); err != nil {
			return nil, &providers.Error{Provider: s.name, Status: http.StatusBadGateway, Message: "decode chunk: " + err.Error()}
		}
		if g.Error != nil {
			status := g.Error.Code
			if status == 0 {
				status = http.StatusBadGateway
			}
			return nil, &providers.Error{Provider: s.name, Status: status, Message: g.Error.Message}
		}
		if g.ResponseID != "" && !s.sentRole {
			// Keep one ID for the whole stream: only take it before the first chunk.
			s.id = responseID(g.ResponseID, time.Time{})
		}
		if g.ModelVersion != "" {
			s.model = g.ModelVersion
		}
		if u := toUsage(g.UsageMetadata); u != nil {
			s.usage = u // cumulative; the last one wins
		}
		if len(g.Candidates) == 0 {
			if g.PromptFeedback != nil && g.PromptFeedback.BlockReason != "" && !s.finished {
				s.finished = true
				filter := api.FinishContentFilter
				s.pending = append(s.pending, s.chunk(api.Delta{Role: "assistant"}, &filter))
			}
			continue
		}

		cand := g.Candidates[0]
		if text := candidateText(cand); text != "" || !s.sentRole {
			d := api.Delta{Content: text}
			if !s.sentRole {
				d.Role = "assistant"
				s.sentRole = true
			}
			s.pending = append(s.pending, s.chunk(d, nil))
		}
		if cand.FinishReason != "" && !s.finished {
			s.finished = true
			fr := finishReason(cand.FinishReason)
			s.pending = append(s.pending, s.chunk(api.Delta{}, &fr))
		}
	}
}

func (s *stream) Close() error { return s.body.Close() }

// post sends a request. Gemini answers an invalid API key with 400
// (API_KEY_INVALID) rather than 401; it is reported as 401 so the router
// falls back to another provider instead of giving up on a "bad request".
func (c *Client) post(ctx context.Context, url string, headers map[string]string, body any) (*http.Response, error) {
	resp, err := providers.PostJSON(ctx, c.http, c.name, url, headers, body)
	var pe *providers.Error
	if errors.As(err, &pe) && pe.Status == http.StatusBadRequest &&
		(strings.Contains(pe.Message, "API key") || strings.Contains(pe.Message, "API_KEY_INVALID")) {
		pe.Status = http.StatusUnauthorized
	}
	return resp, err
}
