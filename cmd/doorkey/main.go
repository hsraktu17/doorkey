// Command doorkey runs the LLM gateway.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hsraktu17/doorkey/internal/config"
	"github.com/hsraktu17/doorkey/internal/handler"
	"github.com/hsraktu17/doorkey/internal/logger"
	"github.com/hsraktu17/doorkey/internal/providers"
	"github.com/hsraktu17/doorkey/internal/router"
	"github.com/hsraktu17/doorkey/internal/usage"
)

func main() {
	log, err := logger.FromEnv(os.Stdout)
	if err != nil {
		// Fall back to a plain JSON logger so the bad setting is still reported.
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("invalid log settings", "error", err)
		os.Exit(2)
	}
	if err := run(log); err != nil {
		log.Error("doorkey failed", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	configPath := flag.String("config", envOr("DOORKEY_CONFIG", "doorkey.json"), "path to the config file")
	addr := flag.String("addr", os.Getenv("DOORKEY_ADDR"), "listen address (overrides the config file)")
	flag.Parse()

	path, err := pickConfig(*configPath, log)
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if *addr != "" {
		cfg.Listen = *addr
	}

	ps, err := cfg.BuildProviders(providers.NewHTTPClient(), log)
	if err != nil {
		return err
	}
	rt := router.New(ps...)
	if err := cfg.ValidateRoutes(rt); err != nil {
		return err
	}
	pricing, err := usage.LoadPricing(cfg.PricingFile)
	if err != nil {
		return err
	}

	apiKey := os.Getenv("DOORKEY_API_KEY")
	if apiKey == "" {
		log.Warn("DOORKEY_API_KEY is not set: the gateway accepts requests without a key")
	}

	metrics := usage.NewStore(usage.DefaultCapacity)
	h := handler.New(handler.Options{
		Router: rt,
		Routes: cfg.Routes,
		// Each finished request is priced, then goes to the metrics store
		// (for the dashboard) and the access log.
		Recorder: usage.NewRecorder(pricing, metrics, logger.NewRequestLog(log)),
		APIKey:   apiKey,
		Metrics:  metrics,
	})
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: it would cut long streams. Per-attempt timeouts
		// live in the router.
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		log.Info("doorkey listening", "addr", cfg.Listen, "config", path, "providers", len(ps), "routes", len(cfg.Routes))
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	log.Info("shutting down, waiting for in-flight requests")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// pickConfig uses the given path, or falls back to doorkey.example.json when
// the default doorkey.json doesn't exist yet.
func pickConfig(path string, log *slog.Logger) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	const example = "doorkey.example.json"
	if path == "doorkey.json" {
		if _, err := os.Stat(example); err == nil {
			log.Warn("doorkey.json not found, using doorkey.example.json")
			return example, nil
		}
	}
	return "", fmt.Errorf("config file %s not found (copy doorkey.example.json to doorkey.json)", path)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
