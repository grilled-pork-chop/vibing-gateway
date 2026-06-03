// Package probe fetches a backend's own OpenAI /v1/models response and rewrites it for the unified
// listing using the target's own rewrite strategy.
package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/llm-gateway/models-aggregator/internal/model"
)

// ErrNoBackend is returned when a target has no reachable backend to probe.
var ErrNoBackend = errors.New("no reachable backend")

// HTTP probes backends over plain HTTP.
type HTTP struct {
	client *http.Client
}

// New returns a probe that uses the given HTTP client (its Timeout bounds each request).
func New(client *http.Client) *HTTP { return &HTTP{client: client} }

// Fetch GETs http://<backend>/v1/models, decodes the OpenAI list, and applies the target's Rewrite
// (id rewriting + entry selection). It returns ErrNoBackend when the target has no backend address
// or the response yields nothing usable.
func (h *HTTP) Fetch(ctx context.Context, t model.Target) ([]model.Object, error) {
	if t.Backend == "" {
		return nil, ErrNoBackend
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+t.Backend+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backend %s returned %d", t.Backend, resp.StatusCode)
	}

	var body struct {
		Data []model.Object `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	objs := t.Rewrite(body.Data)
	if len(objs) == 0 {
		return nil, ErrNoBackend
	}
	return objs, nil
}
