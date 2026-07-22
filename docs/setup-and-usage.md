# Setup and Usage

This is the end-to-end operator guide for getting Goodman running from a fresh
checkout, using the dashboard, and validating that the product is actually
working. It is written for both humans and coding agents.

## Choose Your Path

| Goal | Command | Needs root? | What it proves |
|---|---|---:|---|
| Set up and verify the portable demo | `bash scripts/setup-everything.sh demo --check` | no | Chooses local Go or Docker and verifies the complete demo contract. |
| Set up and verify everything on Linux | `bash scripts/setup-everything.sh all --install --install-openclaw` | yes | Installs supported host prerequisites and OpenClaw, then runs portable plus both live eBPF proofs. |
| Verify everything with a clean host | `bash scripts/setup-everything.sh all --backend docker --live-backend docker` | Docker | Runs the portable demo, then both real-kernel proofs in disposable containers. |
| Check machine readiness | `make doctor` | no | Toolchain, kernel, BTF, LSM capability status. |
| Build everything locally | `make build` | no | eBPF object, sensor, collector, and CLI compile. |
| Backend correctness | `make smoke` | no | Collector/store/fingerprint/diff/API (+ enforce pipeline without kernel). |
| Attack replay corpus | `make replay` | no | npm incident and integration fixtures raise the expected CRITICAL alerts. |
| Product dashboard demo | `make demo` | no | OpenClaw skill drift, reachability, live Mini-Shai-Hulud replay. |
| Demo DoD check | `make demo-check` | no | Non-interactive verification of the five-minute wow. |
| Prove attribution on your app | `bash scripts/setup-everything.sh observe` | yes or Docker | Traces a real Node/Python PID and fails unless an exact dependency is attributed. |
| HA ingest smoke | `make ha-smoke` | Docker | Two collectors vs Postgres: fingerprint parity + alert dedup (skips without Docker). |
| Real local eBPF demo | `sudo make e2e` | yes | Sensor captures real syscalls and attributes package drift. |
| Containerized real-kernel e2e | `make docker-e2e` | Docker | Runs OpenClaw attribution and drift/LSM enforcement against the Linux host kernel. |
| Kubernetes install | `scripts/install-k8s.sh --cluster prod` | cluster-dependent | Installs sensor DaemonSet, collector, dashboard, and service. |

If you are a coding agent, do not stop after `make build`. Use `make smoke` for
the no-root product path, and state clearly if `sudo make e2e` was not run.

## Prerequisites

The portable demo needs Go 1.25+ or Docker and works on Linux, macOS, and
Windows. Live sensor development needs an x86-64 or arm64 Linux host or VM:

- Linux kernel 5.8+ with BTF at `/sys/kernel/btf/vmlinux`
- Go 1.25+
- `clang`, `llvm`, `bpftool`
- Node 20.19+ only when rebuilding the dashboard

When `--install` and `--install-openclaw` are combined, the setup path installs
Node 22.22.3 if the host does not already satisfy OpenClaw's current engine
range.

Kubernetes deployment needs:

- Linux x86-64 or arm64 nodes with kernel 5.8+ and BTF
- Privileged DaemonSets allowed
- `kubectl` and `helm` on the operator machine
- Postgres for production persistence, or default SQLite for a pilot

On Debian/Ubuntu:

```bash
./scripts/setup.sh
```

On other systems, install the prerequisites manually and run:

```bash
make doctor
```

## Fresh Portable Setup

```bash
git clone https://github.com/hi-heisenbug/goodman
cd goodman
bash scripts/setup-everything.sh demo
```

Force the Docker path with `--backend docker`. On Windows PowerShell, where
Bash may not be installed, run:

```bash
docker compose -f deploy/docker/demo.compose.yml up --build
```

Open **http://127.0.0.1:8844**. Expected result:

- CRITICAL alerts with rule chips already in the queue
- an OpenClaw `@goodman-demo/calendar-sync@1.2.3` skill alert
- Reachability tab shows **1,400 declared / 240 executed**
- ~12s later, the 2026 Mini-Shai-Hulud behavior replay appears live
- no root is required for this path

Backend regression without the UI:

```bash
make smoke
make replay
make demo-check
```

## Run The Five-Minute Product Wow

Use this on every discovery call and every "try it yourself" link:

```bash
bash scripts/setup-everything.sh demo
```

Open:

