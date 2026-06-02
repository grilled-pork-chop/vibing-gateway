# slurm-models

Proxies **SLURM-hosted** (out-of-cluster) OpenAI-compatible model servers through the shared
AgentGateway and onto the unified Body-Based-Routing `/v1` endpoint. Each entry in `slurmModels`
is just a **host + port + model name** — no Kubernetes Service, Deployment, or InferencePool. One
release holds **all** your SLURM endpoints; edit the list and `helm upgrade` to add, remove, or
repoint. Standalone chart, no dependencies.

## What it installs

Per `slurmModels` entry, two resources:

- an **`AgentgatewayBackend`** (`ai.provider.host`/`port`/`openai.model`) — AgentGateway connects
  directly to the SLURM `host:port`;
- an **`HTTPRoute`** attached to the Gateway that matches `X-Gateway-Model-Name` (stamped by the BBR
  `AgentgatewayPolicy` from the request body) and forwards to that backend.

Nothing touches KServe. Guardrails attached to the Gateway apply to these backends automatically.

## How requests route

```
POST /v1/completions  {"model":"publishers/slurm/models/llama3-70b", ...}
  → BBR stamps X-Gateway-Model-Name = "publishers/slurm/models/llama3-70b"
  → HTTPRoute slurm-llama3-70b matches the header → AgentgatewayBackend slurm-llama3-70b
  → model rewritten to "meta-llama/Llama-3-70B", forwarded to slurm-node-7.hpc.internal:8000
```

The client's `model` is the routing key (the fully-qualified name); the backend's `openai.model`
is the real name the SLURM vLLM expects — AgentGateway rewrites it upstream.

## Install

```bash
helm upgrade -i slurm-models ./charts/slurm-models -n kserve --create-namespace \
  -f values/slurm-models.yaml
# or: make slurm
```

Change an address/port: edit the entry in `values/slurm-models.yaml`, re-run the command. For a
one-off, `--set slurmModels[0].host=...,slurmModels[0].port=...`.

## Parameters

| Key                                  | Default                  | Description                                                                    |
| ------------------------------------ | ------------------------ | ------------------------------------------------------------------------------ |
| `gatewayName`                        | `kserve-ingress-gateway` | Gateway the rendered HTTPRoutes attach to                                      |
| `namespace`                          | `kserve`                 | Namespace for the backend + route (same as the Gateway → no ReferenceGrant)    |
| `publisher`                          | `slurm`                  | Publisher segment of the FQ routing key `publishers/<publisher>/models/<name>` |
| `slurmModels`                        | `[]`                     | List of SLURM endpoints (see below)                                            |
| `commonLabels` / `commonAnnotations` | `{}`                     | Added to every resource                                                        |

Each `slurmModels` entry:

| Field    | Required | Description                                                                            |
| -------- | -------- | -------------------------------------------------------------------------------------- |
| `name`   | yes      | Short id → resource names (`slurm-<name>`) + default FQ routing key                    |
| `host`   | yes      | SLURM node hostname or IP (must be reachable + resolvable from the Gateway pods)       |
| `port`   | yes      | Port the OpenAI-compatible server listens on                                           |
| `model`  | yes      | Real model name the SLURM vLLM serves (sent upstream)                                  |
| `fqName` | no       | Override the client-facing routing key; default `publishers/<publisher>/models/<name>` |

## Notes

- **Reachability.** The Gateway proxy pods must have a network route to, and DNS resolution for, the
  SLURM hosts. On kind a `*.hpc.internal` name likely won't resolve in-cluster — use a reachable IP
  or fix cluster DNS/routing.
- **Auth.** Assumes open / plain-HTTP endpoints. If a SLURM server needs an API key, extend the
  backend's `ai.provider.openai` with a key/secretRef (not wired by default).
- **Namespace.** Keeping the backend + route in the Gateway's namespace avoids a `ReferenceGrant`
  for the cross-namespace `backendRef`. Point `namespace` elsewhere only if you add one.
