package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/router"
	"github.com/hsraktu17/doorkey/internal/sse"
	"github.com/hsraktu17/doorkey/internal/usage"
)

// stream relays a streamed completion as SSE. Errors before the first chunk
// (when the router has already tried every target) get a normal JSON error
// response. Once SSE has started, an error is sent as a final
// {"error": ...} event, which OpenAI SDKs raise as an exception.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request, route router.Route, req *api.ChatRequest, rec *usage.Record, start time.Time) {
	s, res, err := h.opts.Router.Stream(r.Context(), route, req)
	rec.Apply(res)
	setRouteHeaders(w, res)
	if err != nil {
		h.upstreamError(w, r, err, res, rec)
		return
	}
	defer s.Close()

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no") // stop nginx from buffering the stream
	w.WriteHeader(http.StatusOK)
	rec.Status = http.StatusOK

	rc := http.NewResponseController(w)
	// Adapters always collect usage; only pass it on if the client asked,
	// because clients that didn't may choke on a chunk with no choices.
	wantUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	sentAny := false

	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			if sse.WriteDone(w) == nil {
				_ = rc.Flush()
			}
			return
		}
		if err != nil {
			if r.Context().Err() != nil {
				rec.Status, rec.Error = 499, "client disconnected"
				return
			}
			rec.StreamError, rec.Error = true, err.Error()
			b, _ := json.Marshal(api.ErrorResponse{Error: api.ErrorDetail{Message: err.Error(), Type: "upstream_error"}})
			if sse.WriteData(w, b) == nil {
				_ = rc.Flush()
			}
			return
		}

		if c.Usage != nil {
			u := *c.Usage
			rec.Usage = &u
			if !wantUsage {
				if len(c.Choices) == 0 {
					continue
				}
				cp := *c
				cp.Usage = nil
				c = &cp
			}
		}

		b, err := json.Marshal(c)
		if err != nil {
			continue
		}
		if err := sse.WriteData(w, b); err != nil {
			rec.Status, rec.Error = 499, "client disconnected"
			return
		}
		if err := rc.Flush(); err != nil {
			rec.Status, rec.Error = 499, "client disconnected"
			return
		}
		if !sentAny {
			sentAny = true
			rec.TTFT = time.Since(start)
		}
	}
}
