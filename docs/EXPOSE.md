# Exposing the gateway to your IS users (bare metal + API keys)

After `make install-all` the platform stands up the `Gateway` (`kserve/kserve-ingress-gateway`) and
the AgentGateway controller auto-provisions an Envoy data-plane `Service` of type **LoadBalancer**.
Out of the box that is only reachable via `kubectl port-forward` (see `make smoke`). This runbook
makes it reachable by your organization's internal users on a **bare-metal** cluster:

- **External entrypoint** — MetalLB gives the LoadBalancer Service a real IP from an internal pool.
- **TLS** — cert-manager issues the listener cert from your internal CA.
- **Auth** — an `AgentgatewayPolicy` requires a valid **API key** on every route, including
  `GET /v1/models`.

> **Auth model.** API keys are the idiomatic choice for an OpenAI-compatible endpoint: clients send
> `Authorization: Bearer <key>`, exactly what the OpenAI SDK / LangChain / notebooks already do — no
> token flow to wire up. The keys live in an Opaque Secret you create out-of-band, so they never sit
> in values/git. AgentGateway's API-key auth can also drive per-key token budgets and usage tracking
> (virtual keys) for cost control. For service-to-service callers that already use an IdP, agentgateway
> also supports `jwtAuthentication` as an alternative — out of scope for this runbook.

All of this is wired through the prod overlay `values/values-prod.yaml`; replace the PLACEHOLDER
values there with your environment's specifics.

---

## Prerequisites & ordering

Order matters — each step depends on the previous:

1. **MetalLB installed + an `IPAddressPool`/`L2Advertisement` applied** *before* deploying the chart,
   or the Service stays `<pending>` forever.
2. **The TLS `ClusterIssuer` exists** *before* the chart's `Certificate`, or HTTPS has no Secret.
3. **The API-key Secret exists** *before* `auth.enabled: true` in `Strict` mode, or every request
   (including `/v1/models`) gets a 401. Stage with `auth.mode: Optional`.
4. The MetalLB `IPAddressPool` **metadata.name** must equal the `metallb.universe.tf/address-pool`
   value in `values-prod.yaml` (`llm-gateway-pool`).

---

## 1. Install MetalLB (L2 mode)

L2 mode needs no router config and is the simplest fit for a single gateway VIP.

```bash
helm repo add metallb https://metallb.github.io/metallb
helm upgrade -i metallb metallb/metallb -n metallb-system --create-namespace
kubectl -n metallb-system rollout status deploy/metallb-controller
```

Apply a pool (use a range your network reserves for the cluster) and advertise it on L2. The pool
**name must match** `infrastructure.annotations.metallb.universe.tf/address-pool` in the overlay:

```yaml
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: llm-gateway-pool
  namespace: metallb-system
spec:
  addresses:
    - 10.0.0.50-10.0.0.60   # an internal range reserved for the gateway VIP
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: llm-gateway-l2
  namespace: metallb-system
spec:
  ipAddressPools:
    - llm-gateway-pool
```

```bash
kubectl apply -f metallb-pool.yaml
```

> Switch to **BGP mode** only if you outgrow single-node L2 failover or want true multi-path — it
> peers MetalLB with your routers and needs network-team config.

---

## 2. Create the TLS `ClusterIssuer`

Internal-only DNS can't be validated by Let's Encrypt HTTP-01. Use an **internal corporate CA** (a
CA cert+key your org already trusts) or a **DNS-01** ACME issuer for the internal zone.

Internal-CA example (the CA cert+key live in a Secret in `cert-manager`):

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: corp-internal-ca          # matches values-prod.yaml tls.issuerRef.name
spec:
  ca:
    secretName: corp-ca-keypair   # kubectl create secret tls corp-ca-keypair --cert=ca.crt --key=ca.key -n cert-manager
```

```bash
kubectl apply -f cluster-issuer.yaml
```

---

## 3. Create the API-key Secret

Keys live in an Opaque Secret you manage out-of-band — the chart references it by name
(`auth.secretName`, default `llm-gateway-api-keys`) but never renders it, so keys stay out of
values/git. Generate a strong key per user/team and put them in the Secret:

```bash
kubectl -n kserve create secret generic llm-gateway-api-keys \
  --from-literal=alice=$(openssl rand -hex 24) \
  --from-literal=team-search=$(openssl rand -hex 24)