```text
http://127.0.0.1:8844
```

What happens:

- starts the real collector through the portable `goodman-demo` runner
- uses a local SQLite database at `demo_build/goodman_demo.db`
- seeds multi-service fingerprints and CRITICAL drift alerts via `/v1/events`
- replays a fictional OpenClaw/ClawHub skill drift before the dashboard opens
- persists a reachability snapshot (1,400 declared / 240 executed)
- prints a 60-second guided script
- after ~12s, replays the 2026 Mini-Shai-Hulud behavior profile live
- keeps the embedded React dashboard running until `Ctrl-C`

If port `8844` is busy:

```bash
bash scripts/setup-everything.sh demo --port 8855
```

Useful dashboard routes:

```text
http://127.0.0.1:8844/#alerts
http://127.0.0.1:8844/#fingerprints
http://127.0.0.1:8844/#reachability
```

## Prove Goodman On Your Own Workload

Use this before a full deployment when a user or judge wants proof against an
existing service rather than a Goodman fixture. No collector or database is
needed; this isolates the hardest claim, syscall-to-package attribution.

Start or restart the workload with the relevant Tier-1 runtime switch:

```bash
# Node: applies to npm apps and OpenClaw Gateway
NODE_OPTIONS="--perf-basic-prof-only-functions --interpreted-frames-native-stack" npm start

# CPython 3.12+
PYTHONPERFSUPPORT=1 python app.py
```

Then, from the Goodman checkout:

```bash
bash scripts/setup-everything.sh observe
```

When exactly one supported Node/Python runtime is running, Goodman selects it.
When several are present, it prints their PID, comm, and command without
guessing; choose one explicitly:

```bash
bash scripts/setup-everything.sh observe --pid 1234 --duration 30s
```

Send normal HTTP requests, jobs, or CLI actions to the service during the trace.
The output shows each unique `service | package@version | behavior` once, then a
deterministic proof summary. `-verify` is enabled by this setup path, so zero
events or only `<app>` / `<unknown>` attribution returns a non-zero exit code
with the exact runtime/traffic hint to fix it.

Only the eBPF tracing process is elevated; Goodman never starts the target app
as root. If the host lacks Go/clang/bpftool but has rootful Docker, keep the
host toolchain untouched:

```bash
bash scripts/setup-everything.sh observe --pid 1234 --live-backend docker
```

This container still observes the selected host PID and runs eBPF against the
host kernel (`--pid=host`, host cgroup/tracefs/debugfs/securityfs mounts). Add
`--stacks` only when debugging attribution; the default deduplicated view is
better for a first proof.

## Run The Collector Manually

Use this when developing against a live API:

```bash
GOODMAN_DSN=goodman.db GOODMAN_LEARN_OBS=50 GOODMAN_LEARN_MIN_AGE=1s \
  ./bin/collector -listen :8844
```

Open the dashboard at:

```text
http://127.0.0.1:8844
```

Health check:

```bash
curl -sf http://127.0.0.1:8844/v1/healthz
```

## Use The CLI

With a collector running:

```bash
./bin/goodmanctl alerts
./bin/goodmanctl fingerprints
./bin/goodmanctl export -o goodman-export.json
./bin/goodmanctl tail
```

Point the CLI at a non-default collector:

```bash
GOODMAN_COLLECTOR_URL=http://127.0.0.1:8855 ./bin/goodmanctl alerts
```

Acknowledge or resolve an alert:

```bash
./bin/goodmanctl ack <alert-id>
./bin/goodmanctl resolve <alert-id>
```

## Integrate OpenClaw

Preview the integration on any CI host, even when OpenClaw is absent:

```bash
scripts/integrate-openclaw.sh --dry-run
```

Create the local env file and Tier-1 launcher:

```bash
scripts/integrate-openclaw.sh
```

Optionally install OpenClaw into a user-local Goodman prefix and persist the
required environment in its user systemd service:

```bash
scripts/integrate-openclaw.sh \
  --install-openclaw --systemd-user --restart
```

For Kubernetes:

```bash
scripts/integrate-openclaw.sh --k8s -n agents -l app=openclaw
```

Without `--install-openclaw`, the script leaves the OpenClaw installation
untouched. It detects the CLI and running process, writes the local config,
prepares `openclaw-goodman`, and prints exact collector, sensor, Gateway, and
complete-export commands. Kubernetes patches preserve existing literal
`NODE_OPTIONS`; `valueFrom` values are rejected instead of overwritten. See
[OpenClaw integration](openclaw.md) for the trust boundary and daemon setup.

