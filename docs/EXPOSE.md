# Exposing the gateway to your IS users (bare metal + OIDC)

After `make install-all` the platform stands up the `Gateway` (`kserve/kserve-ingress-gateway`) and
the AgentGateway controller auto-provisions an Envoy data-plane `Service` of type **LoadBalancer**.
Out of the box that is only reachable via `kubectl port-forward` (see `make smoke`). This runbook
makes it reachable by your organization's internal users on a **bare-metal** cluster:

- **External entrypoint** — MetalLB gives the LoadBalancer Service a real IP from an internal pool.
- **TLS** — cert-manager issues the listener cert from your internal CA.
- **Auth** — an `AgentgatewayPolicy` validates bearer JWTs issued by your IdP (Keycloak / Entra /
  Okta) on every route, including `GET /v1/models`.

> **Auth model.** This validates **IdP-issued bearer tokens** (the right model for an
> OpenAI-compatible API: clients send `Authorization: Bearer <token>`). It does **not** run the
> interactive OIDC browser-redirect login flow — that would additionally need a `GatewayExtension` +
> `Backend` + `BackendTLSPolicy` + client-secret `Secret`.

All of this is wired through the prod overlay `values/values-prod.yaml`; replace the PLACEHOLDER
values there with your environment's specifics.

---

## Prerequisites & ordering

Order matters — each step depends on the previous:

1. **MetalLB installed + an `IPAddressPool`/`L2Advertisement` applied** *before* deploying the chart,
   or the Service stays `<pending>` forever.
2. **The TLS `ClusterIssuer` exists** *before* the chart's `Certificate`, or HTTPS has no Secret.
3. **The IdP issuer/JWKS is reachable from the cluster** *before* `auth.enabled: true` in `Strict`
   mode, or every request (including `/v1/models`) gets a 401. Stage with `auth.mode: Permissive`.
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

## 3. Set the hostname and internal DNS

`values-prod.yaml` ships `hostname: llm.corp.internal`. Set it to your real internal FQDN, then add
an **internal DNS A record** pointing that name at the MetalLB IP (from step 5). Clients and the
TLS cert SAN both use this name.

---

## 4. Deploy with the prod overlay

```bash
make deps                                   # vendor OCI subcharts — required before any render/install
helm upgrade -i platform-gateway ./charts/llm-gateway -f values/values-prod.yaml
```

Edit the PLACEHOLDERs in `values-prod.yaml` first: `hostname`, the MetalLB pool name, `tls.issuerRef`,
and the `auth.providers` issuer / audiences / `jwksUrl`.

---

## 5. Verify the external IP

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

## 6. Test with a bearer token

Get a token from your IdP (client-credentials shown; use your real flow), then call the endpoint.
The Gateway-level auth policy covers `/v1/models` and inference alike.

```bash
TOKEN=$(curl -s https://idp.corp.internal/realms/internal/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=llm-gateway -d client_secret=$SECRET \
  | jq -r .access_token)

# No token → 401
curl -so /dev/null -w '%{http_code}\n' https://llm.corp.internal/v1/models          # 401

# Valid token → 200 + model list
curl -s https://llm.corp.internal/v1/models -H "Authorization: Bearer $TOKEN" | jq .

# Inference is gated the same way
curl -s https://llm.corp.internal/v1/completions \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"publishers/<ns>/models/<name>","prompt":"Who are you?"}'
```

(If the internal CA isn't yet in your client trust store, add `--cacert corp-ca.crt`.)

---

## CRD field verification (do before relying on auth)

The agentgateway CRDs are pulled via OCI by `make deps` — they aren't vendored in this repo — so the
exact `spec.traffic.jwtAuthentication` field names for the pinned **v1.2.1** must be confirmed before
you trust the policy. After `make deps`:

```bash
helm template platform-gateway ./charts/llm-gateway -f values/values-prod.yaml \
  | kubectl apply --dry-run=server -f -                      # server validates against the live CRD
kubectl explain agentgatewaypolicy.spec.traffic.jwtAuthentication   # inspect the real schema
```

If the field names differ (e.g. `jwks.url` vs `jwks.uri`), adjust
`charts/llm-gateway/templates/auth-policy.yaml` to match — the template structure (gating, targetRefs,
range-over-providers, one-of JWKS source) stays the same. If v1.2.1 has no native JWT support, front
the Gateway with an ext-authz sidecar (e.g. oauth2-proxy) instead.

---

## Fallback: NodePort + external load balancer (no MetalLB)

If L2/MetalLB isn't permitted on your network, expose the gateway via NodePort and front it with your
existing org LB (HAProxy / F5 / nginx):

1. Override the provisioned Service to `NodePort` (via the agentgateway `AgentgatewayParameters` for
   this GatewayClass, or by patching the Service type) and note the allocated nodePort.
2. Point the external LB's VIP at `<each-nodeIP>:<nodePort>`. Terminate or pass through TLS at the LB.
3. Add internal DNS for the hostname at the LB VIP (instead of the MetalLB IP).
4. The auth policy and everything else are unchanged — only the entrypoint differs.
