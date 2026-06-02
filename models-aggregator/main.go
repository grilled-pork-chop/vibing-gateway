// models-aggregator serves a single OpenAI-compatible GET /v1/models for the whole gateway.
//
// The gateway exposes two kinds of models on the same /v1 Body-Based-Routing endpoint, neither
// of which can answer a bodyless GET /v1/models on its own:
//
//   - In-cluster KServe models  (charts/model-server)  — one LLMInferenceService per release.
//   - External SLURM models     (charts/slurm-models)  — an AgentgatewayBackend + a header-matched
//     HTTPRoute per out-of-cluster OpenAI server.
//
// A background poller discovers both via the Kubernetes API (no per-model config), fetches each
// backend's own /v1/models, and SAVES the full response in an in-memory cache (last-known-good per
// model). GET /v1/models serves the merged union straight from that cache — instant, with constant
// latency regardless of backend health. Every field vLLM returns is passed through unchanged; only
// each object's `id` is rewritten to the fully-qualified Body-Based-Routing key
// (publishers/<ns-or-publisher>/models/<name>) so it can be posted straight back to /v1. A model
// whose backend is currently unreachable keeps its last-known-good entry (or, if never reached, a
// synthesized minimal entry) so nothing silently disappears.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GroupVersionResources we discover models from.
var (
	gvrLLMISVC = schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1alpha1", Resource: "llminferenceservices"}
	gvrRoute   = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	gvrBackend = schema.GroupVersionResource{Group: "agentgateway.dev", Version: "v1alpha1", Resource: "agentgatewaybackends"}
)

const bbrHeader = "X-Gateway-Model-Name" // the routing key the BBR policy matches on

// config is read once from the environment (wired by the Helm chart).
type config struct {
	listenAddr      string        // :8080
	publisherPref   string        // "publishers"
	backendPort     string        // OpenAI port to probe on KServe backends, e.g. "8000"
	slurmSelector   string        // label selector for slurm-models routes
	kserveSvcTmpl   string        // DNS template for a KServe backend: {name}/{namespace}
	requestTimeout  time.Duration // per-backend probe timeout
	refreshInterval time.Duration // background poll interval
}

