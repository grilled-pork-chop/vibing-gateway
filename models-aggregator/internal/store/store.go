// Package store holds the aggregator's in-memory, last-known-good cache of model objects keyed by
// target. Slices are never mutated after being published via Replace, so readers can hold a
// snapshot without copying.
package store

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/llm-gateway/models-aggregator/internal/model"
)

// Cache is a concurrency-safe map of target key → the model objects that target contributes.
type Cache struct {
	mu    sync.RWMutex
	data  map[string][]model.Object
	ready atomic.Bool
}

// New returns an empty cache.
func New() *Cache {
	return &Cache{data: map[string][]model.Object{}}
}

// Replace swaps in a freshly built map (pruning keys no longer present) and marks the cache ready.
func (c *Cache) Replace(data map[string][]model.Object) {
	c.mu.Lock()
	c.data = data
	c.mu.Unlock()
	c.ready.Store(true)
}

// Snapshot returns the current map. The caller must not mutate it or its slices.
func (c *Cache) Snapshot() map[string][]model.Object {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data
}

// Ready reports whether at least one Replace has completed (first poll done).
func (c *Cache) Ready() bool { return c.ready.Load() }

// List returns the merged, de-duplicated (by id), id-sorted objects across all targets.
func (c *Cache) List() []model.Object {
	snap := c.Snapshot()
	seen := make(map[string]bool)
	out := make([]model.Object, 0, len(snap))
	for _, objs := range snap {
		for _, o := range objs {
			id := o.ID()
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}
