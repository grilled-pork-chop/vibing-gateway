package discovery

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/llm-gateway/models-aggregator/internal/model"
)

// discoverKServe builds one target per LLMInferenceService. The advertised id of every model the
// backend reports (base + LoRA adapters) is the FQN prefix "<publisher>/<ns>/models/" + reported id.
func (k *K8s) discoverKServe(ctx context.Context) []model.Target {
	list, err := k.dyn.Resource(gvrLLMISVC).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		k.log.Error("list llminferenceservices", "err", err)
		return nil
	}
	var out []model.Target
	for _, it := range list.Items {
		ns := it.GetNamespace()
		served, _, _ := unstructured.NestedString(it.Object, "spec", "model", "name")
		if served == "" {
			continue
		}
		prefix := k.publisherPrefix + "/" + ns + "/models/"
		out = append(out, model.Target{
			Key:     prefix,
			Backend: k.kserveBackend(it.GetName(), ns),
			BaseID:  prefix + served,
			Root:    served,
			OwnedBy: "kserve",
			Created: it.GetCreationTimestamp().Unix(),
			Rewrite: model.PrefixRewriter(prefix),
		})
	}
	return out
}

// kserveBackend renders the configured DNS template to host:port for a KServe-served model.
func (k *K8s) kserveBackend(name, ns string) string {
	host := strings.NewReplacer("{name}", name, "{namespace}", ns).Replace(k.kserveSvcTmpl)
	return host + ":" + k.backendPort
}
