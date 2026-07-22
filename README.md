# Goodman

[![CI](https://github.com/hi-heisenbug/goodman/actions/workflows/ci.yml/badge.svg)](https://github.com/hi-heisenbug/goodman/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.25+-00ADD8.svg)](go.mod)
[![Kubernetes](https://img.shields.io/badge/kubernetes-helm-326CE5.svg)](deploy/helm/goodman)

**Goodman** (by [Heisenbug](https://github.com/hi-heisenbug)) watches Node and
Python processes on Linux, attributes `open` / `connect` / `execve` to the
exact **npm or PyPI `package@version`** on the call stack, and can identify
versioned ClawHub skill code. It learns a baseline per
`(service, package, version)` and alerts when that actor starts doing something
new.

Kernel tools say "this process touched a file." SCA tools say "this lockfile
has a CVE." Goodman answers: **which dependency made that syscall.**

Optional eBPF LSM block mode exists. It is fail-open and **off by default**.

![Goodman dashboard showing a critical dependency drift alert](docs/images/dashboard.png)

## Why this exists

After a few supply-chain messes (event-stream, eslint-scope, the 2026
Shai-Hulud wave), "node read your AWS creds" stopped being a useful alert. The
process has hundreds of packages. Operators need the package name and version.

I named it **Goodman** on purpose: short, boring, about the work. The hard
problem is attribution (`internal/attribute/`), not a flashy wrapper around
`auditd`.

## Reproduce the demo anywhere

On Linux, macOS, or WSL, one command chooses local Go when available and
otherwise uses Docker. No root or eBPF support is required:

```bash
bash scripts/setup-everything.sh demo
```

Open **http://127.0.0.1:8844**. You get a seeded OpenClaw/ClawHub skill alert,
reachability numbers, and a live Mini-Shai-Hulud behavior replay. Each behavior
stays tied to the responsible package or versioned skill.

```bash
bash scripts/setup-everything.sh demo --check
bash scripts/setup-everything.sh demo --backend docker --check
make test && make smoke && make replay
```

On any Docker Desktop host, including Windows PowerShell, use Compose:

```bash
docker compose -f deploy/docker/demo.compose.yml up --build
```

Video:

- local: [demo_build/goodman_demo.mp4](demo_build/goodman_demo.mp4)
- Vimeo: https://vimeo.com/1211851029

For the full Linux setup and both real-kernel proofs, including the
OpenClaw-shaped Node runtime contract:

```bash
bash scripts/setup-everything.sh all --install --install-openclaw
```

If the host has rootful Docker but you do not want to install build tools or
use host sudo, run the same real-kernel proofs in a disposable privileged
container:

```bash
bash scripts/setup-everything.sh all --live-backend docker
```

If you are in a sandbox that cannot load BPF, use `make smoke` + `make demo`.
That still exercises store → fingerprint → diff → API → dashboard.

## OpenClaw / agent runtimes

OpenClaw ships as an npm package and runs its Gateway on Node. Current ClawHub
skills install into directories with versioned origin and workspace-lock
metadata. Goodman resolves normal npm frames through `package.json`; when Node
executes JavaScript inside a ClawHub skill, Goodman requires matching
`.clawhub/origin.json` and workspace `.clawhub/lock.json` records before it
reports an exact identity such as `@owner/skill@1.2.3`.

Preview or install the host integration:

```bash
scripts/integrate-openclaw.sh --dry-run
scripts/integrate-openclaw.sh
scripts/integrate-openclaw.sh --install-openclaw --systemd-user --restart
```

`make demo` includes a fictional `service=openclaw` skill drift with a
credential read and new outbound connection. Full host and Kubernetes steps:
[docs/openclaw.md](docs/openclaw.md).

Dashboard-independent integration surface:

| Endpoint | Role |
|---|---|
| `POST /v1/events` | sensor ingest |
| `GET /v1/alerts` | open / ack / resolved |
| `GET /v1/stream` | SSE |
| `GET /v1/fingerprints` | baselines |
| `GET /v1/export` | all alert states, fingerprints, reachability, coverage, enforcement |

Full reference: [docs/api.md](docs/api.md).

## What it catches

- package starts reading secrets, tokens, SSH keys, `.npmrc`, cloud creds
- new outbound connect (incl. `169.254.169.254` metadata)
- new `execve` where the baseline had none
- any new canonical behavior vs the learned set for that `package@version`
- **always-on** high-risk rules during the learning window (so a malicious
  package cannot quietly poison the baseline on day one)

Block mode: set rule `action: "block"` and turn on the Helm/sensor gates.
Details: [docs/enforcement.md](docs/enforcement.md).

## How it works

```text
kernel (tracepoints + optional LSM)
open / connect / execve
        |
        v
sensor: syscall + user stack → attribute → batch/spool
        |
        v
collector: fingerprint · diff · alerts · API / SSE / UI
```

1. **Capture** — CO-RE eBPF on `open`/`openat`/`openat2`, `connect`, `execve`
   for watched Node/Python processes; grab the user stack.
2. **Attribute** — resolve through V8 / CPython perf maps and
   `/proc/<pid>/maps` (inside the target mount ns via `/proc/<pid>/root`),
   then map the deepest `node_modules/` or `site-packages/` frame to a version.
3. **Fingerprint** — stable strings like `READ …/.aws/credentials` or
   `CONNECT 169.254.169.254:80`.
4. **Diff** — live set vs baseline; rules are config (`alert` / `warn` /
   `block`), not a pile of hard-coded `if`s.
5. **Enforce (optional)** — LSM deny maps with literal paths/addresses only,
   and only when master gate + runtime switch + namespace label are all on.

If we are not sure which package owns the frame, we emit `<unknown>`. A wrong
package name is worse than an unknown one. That rule is non-negotiable.

Wire format invariant: `bpf/goodman.h` `struct event` and Go `RawEvent` are the
same bytes. Offset tests in `internal/model/types_test.go` fail the build if
they drift. Do not "fix" the test; fix the layout.

## Quick start (dev machine)

The portable path needs Go 1.25+ or Docker on Linux, macOS, or Windows:

```bash
bash scripts/setup-everything.sh demo
```

The live sensor needs x86-64 or arm64 Linux, kernel ≥ 5.8 with BTF (5.10+ and
`bpf` in `lsm=` for enforcement), clang/LLVM, and bpftool:

```bash
bash scripts/setup-everything.sh all --install
```

Or keep the host toolchain untouched and use rootful Docker:

```bash
make docker-e2e
```

Collector alone:

```bash
GOODMAN_DSN=goodman.db GOODMAN_LEARN_OBS=50 GOODMAN_LEARN_MIN_AGE=1s \
  ./bin/collector -listen :8844
```

## Kubernetes

```bash
scripts/install-k8s.sh --cluster prod
scripts/enable-node-attribution.sh --namespace checkout --selector app=api
kubectl -n goodman-system port-forward svc/goodman-collector 8844:8844
```

Postgres for HA (`collector.replicas > 1`), SQLite PVC for pilots. See
[docs/deployment.md](docs/deployment.md) and
[docs/pilot-runbook.md](docs/pilot-runbook.md).

## Repo map

| Path | What it is |
|---|---|
| `bpf/` | eBPF C + shared wire struct |
| `cmd/sensor` | load BPF, attribute, spool, enforce heartbeat |
| `cmd/collector` | ingest, fingerprint, diff, API, dashboard |
| `cmd/goodmanctl` | CLI + `demo` |
| `internal/attribute` | stack → package@version (the hard part) |
| `internal/fingerprint` / `internal/diff` | baselines + rules |
| `dashboard/` | React/Vite source → `internal/api/ui/dist` |
| `deploy/` | Docker + Helm + example rules |
| `test/` | smoke, replay corpus, e2e |

## Docs

| Doc | Purpose |
|---|---|
| [Getting started](docs/getting-started.md) | first alert locally |
| [Setup and usage](docs/setup-and-usage.md) | full workflow |
| [Architecture](docs/architecture.md) | design |
| [Attribution](docs/attribution.md) | npm + PyPI + ClawHub resolve |
| [OpenClaw](docs/openclaw.md) | one-command host/Kubernetes integration |
| [API](docs/api.md) | REST + SSE |
| [Enforcement](docs/enforcement.md) | LSM block mode |
| [AGENTS.md](AGENTS.md) | invariants for coding agents |

## Built with Codex (GPT-5.6)

I used Codex across the build, not as a greenfield "write me an app" pass.
Typical loop: I state the invariant, Codex proposes a patch, I run
`make test` / `make smoke`, I reject anything that guesses package names or
blocks the ringbuf reader.

Places that collaboration actually showed up:

- **Wire layout** — keep C `struct event` and Go `RawEvent` aligned; layout
  tests are the referee
- **Attribution** — container `/proc/<pid>/root` perf maps, `dirfd` relative
  paths, npm vs PyPI package roots
- **Diff / rules** — config-driven high-risk list, always-on path during learn
- **API + Helm** — ingest vs API token classes, env/flag/values kept in sync
- **Dashboard** — live `/v1/*` + SSE, not mock rows in production views
- **Judge path** — `make demo`, smoke, and the replay corpus so you can score
  the alert pipeline without loading BPF

I read the diffs. If I cannot explain a change, it does not ship. Codex is a
pair programmer; the product calls stay mine.

## Status

Shipped on `main` (see [CHANGELOG](CHANGELOG.md)):

- eBPF capture for open / connect / exec
- Tier-1 npm (V8 perf maps) and PyPI (`PYTHONPERFSUPPORT`)
- always-on rules, alert evidence, `warn` = would-block audit
- optional LSM `block` (off by default, fail-open)
- SQLite + Postgres, HA leader election, sensor spool
- multi-cluster fingerprint export/import
- reachability, weekly digest, coverage tab, embedded UI
- Helm, Docker, admission webhook for attribution flags

Still human-gated before a tagged release: a live LSM-kernel proof
(`sudo make e2e` or `make docker-e2e`), two-replica Postgres proof, then image tag
([docs/release.md](docs/release.md)).

Tier-2 flagless V8 attribution is **PARK** —
[docs/research/tier2-attribution.md](docs/research/tier2-attribution.md).

## License

Apache-2.0. See [LICENSE](LICENSE).