func loadConfig() config {
	return config{
		listenAddr:      env("AGG_LISTEN_ADDR", ":8080"),
		publisherPref:   env("PUBLISHER_PREFIX", "publishers"),
		backendPort:     env("BACKEND_PORT", "8000"),
		slurmSelector:   env("SLURM_LABEL_SELECTOR", "app.kubernetes.io/name=slurm-models"),
		kserveSvcTmpl:   env("KSERVE_SVC_TEMPLATE", "{name}.{namespace}.svc.cluster.local"),
		requestTimeout:  time.Duration(envInt("REQUEST_TIMEOUT_SECONDS", 3)) * time.Second,
		refreshInterval: time.Duration(envInt("REFRESH_INTERVAL_SECONDS", 30)) * time.Second,
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// target is a discovered model backend to poll. key is unique per model and used as the cache key.
type target struct {
	key     string // cache key — KServe: the FQN prefix; SLURM: the route's fixed FQN
	kind    string // "kserve" | "slurm"
	prefix  string // KServe only: "publishers/<ns>/models/" — prepended to every reported id
	fixedID string // SLURM only: the single routable FQN (the route's X-Gateway-Model-Name value)
	root    string // real served-model name the backend answers to
	ownedBy string // fallback owned_by for a synthesized entry
	created int64  // fallback created for a synthesized entry
	backend string // host:port to probe, or "" if unknown
}

// baseID is the advertised FQN of the target's base model (used for a synthesized fallback entry).
func (t target) baseID() string {
	if t.kind == "slurm" {
		return t.fixedID
	}
	return t.prefix + t.root
}

// cache holds the last-known-good full model objects, keyed by target.key. Slices are never mutated
// after publishing, so readers can hold a snapshot without copying.
type cache struct {
	mu    sync.RWMutex
	data  map[string][]map[string]any
	ready atomic.Bool
}

func newCache() *cache { return &cache{data: map[string][]map[string]any{}} }

func (c *cache) replace(d map[string][]map[string]any) {
	c.mu.Lock()
	c.data = d
	c.mu.Unlock()
	c.ready.Store(true)
}

func (c *cache) snapshot() map[string][]map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data
}

// list returns the merged, de-duplicated, sorted model objects across all backends.
func (c *cache) list() []map[string]any {
	snap := c.snapshot()
	seen := map[string]bool{}
	out := make([]map[string]any, 0, len(snap))
	for _, objs := range snap {
		for _, o := range objs {
			id := asString(o["id"])
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return asString(out[i]["id"]) < asString(out[j]["id"]) })
	return out
}

type server struct {
	cfg   config
	dyn   dynamic.Interface
	hc    *http.Client
	cache *cache
}

func main() {
	cfg := loadConfig()

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("in-cluster config: %v", err)
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		log.Fatalf("dynamic client: %v", err)
	}

	s := &server{cfg: cfg, dyn: dyn, hc: &http.Client{Timeout: cfg.requestTimeout}, cache: newCache()}
	go s.refreshLoop(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.cache.ready.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "warming up", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/v1/models", s.handleModels)

	log.Printf("models-aggregator on %s (publisher=%s, refresh=%s)", cfg.listenAddr, cfg.publisherPref, cfg.refreshInterval)
	srv := &http.Server{Addr: cfg.listenAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func (s *server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": s.cache.list()})
}

// refreshLoop refreshes the cache immediately, then every refreshInterval.
func (s *server) refreshLoop(ctx context.Context) {
	s.refresh(ctx)
	t := time.NewTicker(s.cfg.refreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refresh(ctx)
		}
	}
}

// refresh rediscovers every model, polls each backend's /v1/models concurrently, and rebuilds the
// cache. The wholesale replace prunes models that no longer exist; a failed poll keeps the model's
// last-known-good entry (or a synthesized minimal one if never seen).
func (s *server) refresh(ctx context.Context) {
	targets := s.discover(ctx)
	prev := s.cache.snapshot()
	next := make(map[string][]map[string]any, len(targets))

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			objs, err := s.fetch(ctx, t)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && len(objs) > 0:
				next[t.key] = objs
			case len(prev[t.key]) > 0:
				next[t.key] = prev[t.key] // last-known-good
			default:
				next[t.key] = []map[string]any{synth(t)}
			}
		}(t)
	}
	wg.Wait()

	s.cache.replace(next)
}

// fetch retrieves a target's backend /v1/models and rewrites each object's id to its FQN, keeping
// every other field. KServe expands every reported model (base + LoRA adapters); SLURM advertises
// only the single route-backed id.
func (s *server) fetch(ctx context.Context, t target) ([]map[string]any, error) {
	if t.backend == "" {
		return nil, errNoBackend
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+t.backend+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	if t.kind == "slurm" {
		// One route → one routable id. Take the object matching the real model name, else the first.
		obj := pickBase(body.Data, t.root)
		if obj == nil {
			return nil, errNoBackend
		}
		obj["id"] = t.fixedID
		return []map[string]any{obj}, nil
	}

	out := make([]map[string]any, 0, len(body.Data))
	for _, o := range body.Data {
		id := asString(o["id"])
		if id == "" {
			continue
		}
		o["id"] = t.prefix + id
		out = append(out, o)
	}
	return out, nil
}

// pickBase returns the object whose id equals root, or the first object if none match.
func pickBase(data []map[string]any, root string) map[string]any {
	for _, o := range data {
		if asString(o["id"]) == root {
			return o
		}
	}
	if len(data) > 0 {
		return data[0]
	}
	return nil
}

// synth is the minimal fallback object for a model whose backend has never been reached.
func synth(t target) map[string]any {
	return map[string]any{
		"id":       t.baseID(),
		"object":   "model",
		"created":  t.created,
		"owned_by": t.ownedBy,
		"root":     t.root,
	}
}

// discover lists targets from both sources. Errors are logged, not fatal: a failure of one source
// must not blank out the other.
func (s *server) discover(ctx context.Context) []target {
	var out []target
	out = append(out, s.discoverKServe(ctx)...)
	out = append(out, s.discoverSlurm(ctx)...)
	return out
}

// discoverKServe: one target per LLMInferenceService.
func (s *server) discoverKServe(ctx context.Context) []target {
	list, err := s.dyn.Resource(gvrLLMISVC).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Printf("list llminferenceservices: %v", err)
		return nil
	}
	var out []target
	for _, it := range list.Items {
		ns := it.GetNamespace()
		served, _, _ := unstructured.NestedString(it.Object, "spec", "model", "name")
		if served == "" {
			continue
		}
		prefix := s.cfg.publisherPref + "/" + ns + "/models/"
		out = append(out, target{
			key:     prefix,
			kind:    "kserve",
			prefix:  prefix,
			root:    served,
			ownedBy: "kserve",
			created: it.GetCreationTimestamp().Unix(),
			backend: s.kserveBackend(it.GetName(), ns),
		})
	}
	return out
}

