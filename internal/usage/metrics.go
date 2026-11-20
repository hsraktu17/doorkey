package usage

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/hsraktu17/doorkey/internal/router"
)

// Sink receives every finished request record.
type Sink interface {
	Add(r *Record)
}

// Store keeps the most recent request records in memory and computes APM
// metrics from them. It resets when the process restarts; Phase 2 will put a
// database behind the same Sink interface.
type Store struct {
	mu      sync.Mutex
	buf     []entry
	next    int
	full    bool
	started time.Time
	now     func() time.Time
}

type entry struct {
	at  time.Time
	rec Record
}

// DefaultCapacity is how many records a Store keeps.
const DefaultCapacity = 10_000

// recentLimit is how many recent requests a snapshot lists.
const recentLimit = 50

// NewStore returns a Store holding up to capacity records.
func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Store{buf: make([]entry, capacity), started: time.Now(), now: time.Now}
}

// Add stores a copy of r, evicting the oldest record when full.
func (s *Store) Add(r *Record) {
	if s == nil {
		return
	}
	c := *r
	c.Attempts = append([]router.Attempt(nil), r.Attempts...)
	if r.Usage != nil {
		u := *r.Usage
		c.Usage = &u
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf[s.next] = entry{at: s.now(), rec: c}
	s.next = (s.next + 1) % len(s.buf)
	if s.next == 0 {
		s.full = true
	}
}

// entries returns stored entries at or after since, oldest first.
func (s *Store) entries(since time.Time) []entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.next
	if s.full {
		n = len(s.buf)
	}
	out := make([]entry, 0, n)
	for i := 0; i < n; i++ {
		idx := i
		if s.full {
			idx = (s.next + i) % len(s.buf)
		}
		if e := s.buf[idx]; !e.at.Before(since) {
			out = append(out, e)
		}
	}
	return out
}

// ---- snapshot types (the /admin/metrics JSON contract) ----

// Metrics is a point-in-time view over a time window.
type Metrics struct {
	GeneratedAt   time.Time       `json:"generated_at"`
	StartedAt     time.Time       `json:"started_at"`
	WindowSeconds int             `json:"window_seconds"`
	Capacity      int             `json:"capacity"`
	Totals        Totals          `json:"totals"`
	Series        []Bucket        `json:"series"`
	Providers     []ProviderStats `json:"providers"`
	Routes        []RouteStats    `json:"routes"`
	Recent        []RecentRequest `json:"recent"`
}

// Percentiles are in milliseconds.
type Percentiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

// Totals summarise every request in the window.
type Totals struct {
	Requests         int         `json:"requests"`
	Errors           int         `json:"errors"`
	ErrorRate        float64     `json:"error_rate"`
	Fallbacks        int         `json:"fallbacks"`
	FallbackRate     float64     `json:"fallback_rate"`
	StreamRequests   int         `json:"stream_requests"`
	RequestsPerMin   float64     `json:"requests_per_min"`
	LatencyMS        Percentiles `json:"latency_ms"`
	TTFTMS           Percentiles `json:"ttft_ms"`
	PromptTokens     int         `json:"prompt_tokens"`
	CompletionTokens int         `json:"completion_tokens"`
	CostUSD          float64     `json:"cost_usd"`
}

// Bucket is one minute of traffic.
type Bucket struct {
	Start    time.Time `json:"start"`
	Requests int       `json:"requests"`
	Errors   int       `json:"errors"`
	P50MS    float64   `json:"p50_ms"`
	P95MS    float64   `json:"p95_ms"`
}

// ProviderStats count every attempt against a provider, not only the
// requests it ended up serving, so rate limits on a fallback chain's first
// target stay visible.
type ProviderStats struct {
	Provider     string      `json:"provider"`
	Attempts     int         `json:"attempts"`
	Served       int         `json:"served"`
	FailureRate  float64     `json:"failure_rate"`
	Status2xx    int         `json:"status_2xx"`
	Status429    int         `json:"status_429"`
	Status4xx    int         `json:"status_4xx"` // other 4xx: auth, not found, bad request
	Status5xx    int         `json:"status_5xx"` // includes 504 timeouts
	NetworkError int         `json:"network_errors"`
	LatencyMS    Percentiles `json:"latency_ms"`
	Tokens       int         `json:"tokens"`
	CostUSD      float64     `json:"cost_usd"`
}

