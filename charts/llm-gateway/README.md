# llm-gateway

The shared LLM ingress — **deploy once**. Renders the AgentGateway `Gateway` (labelled
`serving.kserve.io/gateway` so KServe-managed HTTPRoutes attach to it), an optional
cert-manager TLS `Certificate`, the native Body-Based Routing `AgentgatewayPolicy`, and an
optional guardrails `AgentgatewayPolicy`. Standalone chart, no dependencies.

## What it installs

| Resource                              | When                       | Purpose                                                                                   |
| ------------------------------------- | -------------------------- | ----------------------------------------------------------------------------------------- |
| `Gateway` (gateway.networking.k8s.io) | always                     | Ingress entrypoint on the `agentgateway` GatewayClass; KServe routes attach here          |
| `AgentgatewayPolicy` (BBR)            | `bbr.enabled` (default on) | PreRouting transform — stamps `X-Gateway-Model-Name` from the request body (no extra pod) |
| `AgentgatewayPolicy` (guardrails)     | `guardrails.enabled`       | `backend.ai.promptGuard` on the Gateway — masks PII, rejects banned prompts, prepends a system prompt |
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
| `guardrails.enabled`                 | `false`                        | Render the guardrails AgentgatewayPolicy (promptGuard on the Gateway)            |
| `guardrails.systemPrompt`            | safety instruction             | SYSTEM message prepended to every request (empty to skip)                        |
| `guardrails.pii.enabled` / `.action` | `true` / `Mask`                | Mask or Reject built-in sensitive-data patterns in request + response            |
| `guardrails.pii.builtins`            | `[Ssn,CreditCard,Email]`       | Built-ins: `Ssn`, `CreditCard`, `PhoneNumber`, `Email`, `CaSin`                  |
| `guardrails.reject.enabled`          | `true`                         | Reject requests whose prompt matches `guardrails.reject.matches`                 |
| `guardrails.reject.matches`          | `password` / `secret`          | Custom regex patterns that trigger a rejection                                   |
| `guardrails.reject.message` / `.statusCode` | rejection / `403`       | Response returned to the client on rejection                                     |
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
- **Guardrails** are opt-in and **routing-neutral**: the policy attaches `backend.ai.promptGuard`
  to the Gateway, so it covers every AI backend reached through it (both the dedicated
  `/<namespace>/<release>/v1` routes and the BBR global `/v1` fanout) without adding a backend or
  route. It only masks/rejects/enriches prompts and responses — standard inference is unaffected.
  PII masking, custom reject patterns, and the system prompt are active by default; OpenAI
  moderation, webhook, Bedrock Guardrails, and Google Model Armor are included as commented
  examples in `templates/guardrails-policy.yaml` (some need an external Secret/Service).
