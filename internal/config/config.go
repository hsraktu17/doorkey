// Package config loads doorkey's JSON config file and builds the providers
// and routes it describes.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/hsraktu17/doorkey/internal/api"
	"github.com/hsraktu17/doorkey/internal/providers"
	"github.com/hsraktu17/doorkey/internal/providers/gemini"
	"github.com/hsraktu17/doorkey/internal/providers/mock"
	"github.com/hsraktu17/doorkey/internal/providers/openai"
	"github.com/hsraktu17/doorkey/internal/router"
)

// Provider types.
const (
	TypeOpenAI = "openai" // any OpenAI-compatible API (Groq, OpenRouter, ...)
	TypeGemini = "gemini"
	TypeMock   = "mock"
)

// Config is the whole config file.
type Config struct {
	Listen      string                  `json:"listen"`
	PricingFile string                  `json:"pricing_file"`
	Providers   []ProviderConfig        `json:"providers"`
	Routes      map[string]router.Route `json:"routes"`
}

// ProviderConfig is one upstream provider.
type ProviderConfig struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"`
	BaseURL   string        `json:"base_url,omitempty"`
	APIKeyEnv string        `json:"api_key_env,omitempty"`
	Mock      *mock.Options `json:"mock,omitempty"`
}

// Load reads and parses a config file. Unknown fields are rejected so typos
// don't fail silently.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var c Config
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	return &c, nil
}

// BuildProviders creates one provider per entry. A provider whose key env
// var is unset is still registered, but every call to it fails with 401, so
// routes fall back past it and the log says which key is missing.
func (c *Config) BuildProviders(hc *http.Client, log *slog.Logger) ([]providers.Provider, error) {
	seen := map[string]bool{}
	var out []providers.Provider
	for i, pc := range c.Providers {
		if pc.Name == "" {
			return nil, fmt.Errorf("provider %d has no name", i)
		}
		if seen[pc.Name] {
			return nil, fmt.Errorf("provider %q is defined twice", pc.Name)
		}
		seen[pc.Name] = true

		key := ""
		if pc.APIKeyEnv != "" {
			key = os.Getenv(pc.APIKeyEnv)
			if key == "" && pc.Type != TypeMock {
				log.Warn("provider disabled: API key env var is not set", "provider", pc.Name, "env", pc.APIKeyEnv)
				out = append(out, disabled{name: pc.Name, reason: pc.APIKeyEnv + " is not set"})
				continue
			}
		}

		switch pc.Type {
		case TypeOpenAI:
			out = append(out, openai.New(pc.Name, pc.BaseURL, key, hc))
		case TypeGemini:
			out = append(out, gemini.New(pc.Name, pc.BaseURL, key, hc))
		case TypeMock:
			var opts mock.Options
			if pc.Mock != nil {
				opts = *pc.Mock
			}
			out = append(out, mock.New(pc.Name, opts))
		default:
			return nil, fmt.Errorf("provider %q: unknown type %q (want %s, %s or %s)", pc.Name, pc.Type, TypeOpenAI, TypeGemini, TypeMock)
		}
	}
	return out, nil
}

// ValidateRoutes checks every configured route against the router.
func (c *Config) ValidateRoutes(rt *router.Router) error {
	var errs []error
	for name, r := range c.Routes {
		if err := rt.Validate(r); err != nil {
			errs = append(errs, fmt.Errorf("route %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// disabled stands in for a provider with no API key.
type disabled struct {
	name   string
	reason string
}

func (d disabled) Name() string { return d.name }

func (d disabled) err() error {
	return &providers.Error{Provider: d.name, Status: http.StatusUnauthorized, Message: "provider disabled: " + d.reason}
}

func (d disabled) Complete(context.Context, *api.ChatRequest) (*api.ChatResponse, error) {
	return nil, d.err()
}

func (d disabled) Stream(context.Context, *api.ChatRequest) (providers.Stream, error) {
	return nil, d.err()
}
