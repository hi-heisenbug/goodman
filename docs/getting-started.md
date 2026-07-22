# Getting started

This guide takes you from a fresh checkout to the portable demo on any Docker
or Go host, then to a real eBPF alert on Linux. No Kubernetes is required.

For the complete local, dashboard, CLI, Kubernetes, and coding-agent workflow,
see [Setup and usage](setup-and-usage.md).

## 1. Prerequisites

The portable demo needs either Go 1.25+ or Docker and runs on Linux, macOS, and
Windows. The live sensor needs an **x86-64 or arm64 Linux host** (bare metal,
VM, or WSL2) with:

- kernel **≥ 5.8** with BTF at `/sys/kernel/btf/vmlinux` (most distros since 2020)
- `go` ≥ 1.25, `clang`, `llvm`, `bpftool`
- `node` ≥ 20.19 (only to rebuild the dashboard — the built UI ships in the repo)

The full setup command with `--install-openclaw` installs Node 22.22.3 when the
host's current Node runtime does not satisfy OpenClaw's engine requirement.

> **Not on Linux?** The portable demo works through Docker Desktop. The live
> sensor still needs a real Linux kernel; use a Linux VM or host for the eBPF
> sections below.

The fastest path on Debian/Ubuntu:

```bash
git clone https://github.com/hi-heisenbug/goodman
cd goodman
./scripts/setup.sh      # installs prerequisites, then runs the doctor
```

Or check what you already have without installing anything:

```bash
make doctor
```

`make doctor` verifies every tool, the kernel config, BTF, and whether you can
load eBPF, and prints exactly what to fix if something's missing.

## 2. Build

```bash
make build       # compiles the eBPF object, builds sensor/collector/goodmanctl into bin/
make test        # unit tests across all packages
```

You now have three binaries in `bin/`:

| Binary | Role |
|---|---|
| `bin/sensor` | loads eBPF, attributes syscalls, ships events to the collector (needs root) |
| `bin/collector` | ingests events, learns baselines, runs the diff engine, serves the API + dashboard |
| `bin/goodmanctl` | CLI to tail events, list alerts, and inspect fingerprints |

## 3. See it work without root (`make smoke`)

The quickest way to watch the whole backend fire an alert — no eBPF, no root:

```bash
make smoke
```

This starts the collector, feeds it synthetic events (a learned baseline, then a
benign version bump that must stay silent, then a *drifted* version), and asserts
that **exactly one CRITICAL alert** appears for `good-pkg 1.0.0 → 1.0.1` with the
new secret read and metadata connect — and that no baseline behavior leaked into
it. If it prints `SMOKE TEST PASSED`, your backend, store, fingerprint engine,
diff engine, and API all work.

## 4. Five-minute product wow on any device

For a no-root product walkthrough — the command used on every discovery call —
start the collector with seeded alerts, a preloaded reachability report, an
OpenClaw skill drift, and a live Mini-Shai-Hulud attack replay:

```bash
bash scripts/setup-everything.sh demo
# force Docker: bash scripts/setup-everything.sh demo --backend docker
```

Open **http://127.0.0.1:8844**. On first load:

- **Alerts** already shows CRITICAL drifts with rule chips
- the initial alert list includes the fictional OpenClaw skill
  `@goodman-demo/calendar-sync@1.2.3`
- **Reachability** shows **1,400 declared / 240 executed** (no lockfile upload)
- ~12 seconds later, the 2026 Mini-Shai-Hulud behavior profile appears live as
  a new CRITICAL row (`secret-read`, `cloud-metadata`,
  `new-outbound-connect`, `new-exec`)

The terminal prints a 60-second guided script. The collector keeps running until
you press `Ctrl-C`. It uses a local SQLite database at
`demo_build/goodman_demo.db`, which is ignored by git.

If port `8844` is already in use:

```bash
bash scripts/setup-everything.sh demo --port 8855
```

Non-interactive verification (no browser):

```bash
bash scripts/setup-everything.sh demo --check
```

## 5. Prove attribution on your own process

