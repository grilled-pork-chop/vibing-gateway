// Package httpapi exposes the aggregator over HTTP: the unified OpenAI GET /v1/models plus health
// and readiness probes. It depends only on the narrow Reader interface it declares.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/llm-gateway/models-aggregator/internal/model"
)

// Reader is the read side of the model cache the API serves from.
type Reader interface {
	List() []model.Object
	Ready() bool
}

// Handler returns the HTTP handler for the aggregator.
func Handler(r Reader) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if r.Ready() {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "warming up", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": r.List()}); err != nil {
			slog.Error("encode /v1/models response", "err", err)
		}
	})
	return mux
}
