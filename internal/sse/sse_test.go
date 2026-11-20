package sse

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReader(t *testing.T) {
	in := ": keep-alive\n\n" +
		"event: message_start\ndata: {\"a\":1}\n\n" +
		"data: line1\ndata: line2\n\n" +
		"data: [DONE]" // no trailing blank line
	r := NewReader(strings.NewReader(in))

	want := []Event{
		{Name: "message_start", Data: `{"a":1}`},
		{Data: "line1\nline2"},
		{Data: "[DONE]"},
	}
	for i, w := range want {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
		if got != w {
			t.Fatalf("event %d: got %+v, want %+v", i, got, w)
		}
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("want io.EOF, got %v", err)
	}
}

func TestReaderLargeLine(t *testing.T) {
	big := strings.Repeat("x", 200*1024) // larger than bufio's 64KB default
	r := NewReader(strings.NewReader("data: " + big + "\n\n"))
	ev, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Data) != len(big) {
		t.Fatalf("got %d bytes, want %d", len(ev.Data), len(big))
	}
}

func TestWrite(t *testing.T) {
	var b bytes.Buffer
	if err := WriteData(&b, []byte(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := WriteDone(&b); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), "data: {\"x\":1}\n\ndata: [DONE]\n\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
