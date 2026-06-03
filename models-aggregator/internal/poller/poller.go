// Package poller orchestrates the background refresh loop: discover models, probe each backend, and
// publish the merged result into the store. It depends only on the small interfaces it declares, so
// it can be tested with fakes (no Kubernetes or HTTP).
package poller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/llm-gateway/models-aggregator/internal/model"
)

// Discoverer finds the models currently served through the gateway.
type Discoverer interface {
	Discover(ctx context.Context) []model.Target
}

// Fetcher retrieves and rewrites a target backend's /v1/models response.
type Fetcher interface {
	Fetch(ctx context.Context, t model.Target) ([]model.Object, error)
}

// Store holds the last-known-good objects per target key.
type Store interface {
	Snapshot() map[string][]model.Object
	Replace(map[string][]model.Object)
}

// Poller refreshes the store on an interval.
type Poller struct {
	disc     Discoverer
	fetch    Fetcher
	store    Store
	interval time.Duration
	log      *slog.Logger
}

// New builds a poller.
func New(disc Discoverer, fetch Fetcher, store Store, interval time.Duration, log *slog.Logger) *Poller {
	return &Poller{disc: disc, fetch: fetch, store: store, interval: interval, log: log}
}

// Run refreshes once immediately, then every interval, until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.refresh(ctx)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.refresh(ctx)
		}
	}
}

// refresh rediscovers every model, probes each backend concurrently, and rebuilds the store. The
// wholesale replace prunes models that no longer exist; a failed probe keeps the model's
// last-known-good entry, or a synthesized fallback if it has never been seen.
func (p *Poller) refresh(ctx context.Context) {
	targets := p.disc.Discover(ctx)
	prev := p.store.Snapshot()
	next := make(map[string][]model.Object, len(targets))

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t model.Target) {
			defer wg.Done()
			objs, err := p.fetch.Fetch(ctx, t)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && len(objs) > 0:
				next[t.Key] = objs
			case len(prev[t.Key]) > 0:
				next[t.Key] = prev[t.Key] // last-known-good
			default:
				next[t.Key] = []model.Object{t.Fallback()}
			}
		}(t)
	}
	wg.Wait()

	p.store.Replace(next)
}
