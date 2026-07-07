# Goodman

**Goodman is a runtime security sensor that attributes each kernel syscall to the specific npm/PyPI package that caused it, builds a behavioral fingerprint per (package, version, service), and alerts when a dependency's behavior drifts from its baseline.** It closes the gap the kernel can't: a Node process makes a syscall, but the kernel only knows the *process* made it — not which of its 79 dependencies did. Goodman uses eBPF to grab the user-space call stack at each security-relevant syscall, resolves that stack to a JavaScript source path, and maps it to the exact `package@version` that acted — then flags any new behavior a version introduces (a secret read, a call to cloud metadata, a new outbound connection) as drift.

---

## How it works

```
  ┌──────────────┐   syscall + user stack    ┌────────────┐   attributed events   ┌───────────────┐
  │  eBPF sensor │ ─────────(ring buffer)───▶ │ attributor │ ───(gzip JSON POST)──▶ │   collector   │
  │ (DaemonSet)  │  openat / connect / execve │ stack→pkg  │                        │ fingerprint + │
  └──────────────┘                            └────────────┘                        │ diff + alerts │
        kernel                                   user space                          └──────┬────────┘
                                                                                            │ REST + SSE
                                                                                     ┌──────▼────────┐
                                                                                     │  React dash   │
                                                                                     └───────────────┘
```

1. **Capture** — an eBPF program (`bpf/goodman.bpf.c`, CO-RE) hooks the `openat`, `connect`, and `execve` tracepoints for watched Node/Python pids and grabs the user-space stack via `bpf_get_stack` (frame pointers, on-by-default in Node since 2022).
2. **Attribute** (`internal/attribute/`) — each stack address is resolved through `/tmp/perf-<pid>.map` (V8's JIT symbol map, Tier 1) or `/proc/<pid>/maps` + ELF (native frames). The **deepest frame inside a `node_modules/<pkg>/` directory** is the actor; its version comes from that package's `package.json`. No node_modules frame → attributed to `<app>`; nothing resolvable → `<unknown>`. **Goodman never guesses a package name — unknown is honest, misattribution is not.**
3. **Fingerprint** (`internal/fingerprint/`) — behaviors are canonicalized (`READ /app/src/**`, `CONNECT 1.2.3.4:443`, `EXEC curl`; sensitive paths kept verbatim) and aggregated per `(service, package, version)`. After a learning window (default 500 obs / 24h) the set is promoted to a **baseline**.
4. **Diff** (`internal/diff/`) — when a new version appears or a baseline version does something new, the novel behaviors are the drift. A config-driven high-risk rule list (secret read, cloud-metadata `169.254.169.254`, new outbound connect, new exec) escalates drift to **CRITICAL**.

## Repository layout

| Path | What |
|---|---|
| `bpf/` | eBPF C program + shared `goodman.h` struct + generated `vmlinux.h` |
| `cmd/sensor` | eBPF loader + attributor (runs as DaemonSet) |
| `cmd/collector` | ingest + fingerprint + diff + API + embedded dashboard |
| `cmd/goodmanctl` | dev CLI: `tail`, `alerts`, `ack`, `fingerprints`, `attribute` |
| `internal/{loader,attribute,model,store,fingerprint,diff,api}` | the pipeline |
| `dashboard/` | React + Vite + TypeScript UI (embedded into the collector) |
| `deploy/{docker,helm}` | multi-stage Dockerfiles + Helm chart |
| `test/{workload,fixtures,e2e}` | victim service, benign drift fixtures, e2e harness |

## Quick start (local, Linux)

Requires an x86-64 Linux host, kernel ≥ 5.8 with BTF (`/sys/kernel/btf/vmlinux`), and `clang`, `go`, `node`, `bpftool`.

```bash
make build          # compiles the eBPF object, embeds the dashboard, builds all 3 binaries
make test           # unit tests (attribution, model round-trip, full diff pipeline)
make smoke          # backend end-to-end WITHOUT root (synthetic events → alert assertion)
sudo make e2e       # FULL eBPF drift replay: real sensor + Node workload → CRITICAL alert
```

`make e2e` reproduces a supply-chain attack **benignly**: it runs the victim service
(`test/workload/server.js`) against `good-pkg@1.0.0`, learns a baseline, swaps to
`good-pkg@1.0.1` (which reads a fake credentials file and POSTs to a localhost sink),
and asserts a single CRITICAL alert naming `good-pkg 1.0.0 → 1.0.1`.

> **Root:** the sensor loads eBPF programs, which requires `CAP_BPF`/root (this
> kernel has `unprivileged_bpf_disabled=2`). `make smoke` needs no root and
> exercises everything except the kernel capture path.

### Inspect a single process

```bash
node --perf-basic-prof --interpreted-frames-native-stack test/workload/server.js &
sudo ./bin/goodmanctl attribute -pid $! -stacks
```

## Kubernetes (the "one helm command")

```bash
helm install goodman deploy/helm/goodman \
  --set cluster=prod \
  --set registries=npm,pypi
kubectl port-forward svc/goodman-collector 8844:8844   # open http://localhost:8844
```

Tier-1 attribution needs each watched Node deployment to start with
`NODE_OPTIONS=--perf-basic-prof --interpreted-frames-native-stack` — one env
var, no code change. (Auto-injection via a mutating webhook is a v1.1 item;
Tier-2 in-kernel V8 unwinding removes the flag entirely.)

`make kind-e2e` spins up a kind cluster on a Linux host and runs the whole
install + drift scenario in-cluster.

## Configuration (Helm `values.yaml` / collector flags)

| Key | Default | Meaning |
|---|---|---|
| `cluster` | `dev` | deployment identity |
| `learningWindow.obsCount` | `500` | observations before baseline promotion |
| `learningWindow.minAgeHours` | `24` | wall-clock age before promotion |
| `postgres.dsn` | `""` | Postgres DSN; empty → embedded SQLite (pilot) |
| `attribution.tier` | `perfmap` | `perfmap` (Tier 1) or `v8native` (Tier 2, not in v1) |
| `rules` | built-in | JSON high-risk rule list override |

## Observability

The sensor and collector both expose Prometheus metrics:
`goodman_sensor_events_total`, `goodman_sensor_attributed_total{outcome}`,
`goodman_sensor_ringbuf_drops_total`, `goodman_collector_events_ingested_total`,
`goodman_collector_alerts_total{severity}`.

## Overhead

By design the sensor only traces **watched pids** (Node/Python) and only three
low-volume syscall classes (`openat`/`connect`/`execve`) — never `read`/`write`.
Measure on your workload with `make e2e` under `autocannon` and record the CPU
delta; the design target is **< 2%**.

## Status vs. plan

Built and verified: shared model (§4), eBPF capture with user-stack (§6),
Tier-1 attribution (§5), store + fingerprint + baseline promotion (§8), diff
engine + high-risk rules (§9), benign drift e2e (§10), Docker + Helm (§11), API
+ dashboard (§12), Prometheus metrics (§14). Fast-follow (architected, not
built): mutating-webhook auto-injection, Python/PyPI resolver, Tier-2 in-kernel
V8 unwinding.
