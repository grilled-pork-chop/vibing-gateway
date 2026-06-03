package store

import (
	"testing"

	"github.com/llm-gateway/models-aggregator/internal/model"
)

func TestReadyAfterReplace(t *testing.T) {
	c := New()
	if c.Ready() {
		t.Fatal("fresh cache should not be ready")
	}
	c.Replace(nil)
	if !c.Ready() {
		t.Fatal("cache should be ready after Replace")
	}
}

func TestListDedupesAndSorts(t *testing.T) {
	c := New()
	c.Replace(map[string][]model.Object{
		"b": {{"id": "publishers/x/models/b"}},
		"a": {{"id": "publishers/x/models/a"}, {"id": "publishers/x/models/a"}}, // duplicate id
		"e": {{"object": "model"}},                                              // no id → skipped
	})
	got := c.List()
	if len(got) != 2 {
		t.Fatalf("want 2 unique ids, got %d (%v)", len(got), got)
	}
	if got[0].ID() != "publishers/x/models/a" || got[1].ID() != "publishers/x/models/b" {
		t.Errorf("not sorted/deduped: %v", got)
	}
}
