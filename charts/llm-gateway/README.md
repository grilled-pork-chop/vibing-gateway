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
| `AgentgatewayPolicy` (auth)           | `auth.enabled`             | `spec.traffic.apiKeyAuthentication` on the Gateway — requires a valid API key (from an out-of-band Secret) on every route |
| `Certificate` (cert-manager)          | `tls.enabled`              | TLS cert for the HTTPS listener, written to `tls.secretName`                              |
| models-aggregator `Deployment`/`Service`/RBAC + `HTTPRoute` | `modelsEndpoint.enabled` (default on) | Serves the unified OpenAI `GET /v1/models` (lists KServe + slurm-models) on an Exact `/v1/models` route |

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
| `infrastructure.annotations`         | `{}`                           | Annotations propagated onto the provisioned data-plane Service (MetalLB pool/IP on bare metal; max 8) |
| `tls.enabled`                        | `false`                        | Add a 443 HTTPS listener + request a cert-manager Certificate                    |
| `tls.issuerRef.name` / `.kind`       | `selfsigned` / `ClusterIssuer` | cert-manager issuer that signs the certificate                                   |
| `tls.secretName`                     | `llm-gateway-tls`              | Secret the cert is written to and the listener reads from                        |
| `auth.enabled`                       | `false`                        | Render the API-key AgentgatewayPolicy on the Gateway                            |
| `auth.mode`                          | `Strict`                       | Enforcement mode — `Strict` (key required), `Optional` (validate if present), `Permissive` (never reject) |
| `auth.secretName`                    | `llm-gateway-api-keys`         | Opaque Secret (created out-of-band) holding the valid API keys                   |
| `bbr.enabled`                        | `true`                         | Render the Body-Based Routing AgentgatewayPolicy                                 |
| `guardrails.enabled`                 | `false`                        | Render the guardrails AgentgatewayPolicy (promptGuard on the Gateway)            |
| `guardrails.systemPrompt`            | safety instruction             | SYSTEM message prepended to every request (empty to skip)                        |
| `guardrails.pii.enabled` / `.action` | `true` / `Mask`                | Mask or Reject built-in sensitive-data patterns in request + response            |
| `guardrails.pii.builtins`            | `[Ssn,CreditCard,Email]`       | Built-ins: `Ssn`, `CreditCard`, `PhoneNumber`, `Email`, `CaSin`                  |
| `guardrails.reject.enabled`          | `true`                         | Reject requests whose prompt matches `guardrails.reject.matches`                 |
| `guardrails.reject.matches`          | `password` / `secret`          | Custom regex patterns that trigger a rejection                                   |
| `guardrails.reject.message` / `.statusCode` | rejection / `403`       | Response returned to the client on rejection                                     |
| `modelsEndpoint.enabled`             | `true`                         | Render the models-aggregator + the Exact `/v1/models` HTTPRoute                  |
| `modelsEndpoint.image.*`             | `ghcr.io/llm-gateway/llm-models-aggregator:v0.1.0` | Aggregator image (must match the `images.txt` entry)         |
| `modelsEndpoint.backendPort`         | `8000`                         | OpenAI port the aggregator probes on in-cluster (KServe) backends                |
| `modelsEndpoint.publisherPrefix`     | `publishers`                   | First segment of the advertised FQN `<prefix>/<ns>/models/<servedName>`          |
| `modelsEndpoint.requestTimeoutSeconds` | `3`                          | Per-backend probe timeout (unreachable backends keep their last-known-good entry) |
| `modelsEndpoint.refreshIntervalSeconds` | `30`                        | Background poll interval that refreshes the cached `/v1/models` responses        |
| `commonLabels` / `commonAnnotations` | `{}`                           | Added to every resource                                                          |

## Notes

- **Gateway binding.** `namespace` + `gatewayName` together MUST equal the `control-plane`
  value `kserve-llmisvc-resources.kserve.controller.gateway.ingressGateway.kserveGateway`
  (default `kserve/kserve-ingress-gateway`) — that is the Gateway the KServe-managed router
  attaches its HTTPRoute to. Change both in lockstep.
- **TLS prerequisite.** `tls.issuerRef` must point at an Issuer/ClusterIssuer that already
  exists; this chart does not create one.
- **Exposing on bare metal.** The AgentGateway controller provisions the data-plane Service as type
  `LoadBalancer`. On bare metal it stays `<pending>` until something assigns an external IP — set
  `infrastructure.annotations` to drive MetalLB pool/IP selection and point internal DNS at the
  resulting IP. The full runbook (MetalLB, internal-CA `ClusterIssuer`, DNS, API keys, and a
  NodePort + external-LB fallback) is in [`docs/EXPOSE.md`](../../docs/EXPOSE.md).
- **Auth (API keys).** With `auth.enabled`, an `AgentgatewayPolicy` attaches
  `spec.traffic.apiKeyAuthentication` to the **Gateway**, so a valid API key is required on every
  route through it — `/v1/*`, the path-based `/<ns>/<release>/v1` routes, **and** the `GET /v1/models`
  endpoint — in one place. Clients send `Authorization: Bearer <key>`, the OpenAI convention every SDK
  and notebook already speaks. The keys live in an Opaque Secret (`auth.secretName`) you create
  out-of-band, so they never sit in values/git; AgentGateway's API-key auth can also drive per-key
  token budgets / usage tracking (virtual keys). Verify the `apiKeyAuthentication` field names and the
  Secret layout against the pinned agentgateway **v1.2.1** CRD before install (see `docs/EXPOSE.md`),
  and stage with `auth.mode: Optional` to validate without locking anyone out. For service-to-service
  callers that already use an IdP, agentgateway also supports `jwtAuthentication` as an alternative.
- **BBR** is a no-op header with a single model; it becomes load-bearing for multi-model
  fanout, where HTTPRoute rules match on `X-Gateway-Model-Name`.
- **Guardrails** are opt-in and **routing-neutral**: the policy attaches `backend.ai.promptGuard`
  to the Gateway, so it covers every AI backend reached through it (both the dedicated
  `/<namespace>/<release>/v1` routes and the BBR global `/v1` fanout) without adding a backend or
  route. It only masks/rejects/enriches prompts and responses — standard inference is unaffected.
  PII masking, custom reject patterns, and the system prompt are active by default; OpenAI
  moderation, webhook, Bedrock Guardrails, and Google Model Armor are included as commented
  examples in `templates/guardrails-policy.yaml` (some need an external Secret/Service).
- **Models endpoint** is **routing-neutral**: the `/v1/models` HTTPRoute uses path type `Exact`,
  which outranks the `/v1` PathPrefix BBR fanout in Gateway API precedence, so it intercepts only
  `GET /v1/models` and never shadows inference. The models-aggregator discovers models read-only
  via the Kubernetes API (KServe `LLMInferenceService`s + slurm-models routes), polls each backend's
  own `/v1/models` on `refreshIntervalSeconds`, and **caches the full response** (last-known-good)
  in memory — `GET /v1/models` serves the merged union from cache at constant latency. Every vLLM
  field is passed through; only each `id` is rewritten to the fully-qualified routing key, so it can
  be POSTed straight back to the BBR `/v1` endpoint. A model whose backend is momentarily down keeps
  its cached entry until its `LLMInferenceService`/route is removed. Build/publish the image with
  `make aggregator-image`.
