# llm-platform

A monorepo of four **standalone** Helm charts for an LLM serving platform — **AgentGateway**
(Gateway API) + native **Body-Based Routing** + **KServe `LLMInferenceService`**. Runs locally
on **kind** with no GPU (privileged CPU vLLM, `hf://facebook/opt-125m`); use
`values/values-prod.yaml` (`gpu.enabled=true`) for real vLLM on GPU nodes.

## Charts

| Chart                  | Installs                                                                                                                                   |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `charts/foundation`    | cert-manager + **all CRDs** (Gateway API & GIE vendored/Helm-owned; `kserve-crd`, `kserve-llmisvc-crd`, `agentgateway-crds` as deps)       |
| `charts/control-plane` | `agentgateway` (Inference Extension on) + `kserve-llmisvc-resources` (`createGIECRDs=false`) + `kserve-runtime-configs` (+ optional `lws`) |
| `charts/llm-gateway`   | Gateway + optional TLS cert + BBR `AgentgatewayPolicy` — **deploy once**                                                                   |
| `charts/model-server`  | one `LLMInferenceService` — **deploy once per model**; KServe derives the InferencePool/EPP/HTTPRoute                                      |

`foundation` and `control-plane` are wrapper charts (workloads are pinned OCI subcharts);
`llm-gateway` and `model-server` are dependency-free leaf charts. All four take the same shared
overlay (`values/values-{local,prod}.yaml`) and read only the keys they define.

## Quick start (local, no GPU)

```bash
make kind-create
make deps           # helm dependency update for foundation + control-plane
make install-all    # foundation → control-plane → gateway → one model, ordered
make smoke          # POST a completion through the Gateway
```

Equivalent raw commands (all sharing one overlay):

```bash
helm upgrade -i foundation       ./charts/foundation     -f values/values-local.yaml --wait
helm upgrade -i control-plane    ./charts/control-plane  -f values/values-local.yaml --wait
helm upgrade -i platform-gateway ./charts/llm-gateway    -f values/values-local.yaml          # once
helm upgrade -i model-opt        ./charts/model-server   -f values/values-local.yaml --set servedModelName=facebook/opt-125m
```

Each model is its own release — the `LLMInferenceService` (and its route path) is named after
the release:

```bash
helm upgrade -i model-phi ./charts/model-server -f values/values-local.yaml --set servedModelName=microsoft/phi-3-mini
# or: make model MODEL=microsoft/phi-3-mini RELEASE=model-phi
```

### Request Examples & Routing Behavior

Ensure you have the gateway exposed in a separate terminal before running these examples:

```bash
make port-forward

```

#### Example 1: Dedicated Path-based Routing

Routes via the model's explicit release path (`/<namespace>/<release>/v1/...`). This address goes straight to the specific workload service and natively accepts the clean, short model name in the payload.

```bash
curl -sS -X POST http://localhost:8080/llm-demo/model-opt/v1/completions \
     -H 'Content-Type: application/json' \
     -d '{
        "model": "facebook/opt-125m",
        "prompt": "Who are you?"
      }'

```

#### Example 2: Global Root Path (Body-Based Routing)

Routes via the root multi-model endpoint (`/v1/...`). This relies on the Gateway's **Body-Based Routing (BBR)** policy, which dynamically inspects the incoming JSON payload, extracts the `model` field, and stamps it as an `X-Gateway-Model-Name` header to fan out traffic to the correct backend `InferencePool`.

Because this endpoint is shared globally across multiple tenants, you must use the fully-qualified model name to prevent routing collisions:

```bash
curl -X POST http://localhost:8080/v1/completions \
     -H "Content-Type: application/json" \
     -d '{
        "model": "publishers/llm-demo/models/facebook/opt-125m",
        "prompt": "Who are you?"
     }'

```

### SLURM / external models

Models served **outside the cluster** (e.g. an OpenAI-compatible vLLM on a SLURM node) join the same
`/v1` endpoint via the `slurm-models` chart — each is a host + port + model name, no KServe involved.
List them in `values/slurm-models.yaml` and deploy:

