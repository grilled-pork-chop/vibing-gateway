# foundation

Bootstraps the LLM platform's core infrastructure: **cert-manager** and **every cluster CRD**
the stack needs. It is the single owner of all CRDs — the Gateway API and GIE CRDs are
vendored as Helm-owned templates, and the KServe + AgentGateway CRDs come from pinned
dependency charts. Install this **first**.

## What it installs

| Component                       | Version       | Source                                       |
| ------------------------------- | ------------- | -------------------------------------------- |
| Gateway API CRDs                | `v1.4.1`      | vendored (`templates/gateway-api-crds.yaml`) |
| GIE (Inference Extension) CRDs  | `v1.3.1`      | vendored (`templates/gie-crds.yaml`)         |
| cert-manager                    | `v1.17.0`     | dependency (`oci://quay.io/jetstack/charts`) |
| KServe CRDs                     | `v0.19.0-rc0` | dependency (`kserve-crd`)                    |
| KServe LLMInferenceService CRDs | `v0.19.0-rc0` | dependency (`kserve-llmisvc-crd`)            |
| AgentGateway CRDs               | `v1.2.1`      | dependency (`agentgateway-crds`)             |

## Install

```bash
helm dependency update ./charts/foundation
helm upgrade -i foundation ./charts/foundation -f values/values-local.yaml --wait
```

`--wait` matters: cert-manager must be Ready and the CRDs Established before the
`control-plane` chart installs.

## Parameters

| Key                          | Default | Description                                        |
| ---------------------------- | ------- | -------------------------------------------------- |
| `gatewayApiCRDs.enabled`     | `true`  | Render the vendored Gateway API CRDs               |
| `gieCRDs.enabled`            | `true`  | Render the vendored GIE CRDs                       |
| `cert-manager.enabled`       | `true`  | Install cert-manager (required by KServe webhooks) |
| `cert-manager.crds.enabled`  | `true`  | Install cert-manager's own CRDs                    |
| `kserve-crd.enabled`         | `true`  | Install the KServe (InferenceService) CRD chart    |
| `kserve-llmisvc-crd.enabled` | `true`  | Install the KServe LLMInferenceService CRD chart   |
| `agentgateway-crds.enabled`  | `true`  | Install the AgentGateway CRD chart                 |

## Notes

- **Single CRD owner.** Because foundation owns the GIE CRDs (vendored, Helm-managed) and the
  `control-plane` chart sets `createGIECRDs=false`, the GIE CRDs are never double-shipped — no
  dual-ownership and no `kubectl label/annotate` hack. Requires KServe `v0.19.0-rc0+`.
- These versions are dictated by KServe `v0.19.0-rc0` (GIE `v1.3.1` matches its bundled
  version). Chart versions are pinned in `Chart.yaml`.
