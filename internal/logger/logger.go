// Package logger sets up doorkey's structured logging and writes the
// one-line-per-request access log.
//
// Two env vars control it:
//
//	DOORKEY_LOG_FORMAT  json (default) or text
//	DOORKEY_LOG_LEVEL   debug, info (default), warn or error
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Formats.
const (
	FormatJSON = "json"
	FormatText = "text"
)

// Options configure a logger.
type Options struct {
	Format string     // FormatJSON or FormatText; empty means JSON
	Level  slog.Level // minimum level written
}

// New returns a slog.Logger writing to w.
func New(w io.Writer, opts Options) (*slog.Logger, error) {
	ho := &slog.HandlerOptions{Level: opts.Level}
	switch strings.ToLower(opts.Format) {
	case "", FormatJSON:
		return slog.New(slog.NewJSONHandler(w, ho)), nil
	case FormatText:
		return slog.New(slog.NewTextHandler(w, ho)), nil
	default:
		return nil, fmt.Errorf("unknown log format %q (want %s or %s)", opts.Format, FormatJSON, FormatText)
	}
}

// OptionsFromEnv reads DOORKEY_LOG_FORMAT and DOORKEY_LOG_LEVEL.
func OptionsFromEnv() (Options, error) {
	opts := Options{Format: os.Getenv("DOORKEY_LOG_FORMAT")}
	lvl, err := ParseLevel(os.Getenv("DOORKEY_LOG_LEVEL"))
	if err != nil {
		return opts, err
	}
	opts.Level = lvl
	return opts, nil
}

// FromEnv returns a logger configured from the environment, writing to w.
func FromEnv(w io.Writer) (*slog.Logger, error) {
	opts, err := OptionsFromEnv()
	if err != nil {
		return nil, err
	}
	return New(w, opts)
}

// ParseLevel accepts debug, info, warn or error (any case). Empty means info.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q (want debug, info, warn or error)", s)
	}
}
