# Offline / airgapped install

This bundle installs the whole LLM serving platform (five Helm charts: `foundation`,
`control-plane`, `monitoring`, `llm-gateway`, `model-server`) into an environment with **no
internet access**. Everything needed — container images, charts, subchart dependencies, values,
and this runbook — ships in a single archive.

The charts already vendor their subchart `.tgz` dependencies under `charts/*/charts/`, so the
chart side is fully self-contained: **no `helm repo add` / `helm dependency update` is needed
at install time.** The only thing that normally requires the network is **container images**,
which is what `images.tar` provides.

---

## 1. What's in the bundle

After extracting `llm-platform-offline-<date>.tar.zst` you get:

```
charts/            five charts + vendored subchart .tgz (cert-manager, agentgateway, kserve, lws, kube-prometheus-stack)
values/            values-local.yaml (CPU/kind) and values-prod.yaml (GPU)
samples/           model-pvc.yaml (GPU weight PVC example)
manual/            step-by-step component notes
kind-config.yaml   1 control-plane + 1 worker kind cluster
Makefile           install orchestration (make foundation/control-plane/gateway/model/...)
README.md          architecture and request/routing examples
INSTALL.md         this file
images.txt         pinned image manifest (source of truth)
images.tar         `docker save` of every image in images.txt (multi-image archive)
SHA256SUMS         checksums of every file in the bundle
```

### Images included (pinned)

| Image                                                   | Used by                                                                                |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `quay.io/jetstack/cert-manager-controller:v1.17.0`      | foundation (cert-manager)                                                              |
| `quay.io/jetstack/cert-manager-webhook:v1.17.0`         | foundation                                                                             |
| `quay.io/jetstack/cert-manager-cainjector:v1.17.0`      | foundation                                                                             |
| `quay.io/jetstack/cert-manager-startupapicheck:v1.17.0` | foundation                                                                             |
| `quay.io/jetstack/cert-manager-acmesolver:v1.17.0`      | foundation — **referenced only in a `--acme-http01-solver-image=` arg**                |
| `cr.agentgateway.dev/controller:v1.2.1`                 | control-plane (agentgateway controller)                                                |
| `cr.agentgateway.dev/agentgateway:v1.2.1`               | control-plane — **per-Gateway data-plane proxy, injected via `AGW_PROXY_IMAGE_*` env** |
| `kserve/llmisvc-controller:v0.19.0-rc0`                 | control-plane (KServe controller)                                                      |
| `kserve/storage-initializer:v0.19.0-rc0`                | control-plane / model-server (weight fetch)                                            |
| `ghcr.io/llm-d/llm-d-cuda:v0.6.0`                       | control-plane (KServe LLMInferenceServiceConfig)                                       |
| `ghcr.io/llm-d/llm-d-inference-scheduler:v0.7.1`        | control-plane                                                                          |
| `ghcr.io/llm-d/llm-d-routing-sidecar:v0.7.1`            | control-plane                                                                          |
| `ghcr.io/llm-d/llm-d-uds-tokenizer:v0.7.1`              | control-plane                                                                          |
| `quay.io/pierdipi/vllm-cpu:latest`                      | model-server — **CPU path** (`values-local.yaml`)                                      |
| `docker.io/vllm/vllm-openai:v0.19.1`                    | model-server — **GPU path** (`values-prod.yaml`)                                       |
| `ghcr.io/llm-gateway/llm-models-aggregator:v0.1.0`      | llm-gateway — models-aggregator (`/v1/models`); built with `make aggregator-image`     |
| `kindest/node:v1.35.1`                                  | kind cluster node (Path A only)                                                        |

> The last two "hidden" platform images (acmesolver, agentgateway proxy) do not appear as a
> plain `image:` field in any rendered manifest, so they are pinned by hand in `images.txt`.
> `make images-verify` reconstructs them from the render and fails the build if either drifts.

---

## 2. Build the bundle (connected host)

On a machine **with** internet and `docker`, `helm`, `zstd`:

```bash
make package
# -> dist/llm-platform-offline-<date>.tar.zst   (+ embedded SHA256SUMS)
```

`make package` runs `images-verify` (drift guard) → `images-save` (`docker pull` + `docker
save` → `images.tar`) → stages charts/values/docs + `images.tar`, writes `SHA256SUMS`, and
compresses to the archive.

> If `make package` complains about a missing vendored subchart `.tgz`, run `make deps` first
> (it pulls the upstream subcharts into `charts/*/charts/`; only needed on the connected host).

Transfer the single `.tar.zst` to the airgapped host (USB, one-way diode, internal artifact
store, …).

---

## 3. On the airgapped host

```bash
zstd -d llm-platform-offline-<date>.tar.zst -o bundle.tar   # or: tar --zstd -xf <file>
mkdir llm-platform && tar -C llm-platform -xf bundle.tar
cd llm-platform
sha256sum -c SHA256SUMS          # integrity check
```

