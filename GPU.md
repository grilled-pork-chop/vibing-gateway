# GPU → LLM Inference Service: on-prem deployment runbook

This walks the full path from **a bare GPU node** to **a running KServe
`LLMInferenceService`** served through the gateway, on a generic / self-managed
(on-prem) Kubernetes cluster. For the local CPU-only flow see [`README.md`](README.md);
for the airgapped bundle see [`INSTALL.md`](INSTALL.md).

The GPU serving path is already built into the charts — it's gated behind
`gpu.enabled: true` in [`values/values-prod.yaml`](values/values-prod.yaml). What
this repo does **not** do is prepare the nodes: NVIDIA drivers, the container
runtime, and the Kubernetes device plugin are assumed to exist. Steps 1–3 below
cover that prep; steps 4–7 are the platform itself.

## What's assumed vs. what the charts provide

| Layer | Who owns it | Detail |
| --- | --- | --- |
| GPU driver + `nvidia-container-toolkit` on each node | **You / cluster admin** (Step 1) | not installed by these charts |
| `nvidia.com/gpu` exposed as a schedulable resource | **NVIDIA device plugin / GPU Operator** (Step 2) | not installed by these charts |
| Node labels + taints for GPU pools | **You** (Step 3, optional) | mapped to `vllm.nodeSelector` / `vllm.tolerations` |
| `foundation` / `control-plane` / `llm-gateway` | **charts** (Step 4) | GPU-agnostic; run on any node |
| `LLMInferenceService` → Pod requesting `nvidia.com/gpu: 1` | **`model-server` chart** (Step 6) | KServe derives InferencePool / EPP / HTTPRoute |
| Model weights on a PVC at `/mnt/models` | **You** (Step 5) | the GPU path never pulls from HuggingFace at start |

`gpu.type` and `vllm.resources.limits.nvidia.com/gpu` in `values-prod.yaml` both
reference the **`nvidia.com/gpu`** resource key — that key only exists on a node
once Step 2 is done.

---

## Step 1 — Prepare the GPU nodes (host level)

On every GPU node:

1. **NVIDIA datacenter driver** installed and loaded.
2. **`nvidia-container-toolkit`** installed and the container runtime (containerd)
   configured to use the NVIDIA runtime:

   ```bash
   # on each GPU node, after installing nvidia-container-toolkit
   sudo nvidia-ctk runtime configure --runtime=containerd --set-as-default
   sudo systemctl restart containerd
   ```

3. **Verify the host sees the GPU(s):**

   ```bash
   nvidia-smi          # lists each GPU, driver + CUDA version
   ```

> If you prefer to let Kubernetes manage drivers + toolkit for you, skip the
> manual driver install and use the **NVIDIA GPU Operator** in Step 2 with its
> driver and toolkit components enabled.

---

## Step 2 — Make GPUs schedulable (cluster level)

Kubernetes only schedules onto `nvidia.com/gpu` once a device plugin advertises it.
Pick one:

### Option A (recommended) — NVIDIA GPU Operator

Manages the device plugin, GPU Feature Discovery (node labels), and — if you let
it — drivers and the toolkit too:

```bash
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia && helm repo update
helm install --wait gpu-operator nvidia/gpu-operator \
  -n gpu-operator --create-namespace
# nodes that ALREADY have drivers+toolkit from Step 1:
#   --set driver.enabled=false --set toolkit.enabled=false
```

### Option B (lighter) — device plugin only

Use this when drivers + toolkit are already installed (Step 1) and you don't want
the full operator:

```bash
kubectl create -f \
  https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.17.0/deployments/static/nvidia-device-plugin.yml
```

### Verify GPUs are now allocatable

```bash
kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.allocatable.nvidia\.com/gpu}{"\n"}{end}'
# each GPU node should report a non-empty count, e.g.  gpu-node-1   8
kubectl describe node <gpu-node> | grep -A3 Allocatable    # shows nvidia.com/gpu
```

If the count is empty, the device plugin pod isn't Ready yet — check
`kubectl get pods -n gpu-operator` (or `-n kube-system`) before going further.

---

## Step 3 — Label & taint GPU nodes (optional but recommended)

The GPU Operator's **GPU Feature Discovery** auto-labels nodes (e.g.
`nvidia.com/gpu.product=NVIDIA-H200`). To keep non-GPU workloads off your
expensive GPU nodes, taint them and let only the model pods tolerate it:

```bash
kubectl taint nodes -l nvidia.com/gpu.present=true \
  nvidia.com/gpu=present:NoSchedule
```

Then map that to the model-server in a values overlay (these keys already exist in
`values/values-prod.yaml`, shipped empty):

```yaml
vllm:
  nodeSelector:
    nvidia.com/gpu.product: NVIDIA-H200      # pin to a specific GPU SKU
  tolerations:
    - key: nvidia.com/gpu
      operator: Exists
      effect: NoSchedule
```

`vllm.topologySpreadConstraints` is also available if you want replicas spread
across nodes/zones.

---

## Step 4 — Install the platform charts (ordered)

These three are GPU-agnostic and schedule on any node. The order is **load-bearing**
(CRDs and the gateway must exist before workloads):

```bash
make deps                 # vendor subcharts (connected host only)
make foundation  ENV=prod # cert-manager + all CRDs
make control-plane ENV=prod # agentgateway + KServe llmisvc controllers
make gateway     ENV=prod # shared Gateway + TLS + BBR policy (deploy once)
```

