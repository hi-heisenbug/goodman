# Goodman — build/test/deploy targets.
SHELL := /bin/bash
GO ?= go
CLANG ?= clang
HOST_ARCH := $(shell uname -m)
ARCH ?= $(if $(filter x86_64 amd64,$(HOST_ARCH)),x86,$(if $(filter aarch64 arm64,$(HOST_ARCH)),arm64,$(HOST_ARCH)))
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
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## --- Setup ---
.PHONY: doctor
doctor: ## Check tools, kernel, and eBPF support; print setup guidance
	@bash scripts/preflight.sh

.PHONY: setup
setup: ## Install build prerequisites (Debian/Ubuntu) then run doctor
	@bash scripts/setup.sh

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
.PHONY: fmt
fmt: ## Format all Go source files
	gofmt -w $$(find . -name '*.go' -not -path './dashboard/node_modules/*')

.PHONY: fmt-check
fmt-check: ## Fail when any Go source file needs gofmt
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './dashboard/node_modules/*'))" || \
	  { echo "Go files need formatting (run: make fmt)"; gofmt -l $$(find . -name '*.go' -not -path './dashboard/node_modules/*'); exit 1; }

.PHONY: build
build: bpf $(UI_DIST)/index.html ## Build sensor, collector, goodmanctl into bin/
	$(GO) build -o bin/sensor ./cmd/sensor
	$(GO) build -o bin/collector ./cmd/collector
	$(GO) build -o bin/goodmanctl ./cmd/goodmanctl

.PHONY: portable-build
portable-build: $(UI_DIST)/index.html ## Build collector + demo runner without eBPF tooling
	$(GO) build -o bin/collector ./cmd/collector
	$(GO) build -o bin/goodman-demo ./cmd/goodman-demo

.PHONY: portable-cross-build
portable-cross-build: $(UI_DIST)/index.html ## Cross-build the portable demo for macOS and Windows
	@tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
	for target in darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do \
		os="$${target%/*}"; arch="$${target#*/}"; ext=""; \
		[ "$$os" = windows ] && ext=.exe; \
		GOOS="$$os" GOARCH="$$arch" CGO_ENABLED=0 $(GO) build -o "$$tmp/collector-$$os-$$arch$$ext" ./cmd/collector; \
		GOOS="$$os" GOARCH="$$arch" CGO_ENABLED=0 $(GO) build -o "$$tmp/goodman-demo-$$os-$$arch$$ext" ./cmd/goodman-demo; \
	done

.PHONY: test
test: bpf $(UI_DIST)/index.html ## Run unit tests
	$(GO) test ./...

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

.PHONY: quality
quality: fmt-check vet ## Static, dead-code, duplicate, complexity, vulnerability, and module checks
	$(GO) run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...
	$(GO) run golang.org/x/tools/cmd/deadcode@v0.30.0 -test ./...
	$(GO) run github.com/mibk/dupl@v1.0.0 -t 80 cmd internal test
	@output="$$( $(GO) run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 15 cmd internal 2>&1 || true )"; \
		hotspots="$$( printf '%s\n' "$$output" | grep -v '^exit status 1$$' | grep -v '_test\.go' || true )"; \
		test -z "$$hotspots" || { printf '%s\n' "$$hotspots"; exit 1; }
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...
	$(GO) mod tidy -diff

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

.PHONY: ha-smoke
ha-smoke: build ## Two-replica HA smoke against Docker Postgres (skips if unavailable)
	bash scripts/ha-smoke.sh

.PHONY: replay
replay: ## Replay real npm supply-chain attacks and assert each is caught (no root)
	go test ./test/replay/ -v -count=1

.PHONY: bench
bench: ## Benchmark the collector ingest pipeline and canonicalization (no root)
	go test -run='^$$' -bench=. -benchmem ./internal/fingerprint/ ./internal/attribute/

.PHONY: demo
demo: portable-build ## Portable product wow: OpenClaw skill drift + Mini-Shai-Hulud replay
	./bin/goodman-demo

.PHONY: demo-check
demo-check: portable-demo-check ## Non-interactive demo verification (CI / DoD check)

.PHONY: portable-demo-check
portable-demo-check: portable-build ## Verify the complete demo without eBPF/root
	./bin/goodman-demo -check -port $${GOODMAN_DEMO_PORT:-8855}

.PHONY: setup-everything
setup-everything: ## Auto-prepare and verify the portable demo
	bash scripts/setup-everything.sh demo --check

.PHONY: e2e-openclaw
e2e-openclaw: ## Real eBPF OpenClaw runtime-contract proof (run after make build; needs root)
	bash test/e2e/openclaw_test.sh

## --- Docker / Helm ---
.PHONY: docker
docker: ## Build sensor + collector images
	docker build -f deploy/docker/collector.Dockerfile -t $(REGISTRY)/collector:$(TAG) .
	docker build -f deploy/docker/sensor.Dockerfile -t $(REGISTRY)/sensor:$(TAG) .

.PHONY: docker-demo
docker-demo: ## Build the portable all-in-one demo image
	docker build -f deploy/docker/demo.Dockerfile -t $(REGISTRY)/demo:$(TAG) .

.PHONY: docker-e2e
docker-e2e: ## Run both real eBPF proofs in a privileged Linux container
	docker build -f deploy/docker/e2e.Dockerfile -t $(REGISTRY)/e2e:$(TAG) .
	docker run --rm --privileged --pid=host --cgroupns=host \
		--security-opt seccomp=unconfined --ulimit memlock=-1:-1 \
		-v /sys/fs/cgroup:/sys/fs/cgroup:rw \
		-v /sys/kernel/tracing:/sys/kernel/tracing:rw \
		-v /sys/kernel/debug:/sys/kernel/debug:rw \
		-v /sys/kernel/security:/sys/kernel/security:rw \
		$(REGISTRY)/e2e:$(TAG) all

.PHONY: install-k8s
install-k8s: ## Install Goodman into the current Kubernetes context
	bash scripts/install-k8s.sh

.PHONY: helm-lint
helm-lint: ## Lint the Helm chart
	helm lint deploy/helm/goodman

.PHONY: kind-e2e
kind-e2e: ## Create a kind cluster, install Goodman, run the in-cluster drift test
	bash test/e2e/kind_e2e.sh

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin $(BPF_OBJ) $(LOADER_OBJ) goodman.db goodman.db-* goodman_demo.db goodman_demo.db-* dashboard/dist demo_build/*.db demo_build/*.db-shm demo_build/*.db-wal
