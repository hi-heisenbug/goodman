# Setup and Usage

This is the end-to-end operator guide for getting Goodman running from a fresh
checkout, using the dashboard, and validating that the product is actually
working. It is written for both humans and coding agents.

## Choose Your Path

| Goal | Command | Needs root? | What it proves |
|---|---|---:|---|
| Check machine readiness | `make doctor` | no | Toolchain, kernel, BTF, and eBPF capability status. |
| Build everything locally | `make build` | no | eBPF object, sensor, collector, and CLI compile. |
| Backend correctness | `make smoke` | no | Real collector/store/fingerprint/diff/API path alerts correctly. |
| Product dashboard demo | `make demo` | no | Seeded alerts, reachability 1,400/240, live event-stream replay. |
| Demo DoD check | `make demo-check` | no | Non-interactive verification of the five-minute wow. |
| Real local eBPF demo | `sudo make e2e` | yes | Sensor captures real syscalls and attributes Node package drift. |
| Kubernetes install | `scripts/install-k8s.sh --cluster prod` | cluster-dependent | Installs sensor DaemonSet, collector, dashboard, and service. |

If you are a coding agent, do not stop after `make build`. Use `make smoke` for
the no-root product path, and state clearly if `sudo make e2e` was not run.

## Prerequisites

Local development needs an x86-64 Linux host or VM:

- Linux kernel 5.8+ with BTF at `/sys/kernel/btf/vmlinux`
- Go 1.23+
- `clang`, `llvm`, `bpftool`
- Node 20.19+ only when rebuilding the dashboard

Kubernetes deployment needs:

- Linux x86-64 nodes with kernel 5.8+ and BTF
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

## Fresh Local Setup

```bash
git clone https://github.com/hi-heisenbug/goodman
cd goodman
make doctor
make build
make demo
```

Open **http://127.0.0.1:8844**. Expected result:

- CRITICAL alerts with rule chips already in the queue
- Reachability tab shows **1,400 declared / 240 executed**
- ~12s later, the event-stream / flatmap-stream attack appears live
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
make demo
# or: goodmanctl demo
```

Open:

```text
http://127.0.0.1:8844
```

What happens:

- starts the real collector (`goodmanctl demo`)
- uses a local SQLite database at `demo_build/goodman_demo.db`
- seeds multi-service fingerprints and CRITICAL drift alerts via `/v1/events`
- persists a reachability snapshot (1,400 declared / 240 executed)
- prints a 60-second guided script
- after ~12s, replays the 2018 event-stream attack live
- keeps the embedded React dashboard running until `Ctrl-C`

If port `8844` is busy:

```bash
GOODMAN_DEMO_PORT=8855 make demo
# or: goodmanctl demo -port 8855
```

Useful dashboard routes:

```text
http://127.0.0.1:8844/#alerts
http://127.0.0.1:8844/#fingerprints
http://127.0.0.1:8844/#reachability
```

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

## Run The Real Local eBPF Path

This path proves Goodman can load the sensor, capture real syscalls, attribute
them to a package, and raise a real drift alert:

```bash
sudo make e2e
```

Why sudo is required: the sensor loads eBPF programs and needs root or equivalent
`CAP_BPF`/`CAP_PERFMON` privileges. If you cannot run privileged eBPF on the
host, use `make smoke` and say the live kernel path was not verified.

Manual local tracing flow:

```bash
# terminal 1: collector
GOODMAN_DSN=goodman.db GOODMAN_LEARN_OBS=50 GOODMAN_LEARN_MIN_AGE=1s \
  ./bin/collector -listen :8844

# terminal 2: Node workload with Tier-1 attribution flags
make workload
cd test/workload
node --perf-basic-prof --interpreted-frames-native-stack server.js

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
    value: "--perf-basic-prof --interpreted-frames-native-stack"
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
```

If you cannot run `sudo make e2e`, explicitly say that only the no-root path was
verified.

## Common Setup Failures

- `make doctor` fails: install the missing tool it names first; do not debug the
  product before the host is ready.
- sensor fails with permission errors: run the sensor with root or deploy as the
  privileged DaemonSet.
- dashboard shows no data: confirm the collector is healthy and events were
  ingested with `curl http://127.0.0.1:8844/v1/healthz`.
- Node events show `<unknown>`: start the workload with
  `--perf-basic-prof --interpreted-frames-native-stack`.
- dashboard source changed but UI did not: run `make dashboard`; the collector
  serves `internal/api/ui/dist`, not `dashboard/src`.

More detail is in [Troubleshooting](troubleshooting.md).
