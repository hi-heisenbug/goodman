# Goodman — build/test/deploy targets.
SHELL := /bin/bash
GO ?= go
CLANG ?= clang
ARCH ?= x86
REGISTRY ?= goodman
TAG ?= dev

BPF_SRC := bpf/goodman.bpf.c
BPF_OBJ := bpf/goodman.bpf.o
BPF_INCLUDES := -I bpf -I bpf/include
LOADER_OBJ := internal/loader/goodman.bpf.o
UI_DIST := internal/api/ui/dist

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## --- eBPF ---
.PHONY: vmlinux
vmlinux: ## Regenerate bpf/vmlinux.h from the running kernel's BTF
	bpftool btf dump file /sys/kernel/btf/vmlinux format c > bpf/vmlinux.h

.PHONY: bpf
bpf: $(LOADER_OBJ) ## Compile the eBPF object
$(LOADER_OBJ): $(BPF_SRC) bpf/goodman.h bpf/vmlinux.h
	$(CLANG) -O2 -g -target bpf -D__TARGET_ARCH_$(ARCH) $(BPF_INCLUDES) -c $(BPF_SRC) -o $(BPF_OBJ)
	cp $(BPF_OBJ) $(LOADER_OBJ)

## --- Dashboard ---
.PHONY: dashboard
dashboard: ## Build the React dashboard into the collector's embed dir
	cd dashboard && npm install && npm run build
	rm -rf $(UI_DIST) && cp -r dashboard/dist $(UI_DIST)

$(UI_DIST)/index.html:
	@mkdir -p $(UI_DIST)
	@[ -f $(UI_DIST)/index.html ] || printf '<!doctype html><title>Goodman</title><p>Run: make dashboard</p>' > $(UI_DIST)/index.html

## --- Go ---
.PHONY: build
build: bpf $(UI_DIST)/index.html ## Build sensor, collector, goodmanctl into bin/
	$(GO) build -o bin/sensor ./cmd/sensor
	$(GO) build -o bin/collector ./cmd/collector
	$(GO) build -o bin/goodmanctl ./cmd/goodmanctl

.PHONY: test
test: bpf $(UI_DIST)/index.html ## Run unit tests
	$(GO) test ./...

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

## --- End to end ---
.PHONY: workload
workload: ## Install good-pkg@1.0.0 into the test workload
	@mkdir -p test/workload/node_modules
	rm -rf test/workload/node_modules/good-pkg
	cp -r test/fixtures/good-pkg-1.0.0 test/workload/node_modules/good-pkg

.PHONY: e2e
e2e: build workload ## Full eBPF drift replay (NEEDS ROOT: run `sudo make e2e`)
	bash test/e2e/drift_test.sh

.PHONY: smoke
smoke: build ## Backend-only smoke test (no root needed)
	bash test/e2e/smoke_test.sh

## --- Docker / Helm ---
.PHONY: docker
docker: ## Build sensor + collector images
	docker build -f deploy/docker/collector.Dockerfile -t $(REGISTRY)/collector:$(TAG) .
	docker build -f deploy/docker/sensor.Dockerfile -t $(REGISTRY)/sensor:$(TAG) .

.PHONY: helm-lint
helm-lint: ## Lint the Helm chart
	helm lint deploy/helm/goodman

.PHONY: kind-e2e
kind-e2e: ## Create a kind cluster, install Goodman, run the in-cluster drift test
	bash test/e2e/kind_e2e.sh

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin $(BPF_OBJ) $(LOADER_OBJ) goodman.db goodman.db-* dashboard/dist