**Prerequisites:** `helm` ≥ 3.17 and `kubectl`. Then pick **one** delivery path below.

---

## Path A — kind on the offline host (no registry)

Matches this repo's local topology. Needs `docker` and `kind` on the host. Because every chart
pins `imagePullPolicy: IfNotPresent`, preloading the images means **no pulls, no registry, and
no `global.imageRegistry` rewrite**.

```bash
# 1. Load every image (incl. the kindest/node image) into the host docker daemon
docker load -i images.tar

# 2. Create the cluster (uses the preloaded kindest/node image)
make kind-create                       # kind create cluster --name llm-platform --config kind-config.yaml

# 3. Inject the workload images into the cluster node's containerd
kind load image-archive images.tar --name llm-platform
#    (kind ignores the kindest/node entry; it loads the workload images)

# 4. Install, ordered: foundation -> control-plane -> gateway -> one model
make install-all                       # ENV=local by default (CPU vLLM, facebook/opt-125m)

# 5. Smoke test through the Gateway
make smoke
```

For the GPU path on a kind-like offline cluster, see the GPU note in *Ordered install* below.

---

## Path B — private registry mirror (real cluster)

For an existing airgapped Kubernetes cluster with an internal registry `REG` (e.g.
`registry.internal:5000`).

```bash
docker load -i images.tar
```

### B1 (recommended) — mirror under the *same* repository paths

Re-tag each image to your registry's host but keep the original path, push, and configure the
nodes' container runtime to mirror the public hosts to `REG`. Nodes then pull transparently and
**no Helm value overrides are required**:

```bash
REG=registry.internal:5000
grep -vE '^[[:space:]]*#|^[[:space:]]*$' images.txt | grep -v '^kindest/' | while read -r img; do
  # strip any leading docker.io/ so the path stays canonical under the mirror
  path=${img#docker.io/}
  docker tag "$img" "$REG/$path"
  docker push "$REG/$path"
done
```

Then on each node, add `REG` as a mirror for `quay.io`, `ghcr.io`, `cr.agentgateway.dev`,
`docker.io`, and `registry.k8s.io` via containerd `hosts.toml` (or your distro's equivalent),
and install exactly as in *Ordered install* with **no extra `--set`**.

### B2 (alternative) — explicit Helm registry overrides

If you cannot configure runtime mirroring, override the image registry per chart. Note the
limits: cert-manager and the KServe/llm-d subchart images **do not** honor
`global.imageRegistry`, so B1 (same-path mirror) is the only fully clean route for those. The
overrides that *are* available:

```bash
REG=registry.internal:5000

# model-server (per release)
helm upgrade -i model-opt ./charts/model-server -f values/values-local.yaml \
  --set servedModelName=facebook/opt-125m \
  --set imageRegistry=$REG --set cpu.image.registry=$REG --set vllm.image.registry=$REG

# control-plane (agentgateway controller + data-plane proxy)
helm upgrade -i control-plane ./charts/control-plane -n kserve --create-namespace \
  -f values/values-local.yaml --wait \
  --set agentgateway.image.registry=$REG --set agentgateway.proxy.image.registry=$REG
```

For cert-manager / KServe controller / storage-initializer / llm-d images under B2 you must
mirror them at their original paths (B1) — there is no clean single-flag registry override.

---

## Ordered install (both paths)

The canonical order (what `make install-all` does) is:

```bash
make foundation       # L1: cert-manager + all CRDs (--wait)
make monitoring       # L1b: Prometheus + Alertmanager + Grafana (owns its CRDs; before control-plane)
make control-plane    # L2: agentgateway + KServe llmisvc controllers (--wait)
make gateway          # L3a: shared Gateway + TLS + BBR policy (deploy once)
make model            # L3b: one LLMInferenceService  (MODEL=<repo> RELEASE=<name>)
```

- Platform releases land in namespace `kserve`; model releases in `llm-demo`.
- **More models:** `make model MODEL=microsoft/phi-3-mini RELEASE=model-phi`.
- **GPU (`ENV=prod`):** the chart never pulls weights from HuggingFace — **pre-seed each
  model's PVC out-of-band** (see `samples/model-pvc.yaml`), then:
  `make model ENV=prod RELEASE=<name> MODEL=<repo>` with
  `--set modelStorage.pvc.existingClaim=<pvc>`. A real `ClusterIssuer` matching
  `values-prod.yaml`'s `tls.issuerRef` must already exist.

---

## Verify it worked (offline)

```bash
kubectl --context kind-llm-platform get pods -A     # no pod in ImagePullBackOff / ErrImagePull
make smoke                                          # POST a completion through the Gateway
```

Or use the two routing examples from `README.md` (dedicated path vs. BBR root path) after
`make port-forward`. If a pod is stuck pulling, an image is missing from the node/registry —
re-run the load/push step for that image (cross-check against `images.txt`).

---

## Teardown

```bash
make clean        # uninstall all releases, then delete the kind cluster (Path A)
make clean-dist   # (build host) remove dist/
```