```bash
make slurm        # helm upgrade -i slurm-models ./charts/slurm-models -f values/slurm-models.yaml
```

Address them with their fully-qualified name (the backend rewrites it to the real model name
upstream):

```bash
curl -X POST http://localhost:8080/v1/completions \
     -H "Content-Type: application/json" \
     -d '{
        "model": "publishers/slurm/models/llama3-70b",
        "prompt": "Who are you?"
     }'
```

When a SLURM node/port changes, edit its entry in `values/slurm-models.yaml` and re-run `make slurm`.
The Gateway proxy pods must be able to reach + resolve the SLURM hosts; see
`charts/slurm-models/README.md` for reachability/auth notes.

### List models (`GET /v1/models`)

The `llm-gateway` chart deploys a small **models-aggregator** (`modelsEndpoint.enabled`, on by
default) that serves one OpenAI-compatible `/v1/models` listing **every** model on the gateway —
both KServe `LLMInferenceService`s and the external SLURM backends — discovered live from the
Kubernetes API (no static list to maintain). It polls each backend's own `/v1/models` in the
background and **saves the full response**, so the listing carries all of vLLM's fields and stays
served from cache even if a backend is briefly down:

```bash
curl -sS http://localhost:8080/v1/models | jq
```

```json
{
  "object": "list",
  "data": [
    {
      "id": "publishers/llm-demo/models/facebook/opt-125m",
      "object": "model",
      "created": 1717286400,
      "owned_by": "kserve",
      "root": "facebook/opt-125m",
      "max_model_len": 2048,
      "permission": [ { "id": "modelperm-...", "object": "model_permission" } ]
    },
    { "id": "publishers/slurm/models/llama3-70b", "object": "model", "owned_by": "slurm", "root": "meta-llama/Llama-3-70B" }
  ]
}
```

Only each `id` is rewritten to the **fully-qualified routing key** — copy one straight into the
`model` field of a `/v1/chat/completions` or `/v1/completions` request to the BBR `/v1` endpoint
above. The aggregator image is built/published with `make aggregator-image` and pinned in
`images.txt`.

## Versions (pinned)

| Component                             | Version       |
| ------------------------------------- | ------------- |
| cert-manager                          | `v1.17.0`     |
| Gateway API                           | `v1.4.1`      |
| GIE (Gateway API Inference Extension) | `v1.3.1`      |
| agentgateway                          | `v1.2.1`      |
| KServe                                | `v0.19.0-rc0` |
| LWS (optional)                        | `v0.8.0`      |

These versions are dictated by **KServe `v0.19.0-rc0`** — it pins the compatible Gateway API,
cert-manager, GIE, and LWS versions (GIE `v1.3.1` matches its bundled version). Gateway API +
GIE CRDs are vendored in `charts/foundation/templates`; chart versions are pinned in each `Chart.yaml`.

## Observability

Telemetry ships as a fifth chart (`monitoring`): Prometheus + Alertmanager + Grafana, scraping
vLLM and agentgateway for **per-model / per-pod / per-LLMInferenceService** request and token usage,
with Grafana dashboards ("LLM Usage", "Usage by user", "Platform health") and `PrometheusRule`
alerts. It is on by default in the shared overlays (one switch: `monitoring.enabled`) and installed
in order by `make install-all`. See **[TELEMETRY.md](TELEMETRY.md)** for the full reference, the
per-user `X-User` attribution, and how to reach Grafana/Prometheus.

## Must-do before production
- AuthPolicy + token rate limit on the route (an open OpenAI endpoint gets scanned in days). Also
  upgrades per-user usage from best-effort header attribution to enforceable identity.
- A real `ClusterIssuer` for TLS (`llm-gateway` `tls.issuerRef` points at one but nothing creates it).
- Per-component `ServiceAccount`s; PDB for the EPP. (Telemetry — `PrometheusRule` alerts + Grafana
  dashboards — now ships in the `monitoring` chart; see [TELEMETRY.md](TELEMETRY.md).)
- vLLM image pre-pull DaemonSet (first pod otherwise blocks on image + weight load).
