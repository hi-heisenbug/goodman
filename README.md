# Goodman

[![CI](https://github.com/hi-heisenbug/goodman/actions/workflows/ci.yml/badge.svg)](https://github.com/hi-heisenbug/goodman/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.23+-00ADD8.svg)](go.mod)
[![Kubernetes](https://img.shields.io/badge/kubernetes-helm-326CE5.svg)](deploy/helm/goodman)

Goodman is a runtime dependency-security sensor by [Heisenbug](https://github.com/hi-heisenbug).
It attributes security-relevant Linux syscalls to the exact **npm or PyPI**
package that caused them, learns a behavioral baseline per
`(service, package, version)`, and raises an alert when a dependency starts
doing something new. Optional eBPF LSM **block mode** is fail-open and
**off by default**.

The short version: the kernel can tell you a process opened a file or connected
to an IP. Goodman tells you which dependency in that process did it.

![Goodman dashboard showing a critical dependency drift alert](docs/images/dashboard.png)

The embedded dashboard is a production React/Vite UI with live alert review,
fingerprints, reachability, coverage/trust, SSE updates, and responsive layouts.

## Demo

Watch the product walkthrough:

<video src="demo_build/goodman_demo.mp4" controls width="100%" title="Goodman product demo"></video>

[Open the demo video](demo_build/goodman_demo.mp4)

## What It Detects

- a package version that starts reading secrets, tokens, SSH keys, `.npmrc`, or
  cloud credentials
- a dependency that starts connecting to cloud metadata or a new outbound host
- a package update that adds process execution where the baseline had none
- any new canonical behavior compared to the learned baseline for that package
- **always-on** high-risk rules that fire during the learning window (no baseline
  poisoning gap for credential / metadata access)

Optional enforcement (`action: "block"` + Helm/sensor gates) can deny matching
syscalls in opted-in namespaces. See [docs/enforcement.md](docs/enforcement.md).

## How It Works

```text
kernel (tracepoints + optional LSM)
open / connect / execve  (+ deny hooks when armed)
        |
        v
eBPF sensor: syscall + user stack → attribute → batch/spool → collector
        |
        v
collector: fingerprint · diff · alerts · reachability · digest · API/SSE/UI
```

1. **Capture:** CO-RE eBPF hooks `open`/`openat`/`openat2`, `connect`, and
   `execve` for watched Node/Python processes and records the user-space stack.
2. **Attribute:** userspace resolves stacks through V8 / CPython perf maps and
   `/proc/<pid>/maps`, then maps the deepest `node_modules/` or
   `site-packages/` frame to `package.json` / `*.dist-info` version.
3. **Fingerprint:** events become stable behaviors such as
   `READ /app/node_modules/pkg/**` and `CONNECT 169.254.169.254:80`.
4. **Diff:** live behavior is compared to the baseline; high-risk rules use
   `action: alert | warn | block` (warn marks *would-block* audit evidence).
5. **Enforce (optional):** LSM hooks consult literal deny maps only when the
   master gate, runtime switch, and namespace label are all on — fail-open
   otherwise.

Goodman prefers `<unknown>` over a guessed package name. Incorrect attribution
is worse than no attribution.

## Quick Start

You need an x86-64 Linux host with kernel 5.8+ and BTF (5.10+ with `bpf` in
`lsm=` for enforcement), plus Go, clang/LLVM, and bpftool. Node is only needed
to rebuild the dashboard.

```bash
make doctor
make build
make demo
```

Open **http://127.0.0.1:8844**. In under a minute you get seeded CRITICAL
alerts, reachability stats, and a live event-stream attack replay.

```bash
make demo-check   # CI / DoD without a browser
make test && make smoke && make replay
make ha-smoke     # optional: 2 collectors vs Docker Postgres (skips if no Docker)
```

Live eBPF (root):

```bash
sudo make e2e
```

## Local Dashboard

```bash
GOODMAN_DSN=goodman.db GOODMAN_LEARN_OBS=50 GOODMAN_LEARN_MIN_AGE=1s \
  ./bin/collector -listen :8844
```

Open `http://localhost:8844`. The UI is embedded in the collector binary.

## Kubernetes

```bash
scripts/install-k8s.sh --cluster prod
```

Inject Tier-1 attribution flags (or enable the admission webhook):

```bash
# Node
scripts/enable-node-attribution.sh --namespace checkout --selector app=api

# Or label the namespace and set webhook.enabled=true in Helm
# (injects NODE_OPTIONS + PYTHONPERFSUPPORT=1)
```

```bash
kubectl -n goodman-system port-forward svc/goodman-collector 8844:8844
```

Production notes: Postgres for HA (`collector.replicas > 1`), SQLite PVC for
pilots, multi-cluster baseline export/import, and enforcement defaults.
See [docs/deployment.md](docs/deployment.md) and
[docs/pilot-runbook.md](docs/pilot-runbook.md).

## Repository Map

| Path | Purpose |
|---|---|
| `bpf/` | eBPF C (tracepoints + optional LSM), shared wire struct |
| `cmd/sensor` | privileged sensor: load eBPF, attribute, spool, enforce heartbeat |
| `cmd/collector` | ingest, fingerprint, diff, enforce state, API, dashboard |
| `cmd/goodmanctl` | alerts, fingerprints export/import, report, enforce, demo |
| `internal/attribute` | stack → package@version (npm + PyPI) |
| `internal/enforce` | literal deny-verdict compilation for LSM |
| `internal/fingerprint` | behavior sets + baseline promotion |
| `internal/diff` | drift + high-risk rules (`alert`/`warn`/`block`) |
| `dashboard/` | React/Vite dashboard source |
| `deploy/` | Dockerfiles, Helm chart, example rules |
| `docs/research/` | Tier-2 / Python / HA / LSM decision + impl plans |
| `test/` | smoke, replay corpus, e2e harness |

## Documentation

| Doc | Purpose |
|---|---|
| [Getting started](docs/getting-started.md) | First alert locally |
| [Setup and usage](docs/setup-and-usage.md) | Full local / k8s / CLI workflow |
| [Architecture](docs/architecture.md) | Components and data flow |
| [Attribution](docs/attribution.md) | npm + PyPI stack → package |
| [Enforcement](docs/enforcement.md) | LSM block mode (fail-open, off by default) |
| [Pilot runbook](docs/pilot-runbook.md) | Install, noise, digest, rollback |
| [Release](docs/release.md) | v0.2.0 gate checklist |
| [Configuration](docs/configuration.md) | Flags, env, Helm values |
| [Deployment](docs/deployment.md) | Helm, HA, Postgres, multi-cluster |
| [API](docs/api.md) | REST + SSE + metrics |
| [Development](docs/development.md) | Dev loop and releasing |
| [Troubleshooting](docs/troubleshooting.md) | Common failures |
| [Agent instructions](AGENTS.md) | Invariants for coding agents |

## Development

```bash
make doctor && make build && make vet && make test && make smoke
```

Touching `bpf/` or `internal/loader/`: also run `sudo make e2e` on a real
kernel. Enforcement changes need an LSM-capable kernel (`CONFIG_BPF_LSM`,
`bpf` in `/sys/kernel/security/lsm`).

Highest-severity invariant: `bpf/goodman.h` `struct event` and
`internal/model/types.go` `RawEvent` stay byte-for-byte identical
(`internal/model/types_test.go`).

## Status

Current `main` includes the deferred v0.2/v0.3 codepath (see
[CHANGELOG](CHANGELOG.md) and [v0.2.0 notes](docs/releases/v0.2.0-notes.md)):

- eBPF capture for file open, connect, and exec
- Tier-1 **npm** (V8 perf maps) and **PyPI** (CPython `PYTHONPERFSUPPORT`)
- Always-on rules, alert evidence, would-block audit (`action: warn`)
- Optional LSM enforcement (`action: block`, off by default, fail-open)
- SQLite (PVC) and Postgres; HA replicas with advisory-lock leader election
- Sensor event spool across collector outages
- Multi-cluster baseline export/import with provenance
- Reachability report, weekly digest, Coverage tab, embedded dashboard
- Helm chart, Docker images, admission webhook for `NODE_OPTIONS` /
  `PYTHONPERFSUPPORT`

Still human-gated for a tagged release: `sudo make e2e` on LSM kernels,
staging two-replica Postgres proof, then tag/push images
([docs/release.md](docs/release.md)).

Tier-2 flagless V8 attribution remains **PARK** (year-scale) —
[docs/research/tier2-attribution.md](docs/research/tier2-attribution.md).

Roadmaps: [plan-pilot.md](plan-pilot.md) (pilot path),
[plan-deferred.md](plan-deferred.md) (v0.2→v0.3 deferred phases — code DONE).

## License

Apache-2.0. See [LICENSE](LICENSE).
