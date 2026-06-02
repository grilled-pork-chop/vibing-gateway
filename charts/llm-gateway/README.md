# llm-gateway

The shared LLM ingress — **deploy once**. Renders the AgentGateway `Gateway` (labelled
`serving.kserve.io/gateway` so KServe-managed HTTPRoutes attach to it), an optional
cert-manager TLS `Certificate`, and the native Body-Based Routing `AgentgatewayPolicy`.
Standalone chart, no dependencies.

## What it installs

| Resource                              | When                       | Purpose                                                                                   |
| ------------------------------------- | -------------------------- | ----------------------------------------------------------------------------------------- |
| `Gateway` (gateway.networking.k8s.io) | always                     | Ingress entrypoint on the `agentgateway` GatewayClass; KServe routes attach here          |
| `AgentgatewayPolicy`                  | `bbr.enabled` (default on) | PreRouting transform — stamps `X-Gateway-Model-Name` from the request body (no extra pod) |
| `Certificate` (cert-manager)          | `tls.enabled`              | TLS cert for the HTTPS listener, written to `tls.secretName`                              |

KServe derives the `InferencePool`, `EndpointPicker`, and `HTTPRoute` from each
`LLMInferenceService` — this chart does **not** create them.

## Install

```bash
helm upgrade -i platform-gateway ./charts/llm-gateway -f values/values-local.yaml
```

Deploy a single release; every model's HTTPRoute attaches to this one Gateway.

## Parameters

| Key                                  | Default                        | Description                                                                      |
| ------------------------------------ | ------------------------------ | -------------------------------------------------------------------------------- |
| `namespace`                          | `kserve`                       | Namespace where the Gateway is created (must be a namespace KServe watches)      |
| `className`                          | `agentgateway`                 | GatewayClass name (auto-registered by the AgentGateway controller)               |
| `gatewayName`                        | `kserve-ingress-gateway`       | Gateway name; KServe HTTPRoutes attach via the `serving.kserve.io/gateway` label |
| `hostname`                           | `llm.local`                    | Hostname for HTTPS listeners and the TLS Certificate dnsNames                    |
| `listeners`                          | `[{http,80}]`                  | Listener list on the Gateway (HTTP/HTTPS)                                        |
| `tls.enabled`                        | `false`                        | Add a 443 HTTPS listener + request a cert-manager Certificate                    |
| `tls.issuerRef.name` / `.kind`       | `selfsigned` / `ClusterIssuer` | cert-manager issuer that signs the certificate                                   |
| `tls.secretName`                     | `llm-gateway-tls`              | Secret the cert is written to and the listener reads from                        |
| `bbr.enabled`                        | `true`                         | Render the Body-Based Routing AgentgatewayPolicy                                 |
| `commonLabels` / `commonAnnotations` | `{}`                           | Added to every resource                                                          |

## Notes

- **Gateway binding.** `namespace` + `gatewayName` together MUST equal the `control-plane`
  value `kserve-llmisvc-resources.kserve.controller.gateway.ingressGateway.kserveGateway`
  (default `kserve/kserve-ingress-gateway`) — that is the Gateway the KServe-managed router
  attaches its HTTPRoute to. Change both in lockstep.
- **TLS prerequisite.** `tls.issuerRef` must point at an Issuer/ClusterIssuer that already
  exists; this chart does not create one.
- **BBR** is a no-op header with a single model; it becomes load-bearing for multi-model
  fanout, where HTTPRoute rules match on `X-Gateway-Model-Name`.
