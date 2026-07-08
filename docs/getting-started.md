# Getting started

This guide takes you from a fresh Linux host to your first drift alert in about
ten minutes. No Kubernetes required — everything runs locally.

For the complete local, dashboard, CLI, Kubernetes, and coding-agent workflow,
see [Setup and usage](setup-and-usage.md).

## 1. Prerequisites

You need an **x86-64 Linux host** (bare metal, VM, or WSL2) with:

- kernel **≥ 5.8** with BTF at `/sys/kernel/btf/vmlinux` (most distros since 2020)
- `go` ≥ 1.23, `clang`, `llvm`, `bpftool`
- `node` ≥ 20.19 (only to rebuild the dashboard — the built UI ships in the repo)

> **Not on Linux?** The sensor needs a real kernel. On macOS/Windows, spin up a
> Linux VM (multipass, UTM, Lima, or any cloud instance) and work there. Docker
> Desktop's LinuxKit VM lacks the kernel headers/BTF and will not work.

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

## 4. Open the product dashboard with demo data (`make demo`)

For a no-root product walkthrough, start the collector with realistic seeded
fingerprints and drift alerts:

```bash
make demo
```

Open **http://127.0.0.1:8844**. The command keeps the collector running until
you press `Ctrl-C`. It uses a local SQLite database at
`demo_build/goodman_demo.db`, which is ignored by git.

If port `8844` is already in use:

```bash
GOODMAN_DEMO_PORT=8855 make demo
```

## 5. The full live demo with real eBPF (`sudo make e2e`)

This is the real thing: the eBPF sensor captures actual syscalls from a running
Node app and attributes them to the package that made them.

```bash
sudo make e2e
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
> root. `make smoke` is the no-root alternative for the backend.

## 6. Run the stack yourself and open the dashboard

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
node --perf-basic-prof --interpreted-frames-native-stack server.js

# terminal 3 — the sensor (root)
sudo ./bin/sensor -collector http://127.0.0.1:8844

# terminal 4 — watch attributed events stream by
./bin/goodmanctl tail
```

Drive some traffic (`curl localhost:8080/` a few times), and you'll see attributed
`service | package@version | behavior` lines in `goodmanctl tail` and fingerprints
building up in the dashboard's Fingerprint Explorer.

## Next steps

- [Configuration](configuration.md) — every flag, env var, and tuning knob.
- [Deployment](deployment.md) — install on Kubernetes with one Helm command.
- [Architecture](architecture.md) — how the pieces fit together.
- [Troubleshooting](troubleshooting.md) — when something doesn't work.
