## llm-platform — install orchestration for a local kind cluster.
##
## Monorepo of four standalone Helm charts under charts/ (no root umbrella):
##   foundation     cert-manager + all CRDs
##   control-plane  agentgateway + KServe llmisvc controllers
##   llm-gateway    shared ingress (Gateway + TLS + BBR) — deploy once
##   model-server   one LLMInferenceService — deploy once per model
##
## Bootstrap (ordered): make kind-create deps install-all smoke
## All charts share one overlay (values/values-$(ENV).yaml); switch env with ENV=prod.

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

CLUSTER_NAME ?= llm-platform
# platform-side releases (foundation, control-plane, gateway)
RELEASE_NS   ?= kserve
# workload namespace for model-server releases
MODEL_NS     ?= llm-demo
KUBE_CONTEXT ?= kind-$(CLUSTER_NAME)

# environment overlay selector; switch with ENV=prod
ENV          ?= local
VALUES       := values/values-$(ENV).yaml

# Per-model knobs for the `model` target (override on the CLI):
#   make model MODEL=microsoft/phi-3-mini RELEASE=model-phi
MODEL        ?= facebook/opt-125m
RELEASE      ?= model-opt

KUBECTL ?= kubectl --context $(KUBE_CONTEXT)
HELM    ?= helm --kube-context $(KUBE_CONTEXT)

.PHONY: help tools-check kind-create kind-delete deps \
        foundation control-plane gateway model install-all \
        lint template smoke port-forward port-forward-stop uninstall-all clean

help: ## Show this help
	@awk 'BEGIN{FS=":.*## "} /^[a-zA-Z_-]+:.*## /{printf "  \033[36m%-18s\033[0m %s\n",$$1,$$2}' $(MAKEFILE_LIST)

tools-check: ## Verify helm, kind, kubectl are installed
	@for t in helm kind kubectl; do command -v $$t >/dev/null || { echo "MISSING: $$t"; exit 1; }; done
	@echo "tools OK"

## ── kind ───────────────────────────────────────────────────────────────────
kind-create: ## Create the local kind cluster
	kind create cluster --name $(CLUSTER_NAME) --config kind-config.yaml

kind-delete: ## Delete the local kind cluster
	kind delete cluster --name $(CLUSTER_NAME)

## ── dependencies ─────────────────────────────────────────────────────────────
deps: ## helm dependency update for the wrapper charts (llm-gateway/model-server have no deps)
	$(HELM) dependency update ./charts/foundation
	$(HELM) dependency update ./charts/control-plane

## ── ordered install (canonical) ──────────────────────────────────────────────
foundation: tools-check ## L1: cert-manager + CRDs (--wait: cert-manager Ready, CRDs Established)
	$(HELM) upgrade -i foundation ./charts/foundation \
	  -n $(RELEASE_NS) --create-namespace --wait --timeout 600s \
	  -f $(VALUES)

control-plane: tools-check ## L2: agentgateway + KServe llmisvc controllers (--wait: controller Available)
	$(HELM) upgrade -i control-plane ./charts/control-plane \
	  -n $(RELEASE_NS) --create-namespace --wait --timeout 600s \
	  -f $(VALUES)
	$(KUBECTL) -n $(RELEASE_NS) rollout status deploy/llmisvc-controller-manager --timeout=300s

gateway: tools-check ## L3a: shared ingress — Gateway + TLS cert + BBR policy (deploy once)
	$(HELM) upgrade -i platform-gateway ./charts/llm-gateway \
	  -n $(RELEASE_NS) --create-namespace \
	  -f $(VALUES)

model: tools-check ## L3b: one model-server release (MODEL=<repo> RELEASE=<name>)
	$(HELM) upgrade -i $(RELEASE) ./charts/model-server \
	  -n $(MODEL_NS) --create-namespace \
	  -f $(VALUES) --set servedModelName=$(MODEL)

install-all: foundation control-plane gateway model ## Install all (gateway + one model)
	@echo ">> platform installed. Try: make smoke    (more models: make model MODEL=… RELEASE=…)"

## ── dev helpers ──────────────────────────────────────────────────────────────
lint: ## helm lint every chart with the shared overlay
	@for c in foundation control-plane llm-gateway model-server; do \
	  $(HELM) lint ./charts/$$c -f $(VALUES); \
	done

template: ## Render every chart with the shared overlay (ENV=local|prod)
	$(HELM) template foundation       ./charts/foundation     -f $(VALUES)
	$(HELM) template control-plane    ./charts/control-plane  -f $(VALUES)
	$(HELM) template platform-gateway ./charts/llm-gateway    -f $(VALUES)
	$(HELM) template $(RELEASE)       ./charts/model-server   -f $(VALUES) --set servedModelName=$(MODEL)

smoke: ## Port-forward the Gateway and hit the model (path: /$(MODEL_NS)/$(RELEASE)/...)
	@$(KUBECTL) -n $(RELEASE_NS) port-forward svc/kserve-ingress-gateway 18080:80 >/dev/null 2>&1 & \
	  PF=$$!; sleep 3; \
	  echo ">> POST /$(MODEL_NS)/$(RELEASE)/v1/completions"; \
	  curl -sS -X POST http://localhost:18080/$(MODEL_NS)/$(RELEASE)/v1/completions \
	    -H 'Content-Type: application/json' \
	    -d '{"model":"$(MODEL)","prompt":"Who are you?"}' | head -c 600; echo; \
	  kill $$PF 2>/dev/null || true

port-forward: ## Background port-forward of the Gateway to localhost:8080
	nohup $(KUBECTL) -n $(RELEASE_NS) port-forward svc/kserve-ingress-gateway 8080:80 >/dev/null 2>&1 &
	@echo "Gateway → http://localhost:8080  (path: /$(MODEL_NS)/$(RELEASE)/v1/...)"

port-forward-stop: ## Kill background port-forwards
	@pkill -f "port-forward svc/kserve-ingress-gateway" || true

## ── teardown ─────────────────────────────────────────────────────────────────
uninstall-all: ## Uninstall releases in reverse order (one model shown; repeat for more)
	-$(HELM) uninstall $(RELEASE)         -n $(MODEL_NS)
	-$(HELM) uninstall platform-gateway   -n $(RELEASE_NS)
	-$(HELM) uninstall control-plane      -n $(RELEASE_NS)
	-$(HELM) uninstall foundation         -n $(RELEASE_NS)

clean: uninstall-all kind-delete ## Uninstall everything and delete the cluster
