// Package sse reads and writes Server-Sent Events.
package sse

import (
	"bufio"
	"io"
	"strings"
)

// maxLineSize is the longest SSE line accepted. bufio.Scanner's 64KB default
// is too small for some provider chunks.
const maxLineSize = 4 << 20

// Event is one SSE event.
type Event struct {
	Name string // the "event:" field; empty when not sent
	Data string // the "data:" lines joined by "\n"
}

// Reader parses SSE events from a stream.
type Reader struct {
	s *bufio.Scanner
}

// NewReader returns a Reader for r.
func NewReader(r io.Reader) *Reader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), maxLineSize)
	return &Reader{s: s}
}

// Next returns the next event. It returns io.EOF when the stream ends cleanly.
func (r *Reader) Next() (Event, error) {
	var ev Event
	var data []string
	seen := false
	for r.s.Scan() {
		line := r.s.Text()
		if line == "" {
			if seen {
				ev.Data = strings.Join(data, "\n")
				return ev, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // comment / keep-alive
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			ev.Name = value
			seen = true
		case "data":
			data = append(data, value)
			seen = true
		}
	}
	if err := r.s.Err(); err != nil {
		return Event{}, err
	}
	if seen { // last event had no trailing blank line
		ev.Data = strings.Join(data, "\n")
		return ev, nil
	}
	return Event{}, io.EOF
}

// WriteData writes one "data:" event.
func WriteData(w io.Writer, data []byte) error {
	if _, err := io.WriteString(w, "data: "); err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n\n")
	return err
}

// WriteDone writes the OpenAI end-of-stream marker.
func WriteDone(w io.Writer) error {
	_, err := io.WriteString(w, "data: [DONE]\n\n")
	return err
}
