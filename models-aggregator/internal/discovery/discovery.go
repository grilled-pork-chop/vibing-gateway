// Package discovery finds the models served through the gateway by reading the Kubernetes API, and
// turns each into a model.Target wired with the right rewrite strategy. It is the only package that
// depends on client-go, isolating that dependency from the core.
package discovery

import (
	"context"
	"log/slog"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/llm-gateway/models-aggregator/internal/model"
)

// GroupVersionResources we discover models from.
var (
	gvrLLMISVC = schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1alpha1", Resource: "llminferenceservices"}
	gvrRoute   = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	gvrBackend = schema.GroupVersionResource{Group: "agentgateway.dev", Version: "v1alpha1", Resource: "agentgatewaybackends"}
)

// bbrHeader is the routing key the Body-Based-Routing policy matches on; the SLURM route carries the
// client-facing id as this header's value.
const bbrHeader = "X-Gateway-Model-Name"

// K8s discovers models from KServe LLMInferenceServices and slurm-models HTTPRoutes.
type K8s struct {
	dyn             dynamic.Interface
	log             *slog.Logger
	publisherPrefix string
	backendPort     string
	slurmSelector   string
	kserveSvcTmpl   string
}

// New builds a discoverer. publisherPrefix/backendPort/slurmSelector/kserveSvcTmpl come from config.
func New(dyn dynamic.Interface, log *slog.Logger, publisherPrefix, backendPort, slurmSelector, kserveSvcTmpl string) *K8s {
	return &K8s{
		dyn:             dyn,
		log:             log,
		publisherPrefix: publisherPrefix,
		backendPort:     backendPort,
		slurmSelector:   slurmSelector,
		kserveSvcTmpl:   kserveSvcTmpl,
	}
}

// Discover returns the union of targets from both sources. A failure of one source is logged and
// does not blank out the other.
func (k *K8s) Discover(ctx context.Context) []model.Target {
	var out []model.Target
	out = append(out, k.discoverKServe(ctx)...)
	out = append(out, k.discoverSlurm(ctx)...)
	return out
}
