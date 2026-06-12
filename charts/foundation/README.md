# foundation

The LLM platform's **cluster-prerequisite tier**: installs **cert-manager**, a platform
**ClusterIssuer** for Gateway TLS, and (optionally) the **LeaderWorkerSet** operator for
multi-node serving. Install it **second**, right after `platform-crds`.

> Platform CRDs (Gateway API, GIE, KServe, AgentGateway) are **not** here — they live in the
> `platform-crds` chart, which installs first. cert-manager keeps its own CRDs (an operator owns
> the CRDs it ships).

## What it installs

| Component                | Version   | Source                                       |
| ------------------------ | --------- | -------------------------------------------- |
| cert-manager             | `v1.17.0` | dependency (`oci://quay.io/jetstack/charts`) |
| Platform ClusterIssuer   | —         | template (`templates/clusterissuer.yaml`)    |
| LeaderWorkerSet (optional) | `0.8.0` | dependency (`oci://registry.k8s.io/lws/charts`) |

## Install

```bash
helm dependency update ./charts/foundation
helm upgrade -i foundation ./charts/foundation -f values/values-local.yaml --wait
```

`--wait` matters: cert-manager must be Ready before the ClusterIssuer (and the downstream
`control-plane` chart) reconcile.

## Parameters

| Key                          | Default        | Description                                              |
| ---------------------------- | -------------- | ------------------------------------------------------- |
| `cert-manager.enabled`       | `true`         | Install cert-manager (required by KServe webhooks)      |
| `cert-manager.crds.enabled`  | `true`         | Install cert-manager's own CRDs                         |
| `issuer.enabled`             | `true`         | Create the platform ClusterIssuer                       |
| `issuer.name`                | `selfsigned`   | ClusterIssuer name (must match `llm-gateway` `tls.issuerRef.name`) |
| `issuer.kind`                | `selfsigned`   | Issuer flavor: `selfsigned` \| `ca` \| `acme`           |
| `issuer.ca.secretName`       | `""`           | For `kind=ca`: Secret holding the signing CA cert/key   |
| `issuer.acme.*`              | —              | For `kind=acme`: server / email / solver config         |
| `lws.enabled`                | `false`        | Install the LeaderWorkerSet operator (multi-node)       |

## Notes

- **ClusterIssuer ↔ Gateway TLS.** `llm-gateway`'s `tls.issuerRef` defaults to a `selfsigned`
  ClusterIssuer; this chart creates it so TLS works out of the box. In prod, switch `issuer.kind`
  to `ca` or `acme` via the overlay (and set `issuer.name` to match `tls.issuerRef.name`).
- These versions are dictated by KServe `v0.19.0-rc0`. Chart versions are pinned in `Chart.yaml`.
