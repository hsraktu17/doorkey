package usage

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/hsraktu17/doorkey/internal/api"
)

func TestCost(t *testing.T) {
	p := Pricing{"groq/m": {InputPerMTok: 2, OutputPerMTok: 10}}
	cost, ok := p.Cost("groq", "m", api.Usage{PromptTokens: 1000, CompletionTokens: 500})
	if !ok || math.Abs(cost-0.007) > 1e-12 {
		t.Fatalf("cost = %v, %v; want 0.007", cost, ok)
	}
	if _, ok := p.Cost("groq", "other", api.Usage{}); ok {
		t.Fatal("unknown model must be unpriced")
	}
}

func TestShippedPricingLoads(t *testing.T) {
	p, err := LoadPricing(filepath.Join("..", "..", "pricing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p["gemini/gemini-2.5-flash"]; !ok {
		t.Fatalf("pricing.json should key models as provider/model, got %v", p)
	}
}
