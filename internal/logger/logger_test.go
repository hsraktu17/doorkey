package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/router"
	"github.com/hsraktu17/doorkey/internal/usage"
)

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{
		"": slog.LevelInfo, "INFO": slog.LevelInfo, "debug": slog.LevelDebug,
		"warn": slog.LevelWarn, "warning": slog.LevelWarn, " Error ": slog.LevelError,
	} {
		if got, err := ParseLevel(in); err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Error("unknown level must be an error")
	}
}

func TestFromEnv(t *testing.T) {
	var buf bytes.Buffer
	t.Setenv("DOORKEY_LOG_FORMAT", "text")
	t.Setenv("DOORKEY_LOG_LEVEL", "warn")
	log, err := FromEnv(&buf)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("hidden")
	log.Warn("shown", "k", "v")
	out := buf.String()
	if strings.Contains(out, "hidden") || !strings.Contains(out, "msg=shown k=v") {
		t.Fatalf("text/warn output = %q", out)
	}

	t.Setenv("DOORKEY_LOG_FORMAT", "xml")
	if _, err := FromEnv(&buf); err == nil {
		t.Error("unknown format must be an error")
	}
	t.Setenv("DOORKEY_LOG_FORMAT", "")
	t.Setenv("DOORKEY_LOG_LEVEL", "nope")
	if _, err := FromEnv(&buf); err == nil {
		t.Error("unknown level must be an error")
	}
}

func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not one JSON line: %q: %v", buf.String(), err)
	}
	return m
}

func TestRequestLogLine(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, Options{})
	NewRequestLog(l).Add(&usage.Record{
		RequestID: "req_1", Route: "demo", RequestedModel: "demo", Stream: true,
		Provider: "mock", Model: "mock-2", Status: 200,
		Latency: 1500 * time.Microsecond, TTFT: 500 * time.Microsecond,
		Attempts: []router.Attempt{{Provider: "mock-flaky", Status: 429}, {Provider: "mock", Status: 200}},
		Usage:    &api.Usage{PromptTokens: 2, CompletionTokens: 5}, CostUSD: 0.25, Priced: true,
	})
	m := decodeLine(t, &buf)
	want := map[string]any{
		"msg": "request", "level": "INFO", "request_id": "req_1", "route": "demo",
		"provider": "mock", "status": 200.0, "latency_ms": 1.5, "ttft_ms": 0.5,
		"attempts": 2.0, "tried": "mock-flaky:429 mock:200",
		"prompt_tokens": 2.0, "completion_tokens": 5.0, "cost_usd": 0.25,
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
	if _, ok := m["unpriced"]; ok {
		t.Error("a priced request must not be marked unpriced")
	}
}

func TestRequestLogLevels(t *testing.T) {
	cases := []struct {
		rec  usage.Record
		want string
	}{
		{usage.Record{Status: 200}, "INFO"},
		{usage.Record{Status: 429}, "WARN"},
		{usage.Record{Status: 502}, "ERROR"},
		{usage.Record{Status: 200, StreamError: true}, "ERROR"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		l, _ := New(&buf, Options{})
		NewRequestLog(l).Add(&c.rec)
		if got := decodeLine(t, &buf)["level"]; got != c.want {
			t.Errorf("status %d stream_error=%v: level %v, want %s", c.rec.Status, c.rec.StreamError, got, c.want)
		}
	}
}

func TestRequestLogOmitsEmptyFields(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, Options{})
	NewRequestLog(l).Add(&usage.Record{Status: 200, Attempts: []router.Attempt{{Provider: "a", Status: 200}}})
	m := decodeLine(t, &buf)
	for _, k := range []string{"tried", "ttft_ms", "prompt_tokens", "error", "stream_error"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s should be omitted, line = %v", k, m)
		}
	}
}
