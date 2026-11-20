// Package usage holds the per-request Record, prices it, and keeps the
// in-memory metrics behind the dashboard. Writing log lines is package
// logger's job.
package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/router"
)

// Price is USD per million tokens.
type Price struct {
	InputPerMTok  float64 `json:"input_per_mtok"`
	OutputPerMTok float64 `json:"output_per_mtok"`
}

// Pricing maps "provider/model" to a price.
type Pricing map[string]Price

// LoadPricing reads a pricing file. An empty path gives empty pricing.
func LoadPricing(path string) (Pricing, error) {
	if path == "" {
		return Pricing{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file struct {
		Models Pricing `json:"models"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if file.Models == nil {
		file.Models = Pricing{}
	}
	return file.Models, nil
}

// Cost returns the USD cost of u, and whether the model had a price.
func (p Pricing) Cost(provider, model string, u api.Usage) (float64, bool) {
	price, ok := p[provider+"/"+model]
	if !ok {
		return 0, false
	}
	cost := float64(u.PromptTokens)*price.InputPerMTok/1e6 +
		float64(u.CompletionTokens)*price.OutputPerMTok/1e6
	return cost, true
}

// Record is everything logged about one request.
type Record struct {
	RequestID      string
	Route          string
	RequestedModel string
	Stream         bool

	Provider string // who served it; empty if every target failed
	Model    string
	Attempts []router.Attempt

	Status      int // HTTP status sent to the client (499: client went away)
	Latency     time.Duration
	TTFT        time.Duration // streams: time to first chunk sent
	StreamError bool          // the stream started, then failed

	Usage   *api.Usage
	CostUSD float64
	Priced  bool
	Error   string
}

// Apply copies the router's result into the record.
func (r *Record) Apply(res router.Result) {
	r.Provider, r.Model, r.Attempts = res.Provider, res.Model, res.Attempts
}

// Recorder finishes a request record: it prices the record (when it has
// usage) and hands it to every sink, such as the metrics Store and the
// request log in package logger.
type Recorder struct {
	pricing Pricing
	sinks   []Sink
}

// NewRecorder returns a Recorder.
func NewRecorder(pricing Pricing, sinks ...Sink) *Recorder {
	return &Recorder{pricing: pricing, sinks: sinks}
}

// Finish prices r and passes it to every sink.
func (rc *Recorder) Finish(r *Record) {
	if r.Usage != nil && r.Provider != "" {
		r.CostUSD, r.Priced = rc.pricing.Cost(r.Provider, r.Model, *r.Usage)
	}
	for _, s := range rc.sinks {
		s.Add(r)
	}
}

// Tried summarises attempts as "gemini:429 groq:200".
func Tried(as []router.Attempt) string {
	parts := make([]string, len(as))
	for i, a := range as {
		parts[i] = fmt.Sprintf("%s:%d", a.Provider, a.Status)
	}
	return strings.Join(parts, " ")
}

// MS converts a duration to milliseconds with microsecond precision.
func MS(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
