package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/logger"
	"github.com/hsraktu17/doorkey/internal/providers"
	"github.com/hsraktu17/doorkey/internal/providers/mock"
	"github.com/hsraktu17/doorkey/internal/router"
	"github.com/hsraktu17/doorkey/internal/usage"
)

// brokenStream sends one chunk, then fails.
type brokenProvider struct{}

func (brokenProvider) Name() string { return "broken" }
func (brokenProvider) Complete(context.Context, *api.ChatRequest) (*api.ChatResponse, error) {
	return nil, &providers.Error{Provider: "broken", Status: 500, Message: "boom"}
}
func (brokenProvider) Stream(context.Context, *api.ChatRequest) (providers.Stream, error) {
	return &brokenStream{}, nil
}

type brokenStream struct{ n int }

func (s *brokenStream) Recv() (*api.ChatChunk, error) {
	s.n++
	if s.n == 1 {
		return &api.ChatChunk{Object: api.ObjectChunk, Choices: []api.ChunkChoice{{Delta: api.Delta{Role: "assistant", Content: "par"}}}}, nil
	}
	return nil, &providers.Error{Provider: "broken", Status: 502, Message: "connection lost"}
}
func (s *brokenStream) Close() error { return nil }

func newServer(t *testing.T, apiKey string) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	rt := router.New(
		mock.New("mock", mock.Options{}),
		mock.New("mock-flaky", mock.Options{FailStatus: 429}),
		mock.New("mock-limited", mock.Options{FailStatus: 429}),
		brokenProvider{},
	)
	rt.BaseDelay, rt.MaxDelay = 0, 0
	routes := map[string]router.Route{
		"default": {Targets: []router.Target{{Provider: "mock", Model: "m-default"}}},
		"demo": {Targets: []router.Target{
			{Provider: "mock-flaky", Model: "m1"},
			{Provider: "mock", Model: "m2"},
		}, Retries: 1},
		"limited": {Targets: []router.Target{{Provider: "mock-limited", Model: "m"}}},
		"broken":  {Targets: []router.Target{{Provider: "broken", Model: "b"}}},
	}
	var logs bytes.Buffer
	h := New(Options{
		Router: rt,
		Routes: routes,
		Recorder: usage.NewRecorder(usage.Pricing{"mock/m2": {InputPerMTok: 1e6, OutputPerMTok: 1e6}},
			logger.NewRequestLog(slog.New(slog.NewJSONHandler(&logs, nil)))),
		APIKey: apiKey,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, &logs
}

func post(t *testing.T, srv *httptest.Server, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func chatBody(model string, stream bool, extra string) string {
	return `{"model":"` + model + `","stream":` + map[bool]string{true: "true", false: "false"}[stream] +
		`,"messages":[{"role":"user","content":"hello there"}]` + extra + `}`
}

// readSSE returns every data payload until the stream ends.
func readSSE(t *testing.T, r io.Reader) []string {
	t.Helper()
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			out = append(out, d)
		}
	}
	return out
}

func TestCompleteFallsBack(t *testing.T) {
	srv, logs := newServer(t, "")
	resp := post(t, srv, chatBody("demo", false, ""), nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Doorkey-Provider"); got != "mock" {
		t.Errorf("provider = %q", got)
	}
	if got := resp.Header.Get("X-Doorkey-Attempts"); got != "2" {
		t.Errorf("attempts = %q (429 should fall back without retrying)", got)
	}
	var out api.ChatResponse
	json.NewDecoder(resp.Body).Decode(&out)
	if !strings.Contains(string(out.Choices[0].Message.Content), "hello there") {
		t.Errorf("content = %q", out.Choices[0].Message.Content)
	}
	if !strings.Contains(logs.String(), `"tried":"mock-flaky:429 mock:200"`) || !strings.Contains(logs.String(), `"cost_usd":`) {
		t.Errorf("log line missing fields: %s", logs.String())
	}
}

