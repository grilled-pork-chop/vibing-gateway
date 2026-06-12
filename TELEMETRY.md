# Telemetry

Observability for the LLM platform: **per-model / per-pod / per-LLMInferenceService** request and
token usage, surfaced in Grafana with Prometheus-backed alerts. This page covers what gets
installed, how it works, how to reach it, and how to configure it.

> The whole stack is opt-in behind a single switch — `monitoring.enabled` in the shared overlay.

## What gets installed

| Piece | Where | What it is |
| --- | --- | --- |
| Prometheus Operator **CRDs** | `monitoring` chart (`kube-prometheus-stack`, `crds.enabled=true`) | `ServiceMonitor`, `PodMonitor`, `PrometheusRule`, `Prometheus`, `Alertmanager`, … — version-locked to the operator, so they live with it (not in `foundation`). |
| **Prometheus + Alertmanager + Grafana + operator** | `monitoring` chart (vendors `kube-prometheus-stack`) | The actual telemetry stack, plus kube-state-metrics and node-exporter. |
| Platform **dashboards** | `monitoring` chart (`dashboards/*.json` → ConfigMaps) | "LLM Usage", "Usage by user", "Platform health" — auto-loaded by the Grafana sidecar. |
| Platform **alerts** | `monitoring` chart (`PrometheusRule`) | error rate / latency / queue saturation / model-down / gateway-down. |
| vLLM **scrape** | `model-server` chart (`PodMonitor`) | scrapes every vLLM pod's `/metrics`. |
| Gateway **scrape** | `control-plane` chart (`agentgateway.monitoring`) | agentgateway controller + proxy ServiceMonitors + a dashboard. |
| EPP **scrape** (optional) | `monitoring` chart (`ServiceMonitor`, off by default) | KServe EndpointPicker scheduling metrics. |

Install order (handled by `make install-all`): `foundation → monitoring → control-plane → gateway → model`.
The `monitoring` release **owns the operator CRDs** and installs second (right after `foundation`),
so every `ServiceMonitor`/`PodMonitor`/`PrometheusRule` the later charts emit applies cleanly.

> Why not `foundation`? `foundation` carries the stable, slow-moving platform CRDs (Gateway API,
> GIE, cert-manager) and is already near Helm's per-release size limit. The Prometheus Operator CRDs
> are large *and* version-locked to the operator, so they ride with `kube-prometheus-stack` in the
> dedicated (optional) `monitoring` release — they upgrade in lockstep when the stack is bumped.

## How it works

```
              ┌──────────────┐   scrapes /metrics    ┌────────────────┐
   vLLM pods ─┤ PodMonitor   ├──────────────────────▶│                │
 (model-srv)  └──────────────┘                       │                │
              ┌──────────────┐                        │  Prometheus    │──▶ PrometheusRule (alerts)
 agentgateway─┤ ServiceMon.  ├───────────────────────▶│  (operator-    │        │
              └──────────────┘                        │   managed)     │        ▼
              ┌──────────────┐                        │                │   Alertmanager
   EPP (opt) ─┤ ServiceMon.  ├───────────────────────▶│                │
              └──────────────┘                        └───────┬────────┘
                                                              │ queries
                                                       ┌──────▼───────┐
                                                       │   Grafana    │  dashboards (sidecar-loaded)
                                                       └──────────────┘
```

The Prometheus Operator watches `ServiceMonitor`/`PodMonitor`/`PrometheusRule` objects
**cluster-wide** (the `monitoring` chart sets `*SelectorNilUsesHelmValues: false`), so each chart
ships its own scrape config next to the workload it owns. Grafana's sidecar imports any ConfigMap
labelled `grafana_dashboard: "1"`.

### Where the usage numbers come from

Almost all usage data is **already exported** — we just scrape it:

- **vLLM** exposes per-model Prometheus series on its metrics port. Scraping each pod gives:
  - per-**pod** breakdown — every pod is its own scrape target (`pod` label).
  - per-**model** breakdown — vLLM tags every series with `model_name`; the PodMonitor relabels it to `model`.
  - per-**LLMInferenceService** breakdown — a `relabeling` copies the KServe pod label into `llm_inference_service`.
- **agentgateway** exports gateway/route request counts and latency.

> ⚠️ The vLLM PodMonitor `selector` and the `llm_inference_service` relabel source are
> **KServe-owned pod labels** — confirm them on a live cluster and adjust
> `model-server` `values.monitoring.podMonitor` if they differ:
> `kubectl -n llm-demo get pod -l serving.kserve.io/llminferenceservice --show-labels`

## Metrics reference