// RouteStats summarise requests per route.
type RouteStats struct {
	Route     string      `json:"route"`
	Requests  int         `json:"requests"`
	Errors    int         `json:"errors"`
	Fallbacks int         `json:"fallbacks"`
	LatencyMS Percentiles `json:"latency_ms"`
}

// RecentRequest is request metadata only; prompts and replies are never
// stored.
type RecentRequest struct {
	Time             time.Time `json:"time"`
	RequestID        string    `json:"request_id"`
	Route            string    `json:"route"`
	RequestedModel   string    `json:"requested_model"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	Stream           bool      `json:"stream"`
	Status           int       `json:"status"`
	LatencyMS        float64   `json:"latency_ms"`
	TTFTMS           float64   `json:"ttft_ms"`
	Attempts         int       `json:"attempts"`
	Tried            string    `json:"tried"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	Error            string    `json:"error,omitempty"`
}

// IsError reports whether a request counts as a failure: the gateway could
// not serve it (5xx), every target was rate-limited (429), or the stream
// broke after starting. Client mistakes (400, 401) and disconnects (499)
// are not counted.
func IsError(r *Record) bool {
	return r.StreamError || r.Status >= 500 || r.Status == 429
}

// Snapshot computes metrics over the last window.
func (s *Store) Snapshot(window time.Duration) Metrics {
	now := s.now()
	since := now.Add(-window)
	es := s.entries(since)

	m := Metrics{
		GeneratedAt:   now.UTC(),
		StartedAt:     s.started.UTC(),
		WindowSeconds: int(window / time.Second),
		Capacity:      len(s.buf),
		Series:        buckets(es, since, now),
		Providers:     []ProviderStats{},
		Routes:        []RouteStats{},
		Recent:        []RecentRequest{},
	}

	var lat, ttft []float64
	type provAcc struct {
		ps  ProviderStats
		lat []float64
	}
	provs := map[string]*provAcc{}
	type routeAcc struct {
		rs  RouteStats
		lat []float64
	}
	routes := map[string]*routeAcc{}

	for _, e := range es {
		r := &e.rec
		t := &m.Totals
		t.Requests++
		latMS := MS(r.Latency)
		lat = append(lat, latMS)
		isErr := IsError(r)
		if isErr {
			t.Errors++
		}
		fellBack := r.Provider != "" && len(r.Attempts) > 1
		if fellBack {
			t.Fallbacks++
		}
		if r.Stream {
			t.StreamRequests++
			if r.TTFT > 0 {
				ttft = append(ttft, MS(r.TTFT))
			}
		}
		if r.Usage != nil {
			t.PromptTokens += r.Usage.PromptTokens
			t.CompletionTokens += r.Usage.CompletionTokens
		}
		t.CostUSD += r.CostUSD

		ra := routes[r.Route]
		if ra == nil {
			ra = &routeAcc{rs: RouteStats{Route: r.Route}}
			routes[r.Route] = ra
		}
		ra.rs.Requests++
		ra.lat = append(ra.lat, latMS)
		if isErr {
			ra.rs.Errors++
		}
		if fellBack {
			ra.rs.Fallbacks++
		}

		for _, a := range r.Attempts {
			pa := provs[a.Provider]
			if pa == nil {
				pa = &provAcc{ps: ProviderStats{Provider: a.Provider}}
				provs[a.Provider] = pa
			}
			p := &pa.ps
			p.Attempts++
			pa.lat = append(pa.lat, MS(a.Duration))
			switch {
			case a.Status == 0:
				p.NetworkError++
			case a.Status < 300:
				p.Status2xx++
			case a.Status == 429:
				p.Status429++
			case a.Status < 500:
				p.Status4xx++
			default:
				p.Status5xx++
			}
		}
		if r.Provider != "" {
			if pa := provs[r.Provider]; pa != nil {
				pa.ps.Served++
				if r.Usage != nil {
					pa.ps.Tokens += r.Usage.TotalTokens
				}
				pa.ps.CostUSD += r.CostUSD
			}
		}
	}

	t := &m.Totals
	t.ErrorRate = ratio(t.Errors, t.Requests)
	t.FallbackRate = ratio(t.Fallbacks, t.Requests)
	if mins := window.Minutes(); mins > 0 {
		t.RequestsPerMin = round(float64(t.Requests)/mins, 2)
	}
	t.LatencyMS = percentiles(lat)
	t.TTFTMS = percentiles(ttft)
	t.CostUSD = round(t.CostUSD, 6)

	for _, pa := range provs {
		pa.ps.FailureRate = ratio(pa.ps.Attempts-pa.ps.Status2xx, pa.ps.Attempts)
		pa.ps.LatencyMS = percentiles(pa.lat)
		pa.ps.CostUSD = round(pa.ps.CostUSD, 6)
		m.Providers = append(m.Providers, pa.ps)
	}
	sort.Slice(m.Providers, func(i, j int) bool {
		if m.Providers[i].Attempts != m.Providers[j].Attempts {
			return m.Providers[i].Attempts > m.Providers[j].Attempts
		}
		return m.Providers[i].Provider < m.Providers[j].Provider
	})
	for _, ra := range routes {
		ra.rs.LatencyMS = percentiles(ra.lat)
		m.Routes = append(m.Routes, ra.rs)
	}
	sort.Slice(m.Routes, func(i, j int) bool {
		if m.Routes[i].Requests != m.Routes[j].Requests {
			return m.Routes[i].Requests > m.Routes[j].Requests
		}
		return m.Routes[i].Route < m.Routes[j].Route
	})

	for i := len(es) - 1; i >= 0 && len(m.Recent) < recentLimit; i-- {
		m.Recent = append(m.Recent, recent(es[i]))
	}
	return m
}