func TestStreamDoneAndUsageStripped(t *testing.T) {
	srv, logs := newServer(t, "")
	resp := post(t, srv, chatBody("demo", true, ""), nil)
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	events := readSSE(t, resp.Body)
	if len(events) < 2 || events[len(events)-1] != "[DONE]" {
		t.Fatalf("events = %v", events)
	}
	var text strings.Builder
	for _, e := range events[:len(events)-1] {
		var c api.ChatChunk
		if err := json.Unmarshal([]byte(e), &c); err != nil {
			t.Fatalf("bad chunk %q: %v", e, err)
		}
		if c.Usage != nil {
			t.Fatalf("usage sent although the client did not ask: %s", e)
		}
		for _, ch := range c.Choices {
			text.WriteString(ch.Delta.Content)
		}
	}
	if !strings.Contains(text.String(), "hello there") {
		t.Errorf("text = %q", text.String())
	}
	// The gateway still records usage for itself.
	if !strings.Contains(logs.String(), `"prompt_tokens"`) || !strings.Contains(logs.String(), `"ttft_ms"`) {
		t.Errorf("log missing usage/ttft: %s", logs.String())
	}
}

func TestStreamUsageWhenAsked(t *testing.T) {
	srv, _ := newServer(t, "")
	resp := post(t, srv, chatBody("demo", true, `,"stream_options":{"include_usage":true}`), nil)
	events := readSSE(t, resp.Body)
	var c api.ChatChunk
	json.Unmarshal([]byte(events[len(events)-2]), &c)
	if c.Usage == nil || len(c.Choices) != 0 {
		t.Fatalf("want a usage-only chunk before [DONE], got %s", events[len(events)-2])
	}
}

func TestStreamErrorAfterStartIsAnEvent(t *testing.T) {
	srv, logs := newServer(t, "")
	resp := post(t, srv, chatBody("broken", true, ""), nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	events := readSSE(t, resp.Body)
	if len(events) != 2 || !strings.Contains(events[1], `"error"`) || !strings.Contains(events[1], "connection lost") {
		t.Fatalf("events = %v", events)
	}
	if !strings.Contains(logs.String(), `"stream_error":true`) {
		t.Errorf("log should flag stream_error: %s", logs.String())
	}
}

func TestRateLimitedEverywhereReturns429(t *testing.T) {
	srv, _ := newServer(t, "")
	resp := post(t, srv, chatBody("limited", false, ""), nil)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var e api.ErrorResponse
	json.NewDecoder(resp.Body).Decode(&e)
	if e.Error.Type != "rate_limit_error" {
		t.Fatalf("error = %+v", e)
	}
}

func TestRouteResolution(t *testing.T) {
	srv, _ := newServer(t, "")
	cases := []struct {
		model, header, wantModel string
	}{
		{"demo", "", "m2"},                        // named route (after fallback)
		{"mock/custom-model", "", "custom-model"}, // provider/model
		{"something-else", "", "m-default"},       // default route
		{"demo", `{"targets":[{"provider":"mock","model":"from-header"}]}`, "from-header"},
	}
	for _, c := range cases {
		h := map[string]string{}
		if c.header != "" {
			h[ConfigHeader] = c.header
		}
		resp := post(t, srv, chatBody(c.model, false, ""), h)
		if got := resp.Header.Get("X-Doorkey-Model"); got != c.wantModel {
			t.Errorf("model %q header %q: served model %q, want %q", c.model, c.header, got, c.wantModel)
		}
	}
}

func TestBadRequests(t *testing.T) {
	srv, _ := newServer(t, "")
	cases := map[string]struct {
		body   string
		header string
	}{
		"bad json":      {body: `{"model":`},
		"no messages":   {body: `{"model":"demo","messages":[]}`},
		"bad header":    {body: chatBody("demo", false, ""), header: `{"targets":[{"provider":"nope","model":"x"}]}`},
		"unknown field": {body: chatBody("demo", false, ""), header: `{"targetz":[]}`},
	}
	for name, c := range cases {
		h := map[string]string{}
		if c.header != "" {
			h[ConfigHeader] = c.header
		}
		if resp := post(t, srv, c.body, h); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, resp.StatusCode)
		}
	}
}

func TestAuth(t *testing.T) {
	srv, _ := newServer(t, "secret")
	if resp := post(t, srv, chatBody("demo", false, ""), nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no key: status = %d", resp.StatusCode)
	}
	if resp := post(t, srv, chatBody("demo", false, ""), map[string]string{"Authorization": "Bearer wrong"}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong key: status = %d", resp.StatusCode)
	}
	if resp := post(t, srv, chatBody("demo", false, ""), map[string]string{"Authorization": "Bearer secret"}); resp.StatusCode != 200 {
		t.Errorf("right key: status = %d", resp.StatusCode)
	}
}

