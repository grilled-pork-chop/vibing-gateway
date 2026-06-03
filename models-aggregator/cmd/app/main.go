// Command models-aggregator serves a single OpenAI-compatible GET /v1/models for the whole gateway.
//
// The gateway exposes two kinds of models on the same /v1 Body-Based-Routing endpoint, neither of
// which can answer a bodyless GET /v1/models on its own:
//
//   - In-cluster KServe models  (charts/model-server)  — one LLMInferenceService per release.
//   - External SLURM models     (charts/slurm-models)  — an AgentgatewayBackend + a header-matched
//     HTTPRoute per out-of-cluster OpenAI server.
//
// A background poller discovers both via the Kubernetes API (no per-model config), fetches each
// backend's own /v1/models, and saves the full response in an in-memory cache (last-known-good per
// model). GET /v1/models serves the merged union straight from that cache — instant, with constant
// latency regardless of backend health. Every field a backend returns is passed through unchanged;
// only each object's id is rewritten to the fully-qualified Body-Based-Routing key
// (publishers/<ns-or-publisher>/models/<name>) so it can be posted straight back to /v1.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/llm-gateway/models-aggregator/internal/config"
	"github.com/llm-gateway/models-aggregator/internal/discovery"
	"github.com/llm-gateway/models-aggregator/internal/httpapi"
	"github.com/llm-gateway/models-aggregator/internal/poller"
	"github.com/llm-gateway/models-aggregator/internal/probe"
	"github.com/llm-gateway/models-aggregator/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return err
	}

	cache := store.New()
	disc := discovery.New(dyn, log, cfg.PublisherPrefix, cfg.BackendPort, cfg.SlurmSelector, cfg.KServeSvcTemplate)
	fetcher := probe.New(&http.Client{Timeout: cfg.RequestTimeout})
	p := poller.New(disc, fetcher, cache, cfg.RefreshInterval, log)
	go p.Run(ctx)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httpapi.Handler(cache),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "publisher", cfg.PublisherPrefix, "refresh", cfg.RefreshInterval.String())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
