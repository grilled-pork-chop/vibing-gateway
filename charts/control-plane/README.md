# control-plane

Installs the LLM platform controllers that reconcile the data plane: the **AgentGateway**
controller (with the Gateway API Inference Extension enabled) and the **KServe
LLMInferenceService** controller plus its runtime configs. Generative-only. Install **after**
`foundation`.

## What it installs

| Component                  | Version       | Purpose                                                                                |
| -------------------------- | ------------- | -------------------------------------------------------------------------------------- |
| `agentgateway`             | `v1.2.1`      | AgentGateway controller + Envoy data plane; Inference Extension consumes InferencePool |
| `kserve-llmisvc-resources` | `v0.19.0-rc0` | KServe LLMInferenceService controller (generative); `createGIECRDs=false`              |
| `kserve-runtime-configs`   | `v0.19.0-rc0` | `ClusterServingRuntimes` / `LLMInferenceServiceConfigs`                                |

## Install

```bash
helm dependency update ./charts/control-plane
helm upgrade -i control-plane ./charts/control-plane -f values/values-local.yaml --wait
kubectl rollout status deploy/llmisvc-controller-manager -n <namespace> --timeout=300s
```

`--wait` plus the rollout gate ensure `llmisvc-controller-manager` is Available before you
deploy the gateway and models.

## Parameters

Key toggles and production knobs (see `values.yaml` for the full set, including commented
Tier 2/3 monitoring and autoscaling options):

| Key                                                                               | Default                         | Description                                                                  |
| --------------------------------------------------------------------------------- | ------------------------------- | ---------------------------------------------------------------------------- |
| `agentgateway.enabled`                                                            | `true`                          | Install the AgentGateway controller                                          |
| `agentgateway.inferenceExtension.enabled`                                         | `true`                          | Enable the Gateway API Inference Extension                                   |
| `agentgateway.controller.replicaCount`                                            | `2`                             | Controller replicas (stateless xDS; HA failover)                             |
| `agentgateway.resources`                                                          | `250m/256Mi` → `2/512Mi`        | Requests/limits — also drives `GOMEMLIMIT`/`GOMAXPROCS` via the Downward API |
| `kserve-llmisvc-resources.enabled`                                                | `true`                          | Install the LLMInferenceService controller                                   |
| `kserve-llmisvc-resources.kserve.llmisvc.createGIECRDs`                           | `false`                         | `platform-crds` owns the GIE CRDs (single owner)                            |
| `kserve-llmisvc-resources.kserve.llmisvc.controller.replicas`                     | `2`                             | Controller replicas (leader-elect HA)                                        |
| `kserve-llmisvc-resources.kserve.controller.gateway.ingressGateway.kserveGateway` | `kserve/kserve-ingress-gateway` | `<ns>/<name>` of the Gateway the managed router binds to                     |
| `kserve-runtime-configs.enabled`                                                  | `true`                          | Install runtime configs                                                      |
| `kserve-runtime-configs.kserve.servingruntime.enabled`                            | `false`                         | Predictive runtimes off (generative only)                                    |

> Multi-node serving via **LeaderWorkerSet** (`lws.enabled`) now installs with the `foundation`
> chart (cluster-prerequisite tier), not here.

## Notes

- **Generative only.** Uses `kserve-llmisvc-resources` (not the predictive `kserve-resources`)
  with `createGIECRDs=false`, so the GIE CRDs come from `platform-crds` rather than being
  re-shipped here. Requires KServe `v0.19.0-rc0+`.
- **Gateway binding.** `kserve-llmisvc-resources.kserve.controller.gateway.ingressGateway.kserveGateway`
  MUST equal the `llm-gateway` chart's `<namespace>/<gatewayName>` (default
  `kserve/kserve-ingress-gateway`) — that is the Gateway the KServe-managed HTTPRoute attaches
  to. Change both in lockstep.
- `agentgateway.resources` is not just QoS: empty limits let the Go runtime size itself to the
  whole node, so explicit limits are set by default.