func TestModelsAndHealth(t *testing.T) {
	srv, _ := newServer(t, "")
	resp, err := srv.Client().Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct{ ID string } `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Data) != 4 || out.Data[0].ID != "broken" {
		t.Errorf("models = %+v", out.Data)
	}
	h, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil || h.StatusCode != 200 {
		t.Errorf("healthz: %v %v", h, err)
	}
	h.Body.Close()
}

func TestAllFailedListsEveryAttempt(t *testing.T) {
	srv, _ := newServer(t, "")
	hdr := map[string]string{ConfigHeader: `{"targets":[{"provider":"mock-flaky","model":"a"},{"provider":"broken","model":"b"}]}`}
	resp := post(t, srv, chatBody("x", false, ""), hdr)
	var e api.ErrorResponse
	json.NewDecoder(resp.Body).Decode(&e)
	if !strings.Contains(e.Error.Message, "all 2 attempts failed") ||
		!strings.Contains(e.Error.Message, "mock-flaky") || !strings.Contains(e.Error.Message, "broken") {
		t.Fatalf("message = %q", e.Error.Message)
	}
}

func newMetricsServer(t *testing.T, apiKey string, store *usage.Store) *httptest.Server {
	t.Helper()
	rt := router.New(mock.New("mock", mock.Options{}), mock.New("mock-flaky", mock.Options{FailStatus: 429}))
	opts := Options{
		Router:   rt,
		Routes:   map[string]router.Route{"demo": {Targets: []router.Target{{Provider: "mock-flaky", Model: "a"}, {Provider: "mock", Model: "b"}}}},
		Recorder: usage.NewRecorder(usage.Pricing{}, store, logger.NewRequestLog(slog.New(slog.NewJSONHandler(io.Discard, nil)))),
		APIKey:   apiKey,
	}
	if store != nil {
		opts.Metrics = store
	}
	srv := httptest.NewServer(New(opts))
	t.Cleanup(srv.Close)
	return srv
}

func getMetrics(t *testing.T, srv *httptest.Server, query, key string) (*http.Response, usage.Metrics) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/metrics"+query, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m usage.Metrics
	json.NewDecoder(resp.Body).Decode(&m)
	return resp, m
}

func TestMetricsEndpointCountsTraffic(t *testing.T) {
	store := usage.NewStore(100)
	srv := newMetricsServer(t, "", store)
	post(t, srv, chatBody("demo", false, ""), nil)
	resp := post(t, srv, chatBody("demo", true, ""), nil)
	readSSE(t, resp.Body) // let the stream finish so it is logged

	// The handler logs after the client has read [DONE], so wait briefly
	// for the second record to land.
	var r *http.Response
	var m usage.Metrics
	for i := 0; i < 100; i++ {
		if r, m = getMetrics(t, srv, "?window=5m", ""); m.Totals.Requests == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.StatusCode != 200 || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d, cache-control %q", r.StatusCode, r.Header.Get("Cache-Control"))
	}
	if m.WindowSeconds != 300 || m.Totals.Requests != 2 || m.Totals.Fallbacks != 2 || m.Totals.StreamRequests != 1 {
		t.Fatalf("totals = %+v", m.Totals)
	}
	var flaky *usage.ProviderStats
	for i := range m.Providers {
		if m.Providers[i].Provider == "mock-flaky" {
			flaky = &m.Providers[i]
		}
	}
	if flaky == nil || flaky.Status429 != 2 {
		t.Fatalf("providers = %+v", m.Providers)
	}
	if len(m.Recent) != 2 || m.Recent[0].Tried != "mock-flaky:429 mock:200" {
		t.Fatalf("recent = %+v", m.Recent)
	}
}

func TestMetricsEndpointAuthAndValidation(t *testing.T) {
	srv := newMetricsServer(t, "secret", usage.NewStore(10))
	if r, _ := getMetrics(t, srv, "", ""); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("no key: %d", r.StatusCode)
	}
	if r, _ := getMetrics(t, srv, "", "secret"); r.StatusCode != 200 {
		t.Errorf("right key: %d", r.StatusCode)
	}
	for _, q := range []string{"?window=abc", "?window=10s", "?window=48h"} {
		if r, _ := getMetrics(t, srv, q, "secret"); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", q, r.StatusCode)
		}
	}
}

func TestMetricsEndpointOffWithoutStore(t *testing.T) {
	srv := newMetricsServer(t, "", nil)
	if r, _ := getMetrics(t, srv, "", ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", r.StatusCode)
	}
}