func recent(e entry) RecentRequest {
	r := e.rec
	out := RecentRequest{
		Time:           e.at.UTC(),
		RequestID:      r.RequestID,
		Route:          r.Route,
		RequestedModel: r.RequestedModel,
		Provider:       r.Provider,
		Model:          r.Model,
		Stream:         r.Stream,
		Status:         r.Status,
		LatencyMS:      MS(r.Latency),
		TTFTMS:         MS(r.TTFT),
		Attempts:       len(r.Attempts),
		Tried:          Tried(r.Attempts),
		CostUSD:        round(r.CostUSD, 6),
		Error:          r.Error,
	}
	if r.Usage != nil {
		out.PromptTokens = r.Usage.PromptTokens
		out.CompletionTokens = r.Usage.CompletionTokens
	}
	return out
}

// buckets groups entries into one-minute buckets covering [since, now],
// including empty minutes so charts have no gaps.
func buckets(es []entry, since, now time.Time) []Bucket {
	start := since.UTC().Truncate(time.Minute)
	end := now.UTC().Truncate(time.Minute)
	n := int(end.Sub(start)/time.Minute) + 1
	if n < 1 {
		n = 1
	}
	out := make([]Bucket, n)
	lats := make([][]float64, n)
	for i := range out {
		out[i].Start = start.Add(time.Duration(i) * time.Minute)
	}
	for _, e := range es {
		i := int(e.at.UTC().Sub(start) / time.Minute)
		if i < 0 || i >= n {
			continue
		}
		out[i].Requests++
		if IsError(&e.rec) {
			out[i].Errors++
		}
		lats[i] = append(lats[i], MS(e.rec.Latency))
	}
	for i := range out {
		p := percentiles(lats[i])
		out[i].P50MS, out[i].P95MS = p.P50, p.P95
	}
	return out
}

// percentiles uses the nearest-rank method on a sorted copy.
func percentiles(xs []float64) Percentiles {
	if len(xs) == 0 {
		return Percentiles{}
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	at := func(q float64) float64 {
		i := int(math.Ceil(q*float64(len(s)))) - 1
		if i < 0 {
			i = 0
		}
		return round(s[i], 3)
	}
	return Percentiles{P50: at(0.50), P95: at(0.95), P99: at(0.99)}
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return round(float64(a)/float64(b), 4)
}

func round(x float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(x*p) / p
}
