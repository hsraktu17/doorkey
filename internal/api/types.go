// Package api holds the OpenAI-format types that every layer of doorkey
// speaks. Provider adapters translate to and from these types.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ChatRequest is an OpenAI /v1/chat/completions request.
type ChatRequest struct {
	Model               string         `json:"model"`
	Messages            []Message      `json:"messages"`
	Stream              bool           `json:"stream,omitempty"`
	StreamOptions       *StreamOptions `json:"stream_options,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	TopP                *float64       `json:"top_p,omitempty"`
	MaxTokens           *int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int           `json:"max_completion_tokens,omitempty"`
	Stop                StringList     `json:"stop,omitempty"`
	User                string         `json:"user,omitempty"`
}

// MaxOutputTokens returns the output token limit, preferring the newer
// max_completion_tokens field. It returns nil when neither is set.
func (r *ChatRequest) MaxOutputTokens() *int {
	if r.MaxCompletionTokens != nil {
		return r.MaxCompletionTokens
	}
	return r.MaxTokens
}

// StreamOptions controls extra data in a streamed response.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// Message is one chat message.
type Message struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
	Name    string  `json:"name,omitempty"`
}

// Content is message text. On input it accepts either a plain string or an
// array of {"type":"text","text":...} parts; it is always written as a string.
type Content string

func (c *Content) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*c = Content(s)
		return nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &parts); err != nil {
		return errors.New("content must be a string or an array of content parts")
	}
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type != "text" {
			return fmt.Errorf("content part type %q is not supported yet (only text)", p.Type)
		}
		texts = append(texts, p.Text)
	}
	*c = Content(strings.Join(texts, "\n"))
	return nil
}

// StringList accepts either a single string or an array of strings.
type StringList []string

func (s *StringList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*s = StringList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return errors.New("stop must be a string or an array of strings")
	}
	*s = many
	return nil
}

// ChatResponse is a non-streaming chat completion.
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice is one completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage is token accounting for one request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatChunk is one event of a streamed chat completion.
type ChatChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// ChunkChoice is the per-choice part of a stream chunk.
type ChunkChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// Delta is the incremental message content in a stream chunk.
type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// Finish reasons used in responses.
const (
	FinishStop          = "stop"
	FinishLength        = "length"
	FinishContentFilter = "content_filter"
	FinishToolCalls     = "tool_calls"
)

// Object names used in responses.
const (
	ObjectCompletion = "chat.completion"
	ObjectChunk      = "chat.completion.chunk"
)

// ErrorResponse is the OpenAI error envelope, so OpenAI SDKs can parse it.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes one error.
type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}