`ENV=prod` selects `values/values-prod.yaml`. Its `tls` block points at a
`ClusterIssuer` named `letsencrypt-prod` and `hostname: llm.example.com` — set
those to real values, and make sure the `ClusterIssuer` actually exists (nothing in
this repo creates it).

---

## Step 5 — Pre-seed the model-weights PVC

On the GPU path the chart mounts a **pre-seeded PVC** at `/mnt/models`; it never
downloads from HuggingFace at start. Create the claim from
[`samples/model-pvc.yaml`](samples/model-pvc.yaml) (set `storageClassName` and
`storage` for your cluster), then populate it:

```bash
kubectl apply -n llm-demo -f samples/model-pvc.yaml     # creates PVC "model-weights"
```

Fill it out-of-band with a one-off Job (or `kubectl cp` / `hf download`). Example
Job that downloads a model onto the claim:

```yaml
apiVersion: batch/v1
kind: Job
metadata: { name: seed-weights, namespace: llm-demo }
spec:
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: dl
          image: python:3.11-slim
          command: ["/bin/sh","-c"]
          args:
            - pip install -q "huggingface_hub[cli]" &&
              hf download meta-llama/Meta-Llama-3-8B --local-dir /mnt/models/llama-3-8b
          env:
            - { name: HF_TOKEN, valueFrom: { secretKeyRef: { name: hf-token, key: token } } }
          volumeMounts:
            - { name: weights, mountPath: /mnt/models }
      volumes:
        - name: weights
          persistentVolumeClaim: { claimName: model-weights }
```

One PVC can hold several models in subdirectories — point each release at its
subdir with `modelStorage.pvc.subPath` (e.g. `llama-3-8b`).

---

## Step 6 — Deploy the model (LLMInferenceService)

Because `make model` only forwards `--set servedModelName`, set the PVC claim
(and any scheduling overrides) with a direct `helm upgrade`:

```bash
helm upgrade -i model-llama ./charts/model-server -n llm-demo --create-namespace \
  -f values/values-prod.yaml \
  --set servedModelName=meta-llama/Meta-Llama-3-8B \
  --set modelStorage.pvc.existingClaim=model-weights \
  --set modelStorage.pvc.subPath=llama-3-8b
```

If your defaults already cover the PVC (e.g. via a per-model values file), the
shortcut still works:

```bash
make model ENV=prod RELEASE=model-llama MODEL=meta-llama/Meta-Llama-3-8B
```

What happens: the chart renders an `LLMInferenceService` with `uri: pvc://...` and a
container requesting `nvidia.com/gpu: 1` — the scheduler uses that limit to place
the pod on a GPU node. KServe's controller then derives the **InferencePool +
EndpointPicker + HTTPRoute** and attaches the route to the shared Gateway.

### Tuning knobs (all in `values/values-prod.yaml`)

| Key | What it does |
| --- | --- |
| `replicaCount` | number of serving pods (each takes `nvidia.com/gpu: 1`) |
| `vllm.args: --max-model-len` | max context length |
| `vllm.args: --gpu-memory-utilization` | fraction of VRAM vLLM reserves (lower if OOM) |
| `vllm.resources.limits.memory` | host RAM ceiling — keep ≥ model + KV overhead |
| `vllm.shmSize` | `/dev/shm` size (vLLM KV cache) |

**Multi-GPU (single node):** add `--tensor-parallel-size=N` to `vllm.args` and
raise `vllm.resources.limits.nvidia.com/gpu` to `N`. **Multi-node** serving uses
the optional LeaderWorkerSet — enable `lws.enabled: true` in the control-plane.

---

## Step 7 — Verify end to end

```bash
kubectl -n llm-demo get llminferenceservice,pods
# pod should be Running with a GPU allocated — NOT Pending on
# "0/N nodes are available: Insufficient nvidia.com/gpu".

make smoke    # port-forwards the Gateway and POSTs a completion
```

Or hit it directly after `make port-forward`:

```bash
# Dedicated path (short model name)
curl -sS -X POST http://localhost:8080/llm-demo/model-llama/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"meta-llama/Meta-Llama-3-8B","prompt":"Who are you?"}'

# Body-Based Routing root path (fully-qualified id)
curl -sS -X POST http://localhost:8080/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"publishers/llm-demo/models/meta-llama/Meta-Llama-3-8B","prompt":"Who are you?"}'
```

---

## Troubleshooting

| Symptom | Likely cause / fix |
| --- | --- |
| Pod `Pending`, "Insufficient `nvidia.com/gpu`" | No allocatable GPU on any schedulable node — finish Step 2, or the node is tainted and the pod lacks the matching `vllm.tolerations` (Step 3). |
| `nvidia.com/gpu` absent from `kubectl describe node` | Device plugin not Ready — check the operator/plugin pods; confirm Step 1 (`nvidia-smi`, toolkit) on that node. |
| Pod `CrashLoopBackOff`, CUDA OOM in logs | Lower `--gpu-memory-utilization`, reduce `--max-model-len`, or use a bigger GPU / `--tensor-parallel-size`. |
| vLLM can't find the model / empty `/mnt/models` | PVC not seeded or wrong `modelStorage.pvc.subPath` (Step 5). |
| First pod very slow to become Ready | Image pull + weight load — the startup probe allows ~30 min. Pre-pull the vLLM image with a DaemonSet (see *Must-do before production* in `README.md`). |
