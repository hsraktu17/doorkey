package usage

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/router"
)

// Regenerate the golden file with: go test ./internal/usage -run Golden -update
var update = flag.Bool("update", false, "rewrite testdata golden files")

var base = time.Date(2026, 11, 20, 10, 0, 30, 0, time.UTC)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestStore(capacity int) (*Store, *clock) {
	c := &clock{t: base}
	s := NewStore(capacity)
	s.now = c.now
	s.started = base.Add(-time.Hour)
	return s, c
}

func att(provider string, status int, ms int) router.Attempt {
	return router.Attempt{Provider: provider, Model: provider + "-m", Status: status, Duration: time.Duration(ms) * time.Millisecond}
}

func at(c *clock, s *Store, offset time.Duration, r Record) {
	c.t = base.Add(offset)
	s.Add(&r)
}

// sample fills a store with a mix of traffic: fallbacks, a stream, total
// failure, a rate limit and a broken stream.
func sample() (*Store, *clock) {
	s, c := newTestStore(100)
	at(c, s, 0, Record{RequestID: "r1", Route: "demo", RequestedModel: "demo", Provider: "mock", Model: "mock-2",
		Status: 200, Latency: 5 * time.Millisecond, Attempts: []router.Attempt{att("mock-flaky", 429, 1), att("mock", 200, 2)},
		Usage: &api.Usage{PromptTokens: 2, CompletionTokens: 5, TotalTokens: 7}})
	at(c, s, 20*time.Second, Record{RequestID: "r2", Route: "demo", RequestedModel: "demo", Stream: true, Provider: "mock", Model: "mock-2",
		Status: 200, Latency: 8 * time.Millisecond, TTFT: 3 * time.Millisecond, Attempts: []router.Attempt{att("mock-flaky", 429, 1), att("mock", 200, 2)},
		Usage: &api.Usage{PromptTokens: 2, CompletionTokens: 5, TotalTokens: 7}})
	at(c, s, 70*time.Second, Record{RequestID: "r3", Route: "default", RequestedModel: "anything",
		Status: 502, Latency: time.Millisecond, Error: "all 3 attempts failed",
		Attempts: []router.Attempt{att("gemini", 401, 0), att("groq", 401, 0), att("openrouter", 401, 0)}})
	at(c, s, 80*time.Second, Record{RequestID: "r4", Route: "fast", RequestedModel: "fast", Provider: "groq", Model: "gpt-oss-20b",
		Status: 200, Latency: 120 * time.Millisecond, Attempts: []router.Attempt{att("groq", 200, 118)},
		Usage: &api.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}, CostUSD: 0.0001, Priced: true})
	at(c, s, 130*time.Second, Record{RequestID: "r5", Route: "direct", RequestedModel: "gemini/gemini-2.5-flash",
		Status: 429, Latency: 900 * time.Millisecond, Error: "rate limited",
		Attempts: []router.Attempt{att("gemini", 429, 300), att("gemini", 429, 300)}})
	at(c, s, 150*time.Second, Record{RequestID: "r6", Route: "fast", RequestedModel: "fast", Stream: true, Provider: "groq", Model: "gpt-oss-20b",
		Status: 200, StreamError: true, Latency: 400 * time.Millisecond, TTFT: 50 * time.Millisecond, Error: "connection lost",
		Attempts: []router.Attempt{att("groq", 200, 45)}})
	c.t = base.Add(3 * time.Minute)
	return s, c
}

func TestPercentiles(t *testing.T) {
	xs := make([]float64, 100)
	for i := range xs {
		xs[i] = float64(100 - i) // unsorted on purpose
	}
	if p := percentiles(xs); p.P50 != 50 || p.P95 != 95 || p.P99 != 99 {
		t.Fatalf("1..100: %+v", p)
	}
	if p := percentiles([]float64{7}); p.P50 != 7 || p.P99 != 7 {
		t.Fatalf("single: %+v", p)
	}
	if p := percentiles(nil); p != (Percentiles{}) {
		t.Fatalf("empty: %+v", p)
	}
	if xs[0] != 100 {
		t.Fatal("percentiles must not sort its input in place")
	}
}

func TestRingEvictsOldest(t *testing.T) {
	s, c := newTestStore(3)
	for i := 1; i <= 5; i++ {
		at(c, s, time.Duration(i)*time.Second, Record{RequestID: fmt.Sprint("r", i), Status: 200})
	}
	m := s.Snapshot(time.Hour)
	if m.Totals.Requests != 3 {
		t.Fatalf("requests = %d, want 3", m.Totals.Requests)
	}
	if m.Recent[0].RequestID != "r5" || m.Recent[2].RequestID != "r3" {
		t.Fatalf("recent = %v, want newest first r5..r3", []string{m.Recent[0].RequestID, m.Recent[1].RequestID, m.Recent[2].RequestID})
	}
}

