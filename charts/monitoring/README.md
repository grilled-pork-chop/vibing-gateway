# monitoring

Bundled telemetry for the LLM platform — **Prometheus + Alertmanager + Grafana + the Prometheus
Operator** (via the vendored `kube-prometheus-stack` subchart), plus this repo's Grafana dashboards
and `PrometheusRule` alerts.

Deploy **once**, after `control-plane`:

```bash
make monitoring                 # ENV=local|prod
# or: helm upgrade -i monitoring ./charts/monitoring -n kserve -f values/values-local.yaml
```

- **CRDs**: this chart owns the Prometheus Operator CRDs (`kube-prometheus-stack.crds.enabled=true`).
  They're version-locked to the operator, so they upgrade in lockstep with the stack — and stay out
  of `foundation`'s near-full release. That's why `monitoring` installs **before** `control-plane`.
- **Scrape config** lives next to each workload: `model-server` ships a vLLM `PodMonitor`,
  `control-plane` enables agentgateway ServiceMonitors. Prometheus discovers them cluster-wide.
- **Dashboards** in `dashboards/*.json` become ConfigMaps the Grafana sidecar auto-imports.
- **Alerts** render from `templates/prometheusrule.yaml` (thresholds in `monitoring.alerts.*`).

Master switch: `monitoring.enabled` (shared overlay) — gates this chart's dashboards/alerts and the
vLLM PodMonitor together. Full reference, including per-user `X-User` attribution and how to reach
Grafana/Prometheus: [`../../TELEMETRY.md`](../../TELEMETRY.md).
