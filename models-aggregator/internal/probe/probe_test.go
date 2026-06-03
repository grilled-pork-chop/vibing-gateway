package probe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/llm-gateway/models-aggregator/internal/model"
)

func fakeVLLM(t *testing.T, models ...model.Object) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": models})
	}))
}

func newProbe() *HTTP { return New(&http.Client{Timeout: 2 * time.Second}) }

func hostPort(srv *httptest.Server) string { return srv.URL[len("http://"):] }

func TestFetchAppliesRewrite(t *testing.T) {
	srv := fakeVLLM(t,
		model.Object{"id": "facebook/opt-125m", "object": "model", "max_model_len": 2048.0},
		model.Object{"id": "opt-lora", "object": "model"},
	)
	defer srv.Close()

	tg := model.Target{Backend: hostPort(srv), Rewrite: model.PrefixRewriter("publishers/llm-demo/models/")}
	objs, err := newProbe().Fetch(context.Background(), tg)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(objs) != 2 || objs[0].ID() != "publishers/llm-demo/models/facebook/opt-125m" {
		t.Fatalf("unexpected objects: %v", objs)
	}
	if objs[0]["max_model_len"] != 2048.0 {
		t.Errorf("pass-through field lost: %v", objs[0])
	}
}

func TestFetchNoBackend(t *testing.T) {
	if _, err := newProbe().Fetch(context.Background(), model.Target{Backend: ""}); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("want ErrNoBackend, got %v", err)
	}
}

func TestFetchEmptyAfterRewrite(t *testing.T) {
	srv := fakeVLLM(t) // empty data
	defer srv.Close()
	tg := model.Target{Backend: hostPort(srv), Rewrite: model.PrefixRewriter("p/")}
	if _, err := newProbe().Fetch(context.Background(), tg); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("empty result should be ErrNoBackend, got %v", err)
	}
}
