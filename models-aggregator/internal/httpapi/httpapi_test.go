package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/llm-gateway/models-aggregator/internal/model"
)

type fakeReader struct {
	objs  []model.Object
	ready bool
}

func (f fakeReader) List() []model.Object { return f.objs }
func (f fakeReader) Ready() bool          { return f.ready }

func TestModelsEndpoint(t *testing.T) {
	h := Handler(fakeReader{objs: []model.Object{{"id": "publishers/x/models/a", "object": "model"}}, ready: true})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	var body struct {
		Object string         `json:"object"`
		Data   []model.Object `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Object != "list" || len(body.Data) != 1 || body.Data[0].ID() != "publishers/x/models/a" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestModelsRejectsNonGET(t *testing.T) {
	h := Handler(fakeReader{ready: true})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/models", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestReadyz(t *testing.T) {
	for _, tc := range []struct {
		ready bool
		want  int
	}{{true, http.StatusOK}, {false, http.StatusServiceUnavailable}} {
		rec := httptest.NewRecorder()
		Handler(fakeReader{ready: tc.ready}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code != tc.want {
			t.Errorf("ready=%v: status = %d, want %d", tc.ready, rec.Code, tc.want)
		}
	}
}

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(fakeReader{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