```

> **Verify the Secret layout** against the pinned agentgateway v1.2.1 CRD — implementations differ on
> whether each key is a separate `data` entry (per-identity, as above) or a single field. Confirm with
> `kubectl explain agentgatewaypolicy.spec.traffic.apiKeyAuthentication` (see "CRD field verification"
> below) and adjust the `--from-literal` layout to match. Rotate by adding the new key, rolling
> clients, then removing the old entry — no gateway restart needed.

---

## 4. Set the hostname and internal DNS

`values-prod.yaml` ships `hostname: llm.corp.internal`. Set it to your real internal FQDN, then add
an **internal DNS A record** pointing that name at the MetalLB IP (from step 6). Clients and the
TLS cert SAN both use this name.

---

## 5. Deploy with the prod overlay

```bash
make deps                                   # vendor OCI subcharts — required before any render/install
helm upgrade -i platform-gateway ./charts/llm-gateway -f values/values-prod.yaml
```

Edit the PLACEHOLDERs in `values-prod.yaml` first: `hostname`, the MetalLB pool name, `tls.issuerRef`,
and `auth.secretName` (it must match the Secret created in step 3).

---

## 6. Verify the external IP

```bash
kubectl -n kserve get svc kserve-ingress-gateway -o wide
# EXTERNAL-IP should show an address from llm-gateway-pool (e.g. 10.0.0.50), not <pending>
kubectl -n kserve get gateway kserve-ingress-gateway
```

If it's stuck `<pending>`: MetalLB isn't installed, the pool is exhausted, or the annotation pool
name doesn't match. Confirm the annotation landed on the Service:

```bash
kubectl -n kserve get svc kserve-ingress-gateway -o jsonpath='{.metadata.annotations}'
```

---

## 7. Test with an API key

Use one of the keys you put in the Secret. The Gateway-level auth policy covers `/v1/models` and
inference alike.

```bash
KEY=<one-of-the-keys-from-the-secret>

# No key → 401
curl -so /dev/null -w '%{http_code}\n' https://llm.corp.internal/v1/models          # 401

# Valid key → 200 + model list
curl -s https://llm.corp.internal/v1/models -H "Authorization: Bearer $KEY" | jq .

# Inference is gated the same way
curl -s https://llm.corp.internal/v1/completions \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"model":"publishers/<ns>/models/<name>","prompt":"Who are you?"}'
```

Point any OpenAI client at it directly: `base_url=https://llm.corp.internal/v1`, `api_key=<KEY>`.
(If the internal CA isn't yet in your client trust store, add `--cacert corp-ca.crt`.)

---

## CRD field verification (do before relying on auth)

The agentgateway CRDs are pulled via OCI by `make deps` — they aren't vendored in this repo — so the
exact `spec.traffic.apiKeyAuthentication` field names (and the Secret key layout) for the pinned
**v1.2.1** must be confirmed before you trust the policy. After `make deps`:

```bash
helm template platform-gateway ./charts/llm-gateway -f values/values-prod.yaml \
  | kubectl apply --dry-run=server -f -                       # server validates against the live CRD
kubectl explain agentgatewaypolicy.spec.traffic.apiKeyAuthentication   # inspect the real schema
```

If the field names differ (e.g. `secretRef` vs `secretSelector`, or the Secret layout), adjust
`charts/llm-gateway/templates/auth-policy.yaml` and the step-3 Secret to match — the template structure
(gating, targetRefs, mode, secret reference) stays the same. API-key + JWT auth landed in agentgateway
via kgateway PR #12886 (v1.0+), so v1.2.1 has it; if a build lacks it, front the Gateway with an
ext-authz sidecar instead.

---

## Fallback: NodePort + external load balancer (no MetalLB)

If L2/MetalLB isn't permitted on your network, expose the gateway via NodePort and front it with your
existing org LB (HAProxy / F5 / nginx):

1. Override the provisioned Service to `NodePort` (via the agentgateway `AgentgatewayParameters` for
   this GatewayClass, or by patching the Service type) and note the allocated nodePort.
2. Point the external LB's VIP at `<each-nodeIP>:<nodePort>`. Terminate or pass through TLS at the LB.
3. Add internal DNS for the hostname at the LB VIP (instead of the MetalLB IP).
4. The auth policy and everything else are unchanged — only the entrypoint differs.
