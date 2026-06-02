package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// vLLM-shaped /v1/models response: a base model plus a LoRA adapter, with extra fields that must
// survive pass-through.
func fakeVLLM(t *testing.T, models ...map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": models})
	}))
}

func newTestServer() *server {
	return &server{
		cfg:   config{publisherPref: "publishers", backendPort: "8000"},
		hc:    &http.Client{Timeout: 2 * time.Second},
		cache: newCache(),
	}
}

func TestFetchKServeRewritesIDAndKeepsFields(t *testing.T) {
	srv := fakeVLLM(t,
		map[string]any{"id": "facebook/opt-125m", "object": "model", "owned_by": "vllm", "max_model_len": 2048.0},
		map[string]any{"id": "opt-lora", "object": "model", "parent": "facebook/opt-125m"},
	)
	defer srv.Close()

	s := newTestServer()
	tg := target{kind: "kserve", prefix: "publishers/llm-demo/models/", root: "facebook/opt-125m", backend: hostPort(srv)}
	objs, err := s.fetch(context.Background(), tg)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("want 2 objects (base + adapter), got %d", len(objs))
	}
	if got := objs[0]["id"]; got != "publishers/llm-demo/models/facebook/opt-125m" {
		t.Errorf("base id = %v", got)
	}
	if got := objs[1]["id"]; got != "publishers/llm-demo/models/opt-lora" {
		t.Errorf("adapter id = %v", got)
	}
	// pass-through: non-id fields are preserved verbatim.
	if got := objs[0]["max_model_len"]; got != 2048.0 {
		t.Errorf("max_model_len not preserved: %v", got)
	}
	if got := objs[0]["owned_by"]; got != "vllm" {
		t.Errorf("owned_by not preserved: %v", got)
	}
}

func TestFetchSlurmUsesFixedIDAndDropsAdapters(t *testing.T) {
	srv := fakeVLLM(t,
		map[string]any{"id": "meta-llama/Llama-3-70B", "object": "model", "max_model_len": 8192.0},
	)
	defer srv.Close()

	s := newTestServer()
	tg := target{kind: "slurm", fixedID: "publishers/slurm/models/llama3-70b", root: "meta-llama/Llama-3-70B", backend: hostPort(srv)}
	objs, err := s.fetch(context.Background(), tg)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("slurm should advertise exactly one id, got %d", len(objs))
	}
	if got := objs[0]["id"]; got != "publishers/slurm/models/llama3-70b" {
		t.Errorf("slurm id = %v, want the route's fixed FQN", got)
	}
	if got := objs[0]["max_model_len"]; got != 8192.0 {
		t.Errorf("max_model_len not preserved: %v", got)
	}
}

func TestFetchNoBackendErrors(t *testing.T) {
	s := newTestServer()
	if _, err := s.fetch(context.Background(), target{kind: "slurm", backend: ""}); err == nil {
		t.Fatal("expected error for empty backend")
	}
}

func TestCacheListDedupesAndSorts(t *testing.T) {
	c := newCache()
	c.replace(map[string][]map[string]any{
		"b": {{"id": "publishers/x/models/b"}},
		"a": {{"id": "publishers/x/models/a"}, {"id": "publishers/x/models/a"}}, // duplicate id
	})
	got := c.list()
	if len(got) != 2 {
		t.Fatalf("want 2 unique ids, got %d", len(got))
	}
	if got[0]["id"] != "publishers/x/models/a" || got[1]["id"] != "publishers/x/models/b" {
		t.Errorf("not sorted by id: %v", got)
	}
}

func TestBaseID(t *testing.T) {
	k := target{kind: "kserve", prefix: "publishers/llm-demo/models/", root: "facebook/opt-125m"}
	if got := k.baseID(); got != "publishers/llm-demo/models/facebook/opt-125m" {
		t.Errorf("kserve baseID = %v", got)
	}
	sl := target{kind: "slurm", fixedID: "publishers/slurm/models/llama3-70b"}
	if got := sl.baseID(); got != "publishers/slurm/models/llama3-70b" {
		t.Errorf("slurm baseID = %v", got)
	}
}

// hostPort strips the scheme from an httptest server URL → "127.0.0.1:port".
func hostPort(srv *httptest.Server) string {
	return srv.URL[len("http://"):]
}
