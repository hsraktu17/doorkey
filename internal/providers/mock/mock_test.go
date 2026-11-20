package mock

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
)

var req = &api.ChatRequest{Model: "mock-1", Messages: []api.Message{{Role: "user", Content: "ping pong"}}}

func TestStream(t *testing.T) {
	s, err := New("mock", Options{}).Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var sb strings.Builder
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
			sb.WriteString(ch.Delta.Content)
		}
		if c.Usage != nil {
			usage = c.Usage
		}
	}
	if got, want := sb.String(), "[mock/mock-1] you said: ping pong"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if usage == nil || usage.TotalTokens == 0 {
		t.Fatalf("missing usage")
	}
}

func TestFailStatus(t *testing.T) {
	_, err := New("flaky", Options{FailStatus: 429}).Complete(context.Background(), req)
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Status != 429 || !pe.Retryable() {
		t.Fatalf("want retryable 429, got %v", err)
	}
}
