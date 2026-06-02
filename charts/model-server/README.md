# model-server

Deploys one KServe **`LLMInferenceService`** per release — **deploy once per model**. The
resource is named after the helm release, so the route path is `/<namespace>/<release>/v1/...`.
`gpu.enabled` switches between a privileged CPU vLLM serving `hf://<repo>` (local) and vLLM on
GPU reading a pre-seeded PVC (production). Standalone chart, no dependencies.

## What it installs

A single `LLMInferenceService` with `router` left managed (`{}`), so KServe derives the
`InferencePool`, `EndpointPicker`, and `HTTPRoute` from it and attaches the route to the
Gateway labelled `serving.kserve.io/gateway`. No other resources are rendered.

## Install

```bash
# CPU / local
helm upgrade -i model-opt ./charts/model-server -f values/values-local.yaml \
  --set servedModelName=facebook/opt-125m

# GPU / production (pre-seed the PVC first — see samples/model-pvc.yaml)
helm upgrade -i model-llama ./charts/model-server -f values/values-prod.yaml \
  --set servedModelName=meta-llama/Meta-Llama-3-8B \
  --set modelStorage.pvc.existingClaim=model-weights
```

Each model is its own release with a distinct name → distinct `LLMInferenceService` and route.

## Parameters

| Key                                  | Default                           | Description                                                               |
| ------------------------------------ | --------------------------------- | ------------------------------------------------------------------------- |
| `gpu.enabled`                        | `false`                           | `false` → CPU vLLM (`hf://`); `true` → GPU vLLM (PVC)                     |
| `gpu.type`                           | `nvidia.com/gpu`                  | Accelerator resource key for GPU scheduling                               |
| `servedModelName`                    | `facebook/opt-125m`               | Model name clients send and KServe serves (`model.name`)                  |
| `hfRepo`                             | `facebook/opt-125m`               | HuggingFace repo for the CPU path (`model.uri: hf://<hfRepo>`)            |
| `fullnameOverride`                   | `""`                              | Override the LLMInferenceService name (default: the release name)         |
| `replicaCount`                       | `1`                               | Number of model-server replicas                                           |
| `port`                               | `8000`                            | Container/serving port for OpenAI traffic and `/metrics`                  |
| `router.{gateway,route,scheduler}`   | `{}`                              | KServe-managed by default; override for bring-your-own                    |
| `cpu.*`                              | quay.io `vllm-cpu`, privileged SC | CPU path image / env / securityContext / resources                        |
| `vllm.*`                             | docker.io `vllm-openai:v0.19.1`   | GPU path image / args / resources / scheduling / `shmSize`                |
| `modelStorage.pvc.existingClaim`     | `""`                              | Pre-seeded PVC at `/mnt/models` (empty → `mock://simulation` render-only) |
| `modelStorage.pvc.subPath`           | `"."`                             | Path inside the PVC containing the model                                  |
| `imageRegistry` / `imagePullSecrets` | `""` / `[]`                       | Registry override / pull secrets                                          |
| `commonLabels` / `commonAnnotations` | `{}`                              | Added to every resource                                                   |

## Notes

- **One release per model.** The LLMInferenceService is named after the release (or
  `fullnameOverride`); install distinct release names for distinct models.
- **GPU path needs a pre-seeded PVC.** The chart never pulls from HuggingFace at start time on
  the GPU path — seed the claim out-of-band (see `samples/model-pvc.yaml`), then point
  `modelStorage.pvc.existingClaim` at it. With no claim set, a `mock://` URI keeps the resource
  render-only for chart tests.
- **CPU path** uses a privileged securityContext (`SYS_NICE`/`IPC_LOCK`, Unconfined seccomp)
  required by the CPU vLLM image; KServe's storage-initializer pulls the `hf://` weights.