| Series (vLLM) | Type | Used for |
| --- | --- | --- |
| `vllm:request_success_total` / `vllm:request_failure_total` | counter | request rate, error rate (per `model`) |
| `vllm:prompt_tokens_total` / `vllm:generation_tokens_total` | counter | **tokens/sec** (prompt & generation) per model / pod |
| `vllm:e2e_request_latency_seconds_bucket` | histogram | latency p99 |
| `vllm:time_to_first_token_seconds_bucket` | histogram | TTFT |
| `vllm:num_requests_running` / `vllm:num_requests_waiting` | gauge | in-flight load, queue saturation |

agentgateway request metrics feed the "Gateway" and "Usage by user" panels.

### Dashboards

- **LLM Usage** — requests/sec, prompt & generation tokens/sec, error %, latency p99, running/waiting
  gauges. Template variables: `model`, `service` (LLMInferenceService), `pod`.
- **Usage by user** — request rate per `user` (see below).
- **Platform health** — scrape-target `up`, down-by-job, controller restarts.

### Alerts (`PrometheusRule`, thresholds in `monitoring.alerts.*`)

| Alert | Fires when |
| --- | --- |
| `LLMHighErrorRate` | failure ratio > `errorRatePercent` (default 5%) for 10m |
| `LLMHighLatency` | e2e p99 > `latencyP99Seconds` (default 30s) for 10m |
| `LLMQueueSaturation` | waiting requests > `queueWaiting` (default 20) for 10m |
| `LLMNoModelPods` | a model deployment has 0 ready replicas for 5m |
| `GatewayDown` | no agentgateway target `up` for 5m |

## Per-user attribution

Usage can be broken down by a caller identity carried in a request header (default **`X-User`**).

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'X-User: alice' \
  -d '{"model":"publishers/llm-demo/models/facebook/opt-125m","messages":[{"role":"user","content":"hi"}]}'
```

The client header **passes through the gateway untouched** — there is no request-mutating policy
(an earlier draft stamped a default header on every request, which would have *clobbered* real
identities, so it was removed). The header name is recorded in `llm-gateway` values
(`usageAttribution.header`) and consumed by the "Usage by user" dashboard, which breaks usage down
by the `user` label.

> 🔓 **This is best-effort attribution, not authentication.** The header is client-supplied and
> spoofable. For enforceable per-user accounting, add an `AuthPolicy` (see the README must-do) so
> identity is verified, not asserted.

**Surfacing the header into Prometheus is a gateway-side setting**, not something this repo can
render: agentgateway must be configured to attach the request-header value as a metric label, after
which the "Usage by user" panels work directly off `agentgateway_requests_total{user=...}`. Confirm
that capability on the pinned `agentgateway v1.2.1`; if only **access logs** can carry the header
(plus the response token `usage`), point those panels at a logs source instead. The missing-value →
`unknown` default belongs in the PromQL/relabel **once the metric exists** — never by mutating the
request. Either way, putting a high-cardinality identity on a counter is an anti-pattern — keep the
set of users bounded (or move per-user token accounting to logs).

## How to access

```bash
# Grafana — default login: admin / prom-operator
kubectl -n kserve port-forward svc/monitoring-grafana 3000:80
#   → http://localhost:3000  → Dashboards → "LLM Usage"

# Prometheus — check scrape targets are UP
kubectl -n kserve port-forward svc/monitoring-kube-prometheus-st-prometheus 9090:9090
#   → http://localhost:9090/targets
```

(Service names are `<release>-...`; this repo installs the chart as release `monitoring`.)

## Configuration

| Key | Chart(s) | Effect |
| --- | --- | --- |
| `monitoring.enabled` | model-server + monitoring | **master switch** — vLLM PodMonitor + dashboards + alerts |
| `monitoring.interval` | model-server | vLLM scrape interval |
| `monitoring.podMonitor.*` | model-server | selector + relabelings for the KServe vLLM pods |
| `monitoring.alerts.*` | monitoring | alert thresholds |
| `monitoring.eppServiceMonitor.enabled` | monitoring | scrape the EPP (off by default) |
| `agentgateway.monitoring.enabled` | control-plane | gateway ServiceMonitors + dashboard |
| `usageAttribution.header` | llm-gateway | identity header name for the "Usage by user" dashboard |
| `kube-prometheus-stack.*` | monitoring | retention, storage, resources (subchart) |

**Local vs prod** (`values/values-{local,prod}.yaml`): local runs ephemeral with 6h retention;
prod adds PVCs (Prometheus 50Gi, Alertmanager/Grafana persistence), 15d retention, resource limits,
and enables the control-plane component scrapes kind doesn't expose. Set a real Alertmanager
receiver via `kube-prometheus-stack.alertmanager.config` before going live.

## Offline / air-gapped

The monitoring images are pinned in `images.txt` and bundled by `make package`. After bumping the
`kube-prometheus-stack` pin (`charts/monitoring/Chart.yaml`) run `make deps` then `make images-verify`
— it now renders the `monitoring` chart and cross-checks the rendered image set against `images.txt`,
so the pinned tags can't silently drift.
