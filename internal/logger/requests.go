package logger

import (
	"context"
	"log/slog"

	"github.com/hsraktu17/doorkey/internal/usage"
)

// RequestLog writes one access-log line per finished request. It is a
// usage.Sink, so it sits next to the metrics Store behind a usage.Recorder.
type RequestLog struct {
	log *slog.Logger
}

// NewRequestLog returns a RequestLog writing through log.
func NewRequestLog(log *slog.Logger) *RequestLog {
	return &RequestLog{log: log}
}

// Add writes the record. Requests that end in a server error or a broken
// stream log at ERROR, client errors (4xx) at WARN, the rest at INFO.
func (l *RequestLog) Add(r *usage.Record) {
	attrs := []slog.Attr{
		slog.String("request_id", r.RequestID),
		slog.String("route", r.Route),
		slog.String("requested_model", r.RequestedModel),
		slog.Bool("stream", r.Stream),
		slog.String("provider", r.Provider),
		slog.String("model", r.Model),
		slog.Int("status", r.Status),
		slog.Float64("latency_ms", usage.MS(r.Latency)),
		slog.Int("attempts", len(r.Attempts)),
	}
	if len(r.Attempts) > 1 {
		attrs = append(attrs, slog.String("tried", usage.Tried(r.Attempts)))
	}
	if r.Stream && r.TTFT > 0 {
		attrs = append(attrs, slog.Float64("ttft_ms", usage.MS(r.TTFT)))
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
	l.log.LogAttrs(context.Background(), Level(r), "request", attrs...)
}

// Level is the log level for a finished request.
func Level(r *usage.Record) slog.Level {
	switch {
	case r.Status >= 500 || r.StreamError:
		return slog.LevelError
	case r.Status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