func TestWindowExcludesOlder(t *testing.T) {
	s, _ := sample()
	// Now is 10:03:30; a 1m window starts at 10:02:30 and covers only
	// r5 (10:02:40) and r6 (10:03:00).
	m := s.Snapshot(time.Minute)
	if m.Totals.Requests != 2 {
		t.Fatalf("requests = %d, want 2", m.Totals.Requests)
	}
}

func TestTotals(t *testing.T) {
	s, _ := sample()
	tt := s.Snapshot(5 * time.Minute).Totals
	// Errors: r3 (502), r5 (429), r6 (broken stream).
	if tt.Requests != 6 || tt.Errors != 3 || tt.ErrorRate != 0.5 {
		t.Errorf("requests/errors = %d/%d (%v)", tt.Requests, tt.Errors, tt.ErrorRate)
	}
	// Fallbacks: r1 and r2 were served after a failed attempt. r5 retried
	// but was never served, so it is not a fallback.
	if tt.Fallbacks != 2 {
		t.Errorf("fallbacks = %d, want 2", tt.Fallbacks)
	}
	if tt.StreamRequests != 2 || tt.TTFTMS.P50 != 3 || tt.TTFTMS.P95 != 50 {
		t.Errorf("streams = %d ttft = %+v", tt.StreamRequests, tt.TTFTMS)
	}
	if tt.PromptTokens != 14 || tt.CompletionTokens != 30 || tt.CostUSD != 0.0001 {
		t.Errorf("tokens = %d/%d cost = %v", tt.PromptTokens, tt.CompletionTokens, tt.CostUSD)
	}
}

func TestProviderCountsEveryAttempt(t *testing.T) {
	s, _ := sample()
	got := map[string]ProviderStats{}
	for _, p := range s.Snapshot(5 * time.Minute).Providers {
		got[p.Provider] = p
	}
	flaky := got["mock-flaky"]
	if flaky.Attempts != 2 || flaky.Status429 != 2 || flaky.Served != 0 || flaky.FailureRate != 1 {
		t.Errorf("mock-flaky = %+v: its 429s must show although it never served", flaky)
	}
	if g := got["gemini"]; g.Attempts != 3 || g.Status4xx != 1 || g.Status429 != 2 {
		t.Errorf("gemini = %+v", g)
	}
	if g := got["groq"]; g.Served != 2 || g.Status2xx != 2 || g.Status4xx != 1 || g.Tokens != 30 {
		t.Errorf("groq = %+v", g)
	}
}

func TestBucketsCoverEveryMinute(t *testing.T) {
	s, _ := sample()
	series := s.Snapshot(5 * time.Minute).Series
	// Window 09:58:30..10:03:30 -> minutes 09:58 .. 10:03 = 6 buckets.
	if len(series) != 6 {
		t.Fatalf("buckets = %d, want 6", len(series))
	}
	want := map[string][2]int{ // minute -> requests, errors
		"09:59": {0, 0},
		"10:00": {2, 0}, // r1 10:00:30, r2 10:00:50
		"10:01": {2, 1}, // r3 (502) 10:01:40, r4 10:01:50
		"10:02": {1, 1}, // r5 (429) 10:02:40
		"10:03": {1, 1}, // r6 (stream error) 10:03:00
	}
	for _, b := range series {
		k := b.Start.Format("15:04")
		if w, ok := want[k]; ok && (b.Requests != w[0] || b.Errors != w[1]) {
			t.Errorf("%s: %d req / %d err, want %v", k, b.Requests, b.Errors, w)
		}
	}
}

func TestConcurrentAddAndSnapshot(t *testing.T) {
	s := NewStore(50)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				s.Add(&Record{Status: 200, Latency: time.Millisecond, Attempts: []router.Attempt{att("p", 200, 1)}})
				if i%20 == 0 {
					_ = s.Snapshot(time.Minute)
				}
			}
		}()
	}
	wg.Wait()
	if n := s.Snapshot(time.Hour).Totals.Requests; n != 50 {
		t.Fatalf("requests = %d, want capacity 50", n)
	}
}

func TestAddCopiesRecord(t *testing.T) {
	s, _ := newTestStore(10)
	r := &Record{Status: 200, Attempts: []router.Attempt{att("a", 200, 1)}, Usage: &api.Usage{TotalTokens: 1}}
	s.Add(r)
	r.Attempts[0].Provider = "mutated"
	r.Usage.TotalTokens = 99
	m := s.Snapshot(time.Hour)
	if m.Providers[0].Provider != "a" || m.Providers[0].Tokens != 0 {
		t.Fatalf("store must keep its own copy: %+v", m.Providers[0])
	}
}

// TestGolden pins the /admin/metrics JSON shape. The web dashboard's tests
// load the same file, so a change here that breaks the frontend fails there.
func TestGolden(t *testing.T) {
	s, _ := sample()
	got, err := json.MarshalIndent(s.Snapshot(5*time.Minute), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "metrics.sample.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("metrics JSON changed; if intended, run: go test ./internal/usage -run Golden -update\n%s", got)
	}
}
