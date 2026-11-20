// Package handler is doorkey's HTTP layer: auth, request parsing, route
// selection, and writing JSON or SSE responses in the OpenAI format.
package handler

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
	"github.com/hsraktu17/doorkey/internal/router"
	"github.com/hsraktu17/doorkey/internal/usage"
)

// ConfigHeader carries a per-request route as JSON, overriding the config file.
const ConfigHeader = "X-Doorkey-Config"

// DefaultRoute is used when the model names no route or provider.
const DefaultRoute = "default"

// maxBody is the largest request body accepted.
const maxBody = 4 << 20

// Options configure a Handler.
type Options struct {
	Router *router.Router
	Routes map[string]router.Route
	Log    *usage.Logger
	// APIKey, when set, must be sent as "Authorization: Bearer <key>".
	APIKey string
}

// Handler serves the doorkey HTTP API.
type Handler struct {
	opts Options
	mux  *http.ServeMux
}

// New returns a Handler.
func New(opts Options) *Handler {
	h := &Handler{opts: opts, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST /v1/chat/completions", h.chat)
	h.mux.HandleFunc("GET /v1/models", h.models)
	h.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// ---- /v1/chat/completions ----

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &usage.Record{RequestID: newRequestID()}
	w.Header().Set("X-Doorkey-Request-Id", rec.RequestID)
	defer func() {
		rec.Latency = time.Since(start)
		h.opts.Log.Log(rec)
	}()

	fail := func(status int, typ, msg string) {
		rec.Status, rec.Error = status, msg
		writeError(w, status, typ, msg)
	}

	if !h.authorized(r) {
		fail(http.StatusUnauthorized, "authentication_error", "invalid or missing doorkey API key")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var req api.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(http.StatusBadRequest, "invalid_request_error", "invalid JSON body: "+err.Error())
		return
	}
	rec.RequestedModel, rec.Stream = req.Model, req.Stream
	if len(req.Messages) == 0 {
		fail(http.StatusBadRequest, "invalid_request_error", "messages is required")
		return
	}

	route, name, err := h.resolve(r, req.Model)
	if err != nil {
		fail(http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	rec.Route = name

	if req.Stream {
		h.stream(w, r, route, &req, rec, start)
	} else {
		h.complete(w, r, route, &req, rec)
	}
}

// resolve picks the route, in order:
//  1. the X-Doorkey-Config header (a JSON route)
//  2. "provider/model", when provider is a configured provider name
//  3. a configured route whose name equals the model
//  4. the "default" route
func (h *Handler) resolve(r *http.Request, model string) (router.Route, string, error) {
	if raw := r.Header.Get(ConfigHeader); raw != "" {
		var route router.Route
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&route); err != nil {
			return router.Route{}, "", fmt.Errorf("invalid %s header: %v", ConfigHeader, err)
		}
		if err := h.opts.Router.Validate(route); err != nil {
			return router.Route{}, "", fmt.Errorf("invalid %s header: %v", ConfigHeader, err)
		}
		return route, "header", nil
	}
	if prov, m, ok := strings.Cut(model, "/"); ok && m != "" {
		if _, known := h.opts.Router.Provider(prov); known {
			return router.Route{Targets: []router.Target{{Provider: prov, Model: m}}, Retries: 1}, "direct", nil
		}
	}
	if route, ok := h.opts.Routes[model]; ok {
		return route, model, nil
	}
	if route, ok := h.opts.Routes[DefaultRoute]; ok {
		return route, DefaultRoute, nil
	}
	return router.Route{}, "", fmt.Errorf("model %q matches no route or provider, and there is no %q route", model, DefaultRoute)
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request, route router.Route, req *api.ChatRequest, rec *usage.Record) {
	resp, res, err := h.opts.Router.Complete(r.Context(), route, req)
	rec.Apply(res)
	setRouteHeaders(w, res)
	if err != nil {
		h.upstreamError(w, r, err, res, rec)
		return
	}
	rec.Usage = resp.Usage
	rec.Status = http.StatusOK
	writeJSON(w, http.StatusOK, resp)
}

// upstreamError maps a router error to an OpenAI-style error response.
func (h *Handler) upstreamError(w http.ResponseWriter, r *http.Request, err error, res router.Result, rec *usage.Record) {
	rec.Error = err.Error()
	if r.Context().Err() != nil || errors.Is(err, context.Canceled) {
		rec.Status = 499 // client closed the request; nothing to write
		return
	}
	status, typ := http.StatusBadGateway, "upstream_error"
	var pe *providers.Error
	if errors.As(err, &pe) {
		switch {
		case pe.Status == http.StatusBadRequest:
			status, typ = http.StatusBadRequest, "invalid_request_error"
		case pe.Status == http.StatusTooManyRequests:
			status, typ = http.StatusTooManyRequests, "rate_limit_error"
			if pe.RetryAfter > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(pe.RetryAfter.Seconds()+0.5)))
			}
		case pe.Status == http.StatusGatewayTimeout:
			status, typ = http.StatusGatewayTimeout, "timeout_error"
		}
		// 401/403 from upstream mean a provider key problem on our side, not
		// the client's, so they stay 502.
	}
	rec.Status = status
	msg := err.Error()
	if len(res.Attempts) > 1 {
		// Show every failure, not just the last, so e.g. several missing
		// keys are visible at once.
		parts := make([]string, len(res.Attempts))
		for i, a := range res.Attempts {
			parts[i] = a.Error
		}
		msg = fmt.Sprintf("all %d attempts failed: %s", len(res.Attempts), strings.Join(parts, "; "))
	}
	writeError(w, status, typ, msg)
}

// ---- /v1/models ----

// models lists the route names, so OpenAI clients that call /v1/models see
// something they can pass as "model".
func (h *Handler) models(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		writeError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing doorkey API key")
		return
	}
	names := make([]string, 0, len(h.opts.Routes))
	for n := range h.opts.Routes {
		names = append(names, n)
	}
	sort.Strings(names)
	type model struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	data := make([]model, len(names))
	for i, n := range names {
		data[i] = model{ID: n, Object: "model", OwnedBy: "doorkey"}
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

// ---- helpers ----

func (h *Handler) authorized(r *http.Request) bool {
	if h.opts.APIKey == "" {
		return true
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(got), []byte(h.opts.APIKey)) == 1
}

func setRouteHeaders(w http.ResponseWriter, res router.Result) {
	w.Header().Set("X-Doorkey-Attempts", strconv.Itoa(len(res.Attempts)))
	if res.Provider != "" {
		w.Header().Set("X-Doorkey-Provider", res.Provider)
		w.Header().Set("X-Doorkey-Model", res.Model)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, typ, msg string) {
	writeJSON(w, status, api.ErrorResponse{Error: api.ErrorDetail{Message: msg, Type: typ}})
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "req_" + hex.EncodeToString(b[:])
}
