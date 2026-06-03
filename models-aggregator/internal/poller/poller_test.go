package poller

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/llm-gateway/models-aggregator/internal/model"
	"github.com/llm-gateway/models-aggregator/internal/store"
)

type fakeDisc struct{ targets []model.Target }

func (f fakeDisc) Discover(context.Context) []model.Target { return f.targets }

// fakeFetch returns objs/err per target Key.
type fakeFetch struct {
	objs map[string][]model.Object
	err  map[string]error
}

func (f fakeFetch) Fetch(_ context.Context, t model.Target) ([]model.Object, error) {
	if e := f.err[t.Key]; e != nil {
		return nil, e
	}
	return f.objs[t.Key], nil
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func target(key string) model.Target {
	return model.Target{Key: key, BaseID: key, Root: key, OwnedBy: "test", Rewrite: model.PrefixRewriter("")}
}

func TestRefreshStoresFetchedObjects(t *testing.T) {
	c := store.New()
	p := New(
		fakeDisc{targets: []model.Target{target("a")}},
		fakeFetch{objs: map[string][]model.Object{"a": {{"id": "a", "max_model_len": 8.0}}}},
		c, time.Hour, quietLogger(),
	)
	p.refresh(context.Background())

	got := c.List()
	if len(got) != 1 || got[0].ID() != "a" || got[0]["max_model_len"] != 8.0 {
		t.Fatalf("unexpected list: %v", got)
	}
}

func TestRefreshKeepsLastKnownGoodOnFailure(t *testing.T) {
	c := store.New()
	disc := fakeDisc{targets: []model.Target{target("a")}}

	// First refresh succeeds and populates the cache.
	New(disc, fakeFetch{objs: map[string][]model.Object{"a": {{"id": "a"}}}}, c, time.Hour, quietLogger()).
		refresh(context.Background())

	// Second refresh: backend now fails — the entry must survive (last-known-good).
	New(disc, fakeFetch{err: map[string]error{"a": context.DeadlineExceeded}}, c, time.Hour, quietLogger()).
		refresh(context.Background())

	got := c.List()
	if len(got) != 1 || got[0].ID() != "a" {
		t.Fatalf("last-known-good not retained: %v", got)
	}
}

func TestRefreshSynthesizesWhenNeverSeen(t *testing.T) {
	c := store.New()
	p := New(
		fakeDisc{targets: []model.Target{target("publishers/slurm/models/x")}},
		fakeFetch{err: map[string]error{"publishers/slurm/models/x": context.DeadlineExceeded}},
		c, time.Hour, quietLogger(),
	)
	p.refresh(context.Background())

	got := c.List()
	if len(got) != 1 || got[0].ID() != "publishers/slurm/models/x" || got[0]["object"] != "model" {
		t.Fatalf("want synthesized fallback entry, got %v", got)
	}
}

func TestRefreshPrunesRemovedTargets(t *testing.T) {
	c := store.New()
	// Seed with two targets.
	New(
		fakeDisc{targets: []model.Target{target("a"), target("b")}},
		fakeFetch{objs: map[string][]model.Object{"a": {{"id": "a"}}, "b": {{"id": "b"}}}},
		c, time.Hour, quietLogger(),
	).refresh(context.Background())

	// Re-discover with only "a": "b" must be pruned.
	New(
		fakeDisc{targets: []model.Target{target("a")}},
		fakeFetch{objs: map[string][]model.Object{"a": {{"id": "a"}}}},
		c, time.Hour, quietLogger(),
	).refresh(context.Background())

	got := c.List()
	if len(got) != 1 || got[0].ID() != "a" {
		t.Fatalf("removed target not pruned: %v", got)
	}
}
