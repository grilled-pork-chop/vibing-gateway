package discovery

import (
	"context"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/llm-gateway/models-aggregator/internal/model"
)

// discoverSlurm builds one target per slurm-models HTTPRoute. The route's X-Gateway-Model-Name value
// is the authoritative client-facing id; the paired AgentgatewayBackend gives host:port + the real
// model name. Each target advertises exactly that one id (adapters are not separately routable).
func (k *K8s) discoverSlurm(ctx context.Context) []model.Target {
	routes, err := k.dyn.Resource(gvrRoute).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: k.slurmSelector})
	if err != nil {
		k.log.Error("list slurm httproutes", "err", err)
		return nil
	}
	var out []model.Target
	for _, rt := range routes.Items {
		id := routeModelHeader(rt.Object)
		if id == "" {
			continue
		}
		host, port, root := k.slurmBackend(ctx, rt.GetNamespace(), routeBackendName(rt.Object))
		backend := ""
		if host != "" && port != "" {
			backend = host + ":" + port
		}
		out = append(out, model.Target{
			Key:     id,
			Backend: backend,
			BaseID:  id,
			Root:    root,
			OwnedBy: "slurm",
			Created: rt.GetCreationTimestamp().Unix(),
			Rewrite: model.FixedRewriter(id, root),
		})
	}
	return out
}

// slurmBackend resolves an AgentgatewayBackend to (host, port, realModelName).
func (k *K8s) slurmBackend(ctx context.Context, ns, name string) (host, port, root string) {
	if name == "" {
		return "", "", ""
	}
	be, err := k.dyn.Resource(gvrBackend).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		k.log.Error("get agentgatewaybackend", "namespace", ns, "name", name, "err", err)
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
