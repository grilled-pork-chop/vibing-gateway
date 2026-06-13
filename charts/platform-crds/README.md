# platform-crds

The single owner of every cluster CRD the LLM platform needs **except** those an operator
packages and installs itself. Install this **first**, before `foundation`.

It bundles the **Gateway API** and **GIE** CRDs as Helm-owned templates (vendored), and pulls
the **KServe LLMInferenceService** and **AgentGateway** CRDs as pinned dependency charts — those
operators run in `control-plane`, so their CRDs belong here, not with the operator.

## Why this chart exists

CRD ownership follows one rule, exceptionless:

- **Vendored raw-YAML CRDs**, and **CRD-only subcharts whose operator runs in a *different*
  release** → here (`platform-crds`).
- **CRDs an operator packages *and* installs in the same release** → ride with the operator:
  cert-manager CRDs stay in `foundation`, Prometheus Operator CRDs stay in `monitoring`.

Splitting these out of `foundation` keeps foundation's release Secret small (the vendored CRDs are
~750 KB, near Helm's ~1 MB per-release limit) and means `helm uninstall foundation` can never delete
the Gateway API / GIE / KServe / AgentGateway CRDs (and every CR using them) — those CRDs outlive
cert-manager.

## What it installs

| Component                       | Version       | Source                                       |
| ------------------------------- | ------------- | -------------------------------------------- |
| Gateway API CRDs                | `v1.4.1`      | vendored (`templates/gateway-api-crds.yaml`) |
| GIE (Inference Extension) CRDs  | `v1.3.1`      | vendored (`templates/gie-crds.yaml`)         |
| KServe LLMInferenceService CRDs | `v0.19.0-rc0` | dependency (`kserve-llmisvc-crd`)            |
| AgentGateway CRDs               | `v1.2.1`      | dependency (`agentgateway-crds`)             |

## Install

```bash
helm dependency update ./charts/platform-crds
helm upgrade -i platform-crds ./charts/platform-crds -f values/values-local.yaml --wait
```

`--wait` matters: the CRDs must be Established before `control-plane` or any workload renders a CR.

## Parameters

| Key                          | Default | Description                                      |
| ---------------------------- | ------- | ------------------------------------------------ |
| `gatewayApiCRDs.enabled`     | `true`  | Render the vendored Gateway API CRDs             |
| `gieCRDs.enabled`            | `true`  | Render the vendored GIE CRDs                     |
| `kserve-llmisvc-crd.enabled` | `true`  | Install the KServe LLMInferenceService CRD chart |
| `agentgateway-crds.enabled`  | `true`  | Install the AgentGateway CRD chart               |

## Notes

- **Single CRD owner.** Because this chart owns the GIE CRDs and `control-plane` sets
  `createGIECRDs=false`, the GIE CRDs are never double-shipped — no dual-ownership, no
  `kubectl label/annotate` hack. Requires KServe `v0.19.0-rc0+`.
- These versions are dictated by KServe `v0.19.0-rc0` (GIE `v1.3.1` matches its bundled version)
  and pinned in `Chart.yaml`.
