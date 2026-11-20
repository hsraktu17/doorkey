// Package usage prices requests and writes one structured log line per
// request: provider, model, latency, tokens, cost and what was tried.
package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/router"
)

// Price is USD per million tokens.
type Price struct {
	InputPerMTok  float64 `json:"input_per_mtok"`
	OutputPerMTok float64 `json:"output_per_mtok"`
}

// Pricing maps "provider/model" to a price.
type Pricing map[string]Price

// LoadPricing reads a pricing file. An empty path gives empty pricing.
func LoadPricing(path string) (Pricing, error) {
	if path == "" {
		return Pricing{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file struct {
		Models Pricing `json:"models"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if file.Models == nil {
		file.Models = Pricing{}
	}
	return file.Models, nil
}

// Cost returns the USD cost of u, and whether the model had a price.
func (p Pricing) Cost(provider, model string, u api.Usage) (float64, bool) {
	price, ok := p[provider+"/"+model]
	if !ok {
		return 0, false
	}
	cost := float64(u.PromptTokens)*price.InputPerMTok/1e6 +
		float64(u.CompletionTokens)*price.OutputPerMTok/1e6
	return cost, true
}

// Record is everything logged about one request.
type Record struct {
	RequestID      string
	Route          string
	RequestedModel string
	Stream         bool

	Provider string // who served it; empty if every target failed
	Model    string
	Attempts []router.Attempt

	Status      int // HTTP status sent to the client (499: client went away)
	Latency     time.Duration
	TTFT        time.Duration // streams: time to first chunk sent
	StreamError bool          // the stream started, then failed

	Usage   *api.Usage
	CostUSD float64
	Priced  bool
	Error   string
}

// Apply copies the router's result into the record.
func (r *Record) Apply(res router.Result) {
	r.Provider, r.Model, r.Attempts = res.Provider, res.Model, res.Attempts
}

// Logger writes records with slog.
type Logger struct {
	log     *slog.Logger
	pricing Pricing
}

// NewLogger returns a Logger.
func NewLogger(log *slog.Logger, pricing Pricing) *Logger {
	return &Logger{log: log, pricing: pricing}
}

// Log prices the record (when it has usage) and writes it.
func (l *Logger) Log(r *Record) {
	if r.Usage != nil && r.Provider != "" {
		r.CostUSD, r.Priced = l.pricing.Cost(r.Provider, r.Model, *r.Usage)
	}

	attrs := []slog.Attr{
		slog.String("request_id", r.RequestID),
		slog.String("route", r.Route),
		slog.String("requested_model", r.RequestedModel),
		slog.Bool("stream", r.Stream),
		slog.String("provider", r.Provider),
		slog.String("model", r.Model),
		slog.Int("status", r.Status),
		slog.Float64("latency_ms", ms(r.Latency)),
		slog.Int("attempts", len(r.Attempts)),
	}
	if len(r.Attempts) > 1 {
		attrs = append(attrs, slog.String("tried", tried(r.Attempts)))
	}
	if r.Stream && r.TTFT > 0 {
		attrs = append(attrs, slog.Float64("ttft_ms", ms(r.TTFT)))
	}
	if r.Usage != nil {
		attrs = append(attrs,
			slog.Int("prompt_tokens", r.Usage.PromptTokens),
			slog.Int("completion_tokens", r.Usage.CompletionTokens),
			slog.Float64("cost_usd", r.CostUSD),
		)
		if !r.Priced {
			attrs = append(attrs, slog.Bool("unpriced", true))
		}
	}
	if r.StreamError {
		attrs = append(attrs, slog.Bool("stream_error", true))
	}
	if r.Error != "" {
		attrs = append(attrs, slog.String("error", r.Error))
	}

	level := slog.LevelInfo
	if r.Status >= 500 || r.StreamError {
		level = slog.LevelError
	} else if r.Status >= 400 {
		level = slog.LevelWarn
	}
	l.log.LogAttrs(context.Background(), level, "request", attrs...)
}

// tried summarises attempts as "gemini:429 groq:200".
func tried(as []router.Attempt) string {
	parts := make([]string, len(as))
	for i, a := range as {
		parts[i] = fmt.Sprintf("%s:%d", a.Provider, a.Status)
	}
	return strings.Join(parts, " ")
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