Before deploying the whole stack, trace one of your real Node/Python services.
Start it with the Tier-1 runtime switch (no source change), then exercise normal
traffic while Goodman traces for 20 seconds:

```bash
# Node
NODE_OPTIONS="--perf-basic-prof-only-functions --interpreted-frames-native-stack" npm start

# or Python 3.12+
PYTHONPERFSUPPORT=1 python app.py
```

In another terminal:

```bash
bash scripts/setup-everything.sh observe
```

Goodman auto-selects the target only when exactly one supported runtime exists.
Otherwise it prints the candidate list; rerun with `--pid <PID>`. The command
shows unique package behaviors and fails unless at least one exact dependency
identity was proven. Use `--live-backend docker` to avoid installing the host
build toolchain.

## 6. The full live demo with real eBPF (`sudo make e2e`)

This is the real thing: the eBPF sensor captures actual syscalls from a running
Node app and attributes them to the package that made them.

```bash
sudo make e2e
sudo make e2e-openclaw
```

To install missing Debian/Ubuntu prerequisites, optionally install OpenClaw,
and run the portable plus both live proofs in one command:

```bash
bash scripts/setup-everything.sh all --install --install-openclaw
```

To leave the host toolchain and OpenClaw installation untouched, use rootful
Docker on Linux. This still runs the real eBPF programs against the host kernel:

```bash
bash scripts/setup-everything.sh all --live-backend docker
```

What it does (all benign — no real malware, no real exfiltration):

1. Starts the collector and the eBPF sensor.
2. Runs a victim Node service (`test/workload/server.js`) that depends on
   `good-pkg@1.0.0`, drives HTTP traffic, and waits for the baseline to be learned.
3. Swaps the dependency to `good-pkg@1.0.1` — a version that now reads a **fake**
   credentials file and POSTs to a **localhost** sink (the benign stand-in for a
   supply-chain compromise).
4. Asserts a **CRITICAL** alert naming `good-pkg 1.0.0 → 1.0.1` with the new
   secret read and the sink connect.

> **Why sudo?** Loading eBPF programs requires `CAP_BPF`/root. If your host has
> `unprivileged_bpf_disabled` set (check `make doctor`), the sensor must run as
> root. `make docker-e2e` is the disposable rootful-Docker alternative;
> `make smoke` is the no-root backend alternative.

## 7. Run the stack yourself and open the dashboard

Start the collector with a short, dev-friendly learning window:

```bash
GOODMAN_DSN=goodman.db GOODMAN_LEARN_OBS=50 GOODMAN_LEARN_MIN_AGE=1s \
  ./bin/collector -listen :8844
```

Open **http://localhost:8844** — the dashboard is served by the collector itself.
You'll see two screens: the **Alerts feed** and the **Fingerprint Explorer**.

In another terminal, start a Node workload with the profiling flag that Tier-1
attribution needs (one flag, no code change), then point the sensor at it:

```bash
# terminal 2 — the workload
make workload                    # installs good-pkg@1.0.0 into test/workload
cd test/workload
node --perf-basic-prof-only-functions --interpreted-frames-native-stack server.js

# terminal 3 — the sensor (root)
sudo ./bin/sensor -collector http://127.0.0.1:8844

# terminal 4 — watch attributed events stream by
./bin/goodmanctl tail
```

Drive some traffic (`curl localhost:8080/` a few times), and you'll see attributed
`service | package@version | behavior` lines in `goodmanctl tail` and fingerprints
building up in the dashboard's Fingerprint Explorer.

## Next steps

- [Pilot runbook](pilot-runbook.md) — production pilot install and day-2 ops.
- [Enforcement](enforcement.md) — optional LSM block mode (off by default).
- [Configuration](configuration.md) — every flag, env var, and tuning knob.
- [Deployment](deployment.md) — Helm, HA, Postgres, multi-cluster baselines.
- [Architecture](architecture.md) — how the pieces fit together.
- [Release checklist](release.md) — v0.2.0 gate (e2e, tag, images).
- [Troubleshooting](troubleshooting.md) — when something doesn't work.
