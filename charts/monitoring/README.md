# monitoring

Bundled telemetry for the LLM platform — **Prometheus + Alertmanager + Grafana + the Prometheus
Operator** (via the vendored `kube-prometheus-stack` subchart), plus this repo's Grafana dashboards
and `PrometheusRule` alerts.

Deploy **once**, after `control-plane`:

```bash
make monitoring                 # ENV=local|prod
# or: helm upgrade -i monitoring ./charts/monitoring -n kserve -f values/values-local.yaml
```

- **CRDs** come from the `foundation` chart (`prometheus-operator-crds`); this chart sets
  `kube-prometheus-stack.crds.enabled=false` so there is a single CRD owner.
- **Scrape config** lives next to each workload: `model-server` ships a vLLM `PodMonitor`,
  `control-plane` enables agentgateway ServiceMonitors. Prometheus discovers them cluster-wide.
- **Dashboards** in `dashboards/*.json` become ConfigMaps the Grafana sidecar auto-imports.
- **Alerts** render from `templates/prometheusrule.yaml` (thresholds in `monitoring.alerts.*`).

Master switch: `monitoring.enabled` (shared overlay) — gates this chart's dashboards/alerts and the
vLLM PodMonitor together. Full reference, including per-user `X-User` attribution and how to reach
Grafana/Prometheus: [`../../TELEMETRY.md`](../../TELEMETRY.md).