## Run The Real Local eBPF Path

This path proves Goodman can load the sensor, capture real syscalls, attribute
them to a package, and raise a real drift alert:

```bash
sudo make e2e
sudo make e2e-openclaw
```

Why sudo is required: the sensor loads eBPF programs and needs root or equivalent
`CAP_BPF`/`CAP_PERFMON` privileges. If you cannot run privileged eBPF on the
host directly but have rootful Docker, use:

```bash
make docker-e2e
```

The container is disposable but the proof is not synthetic: it uses the host
kernel, PID namespace, tracefs/debugfs/securityfs, and cgroup hierarchy. If
neither host root nor privileged Docker is available, use `make smoke` and say
the live kernel path was not verified.

Manual local tracing flow:

```bash
# terminal 1: collector
GOODMAN_DSN=goodman.db GOODMAN_LEARN_OBS=50 GOODMAN_LEARN_MIN_AGE=1s \
  ./bin/collector -listen :8844

# terminal 2: Node workload with Tier-1 attribution flags
make workload
cd test/workload
node --perf-basic-prof-only-functions --interpreted-frames-native-stack server.js

# terminal 3: sensor
sudo ./bin/sensor -collector http://127.0.0.1:8844

# terminal 4: live stream
./bin/goodmanctl tail
```

Drive workload traffic:

```bash
curl http://127.0.0.1:8080/
```

## Production Kubernetes Setup

Install Goodman into the current `kubectl` context:

```bash
scripts/install-k8s.sh --cluster prod
```

Production persistence with Postgres:

```bash
scripts/install-k8s.sh --cluster prod --postgres-dsn "$GOODMAN_POSTGRES_DSN"
```

Open the dashboard:

```bash
kubectl -n goodman-system port-forward svc/goodman-collector 8844:8844
```

Then open:

```text
http://127.0.0.1:8844
```

Enable Tier-1 Node attribution on selected workloads:

```bash
scripts/enable-node-attribution.sh --namespace checkout --selector app=api
```

Or patch every Deployment in a namespace:

```bash
scripts/enable-node-attribution.sh --namespace checkout --all
```

This adds:

```yaml
env:
  - name: NODE_OPTIONS
    value: "--perf-basic-prof-only-functions --interpreted-frames-native-stack"
```

The env var changes pod templates, so Kubernetes will roll the selected
Deployments.

## Dashboard Development

The collector serves the committed production dashboard from
`internal/api/ui/dist`. For frontend development:

```bash
cd dashboard
npm install
npm run dev
```

Run a collector separately on `:8844`; the Vite dev server proxies `/v1/*` to it.

Before committing dashboard changes:

```bash
make dashboard
(cd dashboard && npm audit --audit-level=moderate)
make build
make smoke
```

Commit both:

- `dashboard/src/...`
- rebuilt `internal/api/ui/dist/...`

## Required Validation Before Hand-off

For most code or docs changes:

```bash
make vet
make test
make smoke
```

For dashboard changes:

```bash
make dashboard
(cd dashboard && npm audit --audit-level=moderate)
make build
make smoke
```

For eBPF, loader, or attribution changes:

```bash
sudo make e2e
# or, on a Linux host with rootful Docker:
make docker-e2e
```

If neither live path can run, explicitly say that only the no-root path was
verified.

## Common Setup Failures

- `make doctor` fails: install the missing tool it names first; do not debug the
  product before the host is ready.
- sensor fails with permission errors: run the sensor with root or deploy as the
  privileged DaemonSet.
- privileged Docker e2e cannot attach tracepoints/LSM: use `make docker-e2e` or
  `setup-everything`; their command mounts host tracefs, debugfs, securityfs,
  and cgroup v2 explicitly.
- dashboard shows no data: confirm the collector is healthy and events were
  ingested with `curl http://127.0.0.1:8844/v1/healthz`.
- Node events show `<unknown>`: start the workload with
  `--perf-basic-prof-only-functions --interpreted-frames-native-stack`.
- dashboard source changed but UI did not: run `make dashboard`; the collector
  serves `internal/api/ui/dist`, not `dashboard/src`.

More detail is in [Troubleshooting](troubleshooting.md).
