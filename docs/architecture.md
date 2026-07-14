# Architecture

Goodman answers a question the kernel can't: **which dependency made this
syscall?** The kernel knows a *process* opened a file or opened a socket. It does
not know which of the process's dozens of npm/PyPI packages was on the call stack
when it happened. Goodman closes that gap and turns it into a behavioral security
signal.

npm and PyPI are the first wedge (not the only ecosystems that could matter
later). See [Why npm and PyPI](attribution.md#why-npm-and-pypi-not-every-ecosystem-yet)
in the attribution doc.

## The four steps

```
   ┌─────────── kernel ───────────┐   ┌──────────────── user space ─────────────────┐
                                                                                       
   syscall (open/openat/openat2/connect/execve)
        │                                                                              
        ▼                                                                              
   ┌─────────┐  event + user stack   ┌──────────┐  package@version   ┌─────────────┐  
   │  eBPF   │ ───(ring buffer)────▶ │ attribute │ ──────────────────▶│ fingerprint │  
   │ program │  (frame-pointer walk) │  resolve  │   + behavior       │  aggregate  │  
   └─────────┘                       └──────────┘                     └──────┬──────┘  
                                                                             │         
                                                              per-fp update  ▼         
                                                                       ┌──────────┐    
                                                                       │   diff   │    
                                                                       │  engine  │    
                                                                       └────┬─────┘    
                                                                    alert   │          
                                                                            ▼          
                                                              ┌───────────────────────┐
                                                              │ store (PG / SQLite)    │
                                                              │ api (REST + SSE)       │
                                                              │ dashboard (embedded)   │
                                                              └───────────────────────┘
```

1. **Capture** — eBPF programs hook low-volume security syscall tracepoints and, for each
   event from a *watched* process, grabs the user-space call stack.
2. **Attribute** — user space resolves that stack to a source file, finds the
   deepest frame inside `node_modules/<pkg>/` or `site-packages/<pkg>/`
   (or `dist-packages/`), and reads the version from `package.json` or the
   adjacent `*.dist-info` metadata. Frozen / unresolvable frames stay
   `<app>` / `<unknown>` — never a guess.
3. **Fingerprint** — behaviors are canonicalized and aggregated into a set per
   `(service, package, version)`; after a learning window the set becomes a baseline.
4. **Diff** — new behavior that isn't in the baseline is drift; high-risk rules
   escalate with `action: alert | warn | block` (warn = would-block audit;
   block = same alert path plus optional LSM deny when armed).
5. **Enforce (optional)** — LSM hooks (`file_open` / `socket_connect` /
   `bprm_check_security`) exact-match literal deny maps only when the master
   gate, runtime switch, and `goodman.io/enforce=enabled` scope are all on.
   Fail-open otherwise. See [`enforcement.md`](enforcement.md).

## Processes

Goodman ships two long-running binaries plus a CLI.

### Sensor (`cmd/sensor`)

Runs on every node (a privileged DaemonSet in Kubernetes, or as root locally).

- Loads the embedded eBPF object and attaches the syscall tracepoints
  (`internal/loader`).
- Maintains the in-kernel `watched_pids` map by periodically scanning `/proc` for
  built-in runtime comm names (`node`, `nodejs`, `MainThread`, `python`,
  `python3`, versioned `python3.12`/`python3.13`, `gunicorn`, `celery`,
  `uwsgi`, `uvicorn`) plus configured extras (`RefreshWatched` / `-comms`).
- Reads `RawEvent`s from the ring buffer, resolves each to an `Attributed` event
  (`internal/attribute`), including `openat*` dirfd and container-mount path
  resolution, and batches them to the collector over gzip'd HTTP.
  Failed POSTs go into a bounded RAM spool (`-spool-events`) and retry.
- When `-enforce-enabled`, optionally attaches LSM programs, heartbeats the
  enforce deadline, and reconciles cgroup scope + deny maps from the collector.
- **Never blocks the ring-buffer reader on the network:** events flow through a
  buffered channel; if it fills, events are dropped and counted
  (`goodman_sensor_channel_drops_total`).
- Exposes its own Prometheus metrics.

### Collector (`cmd/collector`)

Serves everything the outside world touches: ingest, fingerprint aggregation,
diff, alerts API, SSE stream, embedded dashboard, and Prometheus metrics. In
production you may run **N replicas** behind the existing Service when backed
by Postgres (`GOODMAN_HA_REPLICAS` / `collector.replicas`); SQLite remains the
single-replica dev/pilot path. Singleton side-effecting loops (retention prune,
reachability refresh, weekly digest) use Postgres advisory-lock leader election
so only one replica fires per tick. Concurrent ingest uses transactional
`MergeFingerprint` and `UpsertAlert` (row locks on Postgres).

- `POST /v1/events` ingests batches, runs fingerprint aggregation
  (`internal/fingerprint`), then the diff engine (`internal/diff`).
- Persists fingerprints and alerts (`internal/store`).
- Serves the REST API, a Server-Sent-Events stream, Prometheus metrics, and the
  embedded React dashboard (`internal/api`).

### goodmanctl (`cmd/goodmanctl`)

Operator CLI: `tail` (live SSE stream), `alerts`, `ack`, `fingerprints`, and
`attribute` (one-shot live attribution of a single pid — handy for debugging the
resolver).

## Why eBPF, and why now

- **eBPF** lets Goodman observe every process on a node from the kernel with no
  code changes to the applications and negligible overhead — the sensor is a
  single privileged pod per node, not a library each app must adopt.
- **CO-RE** (Compile Once, Run Everywhere) means one eBPF object built against
  `vmlinux.h` runs across kernel versions; struct differences are relocated at
  load time.
- **Frame pointers** are the enabling precondition: `bpf_get_stack(...,
  BPF_F_USER_STACK)` walks the user stack using frame pointers, and Node/V8 have
  shipped frame pointers on by default since 2022. Without them the stack walk
  would be unreliable.

## Keeping overhead low (< 2% target)

Overhead is a feature, not an afterthought. Two design choices keep it bounded:

- **Only watched pids.** The eBPF programs early-return for any process not in the
  `watched_pids` map, so syscalls from the rest of the system cost a single hash
  lookup.
- **Only low-volume security syscalls.** Goodman traces file opens (`open`,
  `openat`, `openat2`), `connect`, and `execve` — deliberately **not**
  `read`/`write`, which are orders of magnitude higher volume. A file *open*
  already tells you what a package touches.

If overhead regresses, the next levers are a tighter syscall set and in-kernel
per-`(pid, behavior)` deduplication so identical behaviors aren't re-sent.

## The store

`internal/store` speaks `database/sql` over **one codepath** that runs on both:

- **PostgreSQL** in production (behavior sets as `JSONB`).
- **SQLite** for local dev and pilots (single writer, WAL; behavior sets as JSON text).

Migrations are dialect-suffixed (`001_init.postgres.sql`, `001_init.sqlite.sql`)
and applied at startup. `$N` placeholders and `ON CONFLICT ... DO UPDATE` work in
both engines.

## Data model

The types that flow through the system live in `internal/model`:

- **`RawEvent`** — the exact bytes the kernel writes (mirrors `struct event` in
  `bpf/goodman.h`), including the `openat*` dirfd needed for safe relative-path
  resolution.
- **`Attributed`** — a `RawEvent` after resolution: `(service, package, version,
  behavior, timestamp)`.
- **`Fingerprint`** — the behavior set for one `(service, package, version)`, plus
  observation count, first/last seen, and the `is_baseline` flag.
- **`Alert`** — a drift finding: old/new version, severity, the new behaviors, and
  a lifecycle status.

## Design boundaries (v1)

Goodman v1 **detects and alerts**; it does not block or kill processes
(enforcement is a v2 concern). Attribution uses **Tier 1** (perf-map JIT
resolution), which needs the target Node process started with `--perf-basic-prof-only-functions`
— a launch flag, not a code change. **Tier 2** (in-kernel V8 stack unwinding)
removes even that flag and is the future zero-config upgrade; the perf-map path
remains a permanent fallback. See [attribution.md](attribution.md) for both tiers.
