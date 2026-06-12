# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A monorepo of **five standalone Helm charts** plus one small Go service that together stand up an
LLM serving platform: AgentGateway (Gateway API) + native Body-Based Routing (BBR) + KServe
`LLMInferenceService`, with an optional bundled telemetry stack. Runs locally on `kind` with no GPU;
`values/values-prod.yaml` switches to real GPU vLLM. See `README.md` for the user-facing walkthrough,
`TELEMETRY.md` for observability, and `INSTALL.md` for the offline/airgapped path.

## Common commands

Everything is driven through the `Makefile` (uses `--kube-context kind-$(CLUSTER_NAME)`). `ENV=local`
(default) or `ENV=prod` selects the shared overlay `values/values-$(ENV).yaml`.

```bash
make kind-create deps install-all smoke   # full local bootstrap, ordered
make lint        # helm lint every chart with the shared overlay
make template    # render every chart (use to inspect generated manifests)
make model MODEL=microsoft/phi-3-mini RELEASE=model-phi   # add another model (one release per model)
make slurm       # (re)deploy external SLURM backends from values/slurm-models.yaml
make smoke       # port-forward Gateway + POST a completion through it
make uninstall-all / make clean            # teardown / teardown + delete cluster
```

`make install-all` runs `foundation → control-plane → gateway → model` **in that order** — the order
is load-bearing (CRDs and the gateway must exist before workloads). Don't reorder.

models-aggregator (Go, in `models-aggregator/`):

```bash
cd models-aggregator && go test ./...        # unit tests (each internal/* has a _test.go)
cd models-aggregator && go test ./internal/poller/   # single package
make aggregator-image                        # docker build + push (PUSH=0 to build only)
```

Offline bundle: `make package` builds a self-contained `dist/*.tar.zst` (images + charts + values +
docs). `make images-verify` is a drift guard that cross-checks `images.txt` against a live `helm
template` of all charts — run it after changing any pinned image.

## Architecture

### The five charts (`charts/`)

| Chart | Role | Cardinality |
| --- | --- | --- |
| `foundation` | cert-manager + **all platform CRDs** (Gateway API & GIE vendored in `templates/`; kserve/agentgateway CRDs as deps) | once |
| `control-plane` | agentgateway controller + KServe `llmisvc` controllers (+ optional LWS) | once |
| `monitoring` | bundled telemetry: Prometheus + Alertmanager + Grafana (kube-prometheus-stack) + platform dashboards/alerts | **deploy once** |
| `llm-gateway` | the shared `Gateway` + optional TLS cert + **BBR `AgentgatewayPolicy`** + the models-aggregator | **deploy once** |
| `model-server` | one `LLMInferenceService` (+ vLLM `PodMonitor`); KServe derives the InferencePool/EPP/HTTPRoute from it | **once per model** |
| `slurm-models` | `AgentgatewayBackend` + HTTPRoute per out-of-cluster OpenAI server | once, list-driven |

`foundation`/`control-plane`/`monitoring` are **wrapper charts** (real workloads are pinned OCI/HTTP
subcharts, vendored into `charts/*/charts/` by `make deps`). `llm-gateway`/`model-server`/`slurm-models`
are dependency-free leaf charts. Telemetry is documented in `TELEMETRY.md`. (One CRD-ownership
exception: the Prometheus Operator CRDs live in `monitoring`, not `foundation` — they're version-
locked to the operator and large, so they ride with `kube-prometheus-stack`.) **All charts read the same shared overlay** and each consumes only the
keys it defines (Helm ignores the rest) — that's why one `values/values-{local,prod}.yaml` feeds all of
them. This repo is intentionally **not** an umbrella chart: there is no root `Chart.yaml`, no
`global`-everything, no shared lib chart. Keep the charts standalone.

### Two routing paths (important for understanding requests)

1. **Path-based** `/<ns>/<release>/v1/...` → straight to one workload; accepts the short model name.
2. **Body-based (BBR)** `/v1/...` → the BBR `AgentgatewayPolicy` inspects the JSON body, extracts
   `model`, and stamps it as the `X-Gateway-Model-Name` header to fan out to the right
   `InferencePool`. On this shared endpoint you must use the **fully-qualified** id
   `publishers/<ns-or-publisher>/models/<name>` to avoid collisions.

### models-aggregator (`models-aggregator/`)

A small Go service (Deployed by the `llm-gateway` chart) that serves one OpenAI-compatible
`GET /v1/models` for the whole gateway — neither BBR path can answer a bodyless GET on its own. Clean
layering, client-go isolated to one package:

- `internal/discovery` — the **only** package depending on client-go; reads the K8s API
  (`LLMInferenceService`s + slurm `HTTPRoute`/`AgentgatewayBackend`s) and produces `model.Target`s with
  their rewrite strategy. No static model list.
- `internal/poller` — background loop; for each target calls `probe` and writes to `store`.
- `internal/probe` — fetches each backend's own `/v1/models`.
- `internal/store` — in-memory last-known-good cache per model; `GET /v1/models` (in `internal/httpapi`)
  serves the merged union straight from cache, so listing latency is constant and survives a backend
  being briefly down. Every backend field is passed through unchanged; **only each `id` is rewritten**
  to the fully-qualified BBR routing key.
- `internal/config` — all runtime config comes from env vars (wired by the chart's Deployment); see the
  struct in `config.go` for the full list and defaults.

## Versions are pinned and coupled

Component versions (cert-manager, Gateway API, GIE, agentgateway, KServe, LWS) are dictated by **KServe
`v0.19.0-rc0`** and pinned in each `Chart.yaml` + `images.txt`. Don't bump one in isolation — see the
version table in `README.md`.