// discoverSlurm: one target per slurm-models HTTPRoute. The route's X-Gateway-Model-Name value is
// the authoritative client-facing id; the paired AgentgatewayBackend gives host:port + real model.
func (s *server) discoverSlurm(ctx context.Context) []target {
	routes, err := s.dyn.Resource(gvrRoute).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: s.cfg.slurmSelector})
	if err != nil {
		log.Printf("list slurm httproutes: %v", err)
		return nil
	}
	var out []target
	for _, rt := range routes.Items {
		ns := rt.GetNamespace()
		id := routeModelHeader(rt.Object)
		if id == "" {
			continue
		}
		host, port, root := s.slurmBackend(ctx, ns, routeBackendName(rt.Object))
		backend := ""
		if host != "" && port != "" {
			backend = host + ":" + port
		}
		out = append(out, target{
			key:     id,
			kind:    "slurm",
			fixedID: id,
			root:    root,
			ownedBy: "slurm",
			created: rt.GetCreationTimestamp().Unix(),
			backend: backend,
		})
	}
	return out
}

// kserveBackend renders the configured DNS template to host:port for a KServe-served model.
func (s *server) kserveBackend(name, ns string) string {
	host := strings.NewReplacer("{name}", name, "{namespace}", ns).Replace(s.cfg.kserveSvcTmpl)
	return host + ":" + s.cfg.backendPort
}

// slurmBackend resolves an AgentgatewayBackend to (host, port, realModelName).
func (s *server) slurmBackend(ctx context.Context, ns, name string) (host, port, root string) {
	if name == "" {
		return "", "", ""
	}
	be, err := s.dyn.Resource(gvrBackend).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.Printf("get agentgatewaybackend %s/%s: %v", ns, name, err)
		return "", "", ""
	}
	host, _, _ = unstructured.NestedString(be.Object, "spec", "ai", "provider", "host")
	if p, ok, _ := unstructured.NestedInt64(be.Object, "spec", "ai", "provider", "port"); ok {
		port = strconv.FormatInt(p, 10)
	}
	root, _, _ = unstructured.NestedString(be.Object, "spec", "ai", "provider", "openai", "model")
	return host, port, root
}

// routeModelHeader extracts the X-Gateway-Model-Name header value from an HTTPRoute's matches.
func routeModelHeader(obj map[string]any) string {
	rules, _, _ := unstructured.NestedSlice(obj, "spec", "rules")
	for _, r := range rules {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		matches, _, _ := unstructured.NestedSlice(rm, "matches")
		for _, m := range matches {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			headers, _, _ := unstructured.NestedSlice(mm, "headers")
			for _, h := range headers {
				hm, ok := h.(map[string]any)
				if !ok {
					continue
				}
				if name, _, _ := unstructured.NestedString(hm, "name"); name == bbrHeader {
					if v, _, _ := unstructured.NestedString(hm, "value"); v != "" {
						return v
					}
				}
			}
		}
	}
	return ""
}

// routeBackendName returns the first AgentgatewayBackend backendRef name on an HTTPRoute.
func routeBackendName(obj map[string]any) string {
	rules, _, _ := unstructured.NestedSlice(obj, "spec", "rules")
	for _, r := range rules {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		refs, _, _ := unstructured.NestedSlice(rm, "backendRefs")
		for _, ref := range refs {
			rf, ok := ref.(map[string]any)
			if !ok {
				continue
			}
			if name, _, _ := unstructured.NestedString(rf, "name"); name != "" {
				return name
			}
		}
	}
	return ""
}

func asString(v any) string { s, _ := v.(string); return s }

type aggError string

func (e aggError) Error() string { return string(e) }

const errNoBackend = aggError("no reachable backend")
