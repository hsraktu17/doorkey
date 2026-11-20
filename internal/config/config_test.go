package config

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
	"github.com/hsraktu17/doorkey/internal/router"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// The example config shipped in the repo must always load and validate.
func TestExampleConfigIsValid(t *testing.T) {
	for _, env := range []string{"GEMINI_API_KEY", "GROQ_API_KEY", "OPENROUTER_API_KEY", "HF_TOKEN"} {
		t.Setenv(env, "")
	}
	c, err := Load(filepath.Join("..", "..", "doorkey.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	ps, err := c.BuildProviders(nil, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateRoutes(router.New(ps...)); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Routes["demo"]; !ok {
		t.Fatal("example config should have a keyless demo route")
	}
}

func TestMissingKeyDisablesProvider(t *testing.T) {
	t.Setenv("DOORKEY_TEST_KEY", "")
	c := &Config{Providers: []ProviderConfig{{Name: "groq", Type: TypeOpenAI, APIKeyEnv: "DOORKEY_TEST_KEY"}}}
	ps, err := c.BuildProviders(nil, quiet)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ps[0].Complete(context.Background(), &api.ChatRequest{})
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Status != http.StatusUnauthorized {
		t.Fatalf("want 401 so routes fall back, got %v", err)
	}
}

func TestBadConfigs(t *testing.T) {
	cases := map[string][]ProviderConfig{
		"duplicate name": {{Name: "a", Type: TypeMock}, {Name: "a", Type: TypeMock}},
		"unknown type":   {{Name: "a", Type: "anthropicx"}},
		"no name":        {{Type: TypeMock}},
	}
	for name, pcs := range cases {
		c := &Config{Providers: pcs}
		if _, err := c.BuildProviders(nil, quiet); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(path, []byte(`{"listen":":1","providerz":[]}`), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("a typo in the config must be an error")
	}
}
