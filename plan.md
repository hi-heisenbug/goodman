# Goodman — End-to-End Build Plan

> **Product:** Goodman, a runtime security sensor that attributes each kernel syscall to the specific npm/PyPI package that caused it, builds a behavioral fingerprint per (package, version, service), and alerts when a dependency's behavior drifts from its baseline.
>
> **Audience of this document:** an engineer *or a coding agent* building the product from zero. Every phase has exact files, commands, code skeletons, and a **Definition of Done (DoD)**. Build phases **strictly in order**. Do not start a phase until the previous phase's DoD passes.

---

## 0. Read this first — the mental model

A running Node.js process makes syscalls (open a file, connect to a host, spawn a process). The kernel knows the **process** made the syscall. It does **not** know **which of the 79 npm packages** inside that process made it. Goodman closes that gap in four steps:

1. **Capture** — an eBPF program in the kernel intercepts security-relevant syscalls and grabs the *user-space call stack* at the moment of the syscall.
2. **Attribute** — in user space, we resolve that stack's addresses into JavaScript source file paths, find the deepest frame inside a `node_modules/<package>/` directory, and read that package's version from its `package.json`. That (package, version) is the culprit.
3. **Fingerprint** — we record, per `(service, package, version)`, the *set* of behaviors observed: files read/written, network destinations, processes spawned, syscalls used.
4. **Diff** — when a package's version changes (or new behavior appears), we compare the new behavior set against the established baseline. Anything **new** is a drift alert.

**The single hardest task in the whole product is step 2 (attribution).** This plan de-risks it with a two-tier strategy (see §5). Build Tier 1 first. It is enough to ship.

**Honesty note on "zero code changes":** Tier 1 attribution needs Node started with a profiling flag (`--perf-basic-prof`). This is *not* a code change to the app, but it *is* a launch-flag change, injected at the container level by our Helm chart (see §11). Tier 2 removes even that. Do not claim "literally zero config" until Tier 2 works.

---

## 1. Scope and non-goals for v1

**In scope (v1):**
- Linux, x86-64, kernel ≥ 5.8 (needs `bpf_get_stack`, ring buffer, CO-RE).
- Node.js workloads (npm packages). Python/PyPI is a fast-follow, architected for but not built in v1.
- Syscall classes: file (`openat`), network (`connect`), process (`execve`).
- Kubernetes deployment via Helm (DaemonSet).
- A collector service, a datastore, a diff engine, an alerts API, and a minimal dashboard.

**Explicit non-goals (v1):**
- ARM64, macOS, Windows.
- Blocking/enforcement (we *detect and alert*, we do not kill processes in v1 — the "Quarantine" button in the deck is a v2 action).
- Full inline-frame reconstruction.
- Multi-cluster fingerprint network (that is the Series-A "network effects" milestone; architect the schema for it, do not build it).

---

## 2. Tech stack (do not substitute without reason)

| Layer | Choice | Why |
|---|---|---|
| eBPF program | **C**, CO-RE (`vmlinux.h` + libbpf) | One binary runs on all kernels; matches the deck's "eBPF CO-RE" claim. |
| eBPF loader / sensor | **Go 1.22+** with `github.com/cilium/ebpf` | Best-documented production eBPF loader; single static binary; easy containerization. |
| Native symbol resolution | `github.com/cilium/ebpf` + parsing `/proc/<pid>/maps` + Go `debug/elf` | Resolve non-JS frames. |
| JIT (JS) symbol resolution | Parse `/tmp/perf-<pid>.map` (Tier 1) | Node writes JS function name + source path here. This is the attribution shortcut. |
| Collector + API | **Go** (same repo, separate binary) — `net/http` + `chi` router | One language for whole backend. |
| Datastore | **PostgreSQL 15** (behavior sets as JSONB) | Durable, queryable, good enough to $1M ARR. Use SQLite only for the local dev harness. |
| Live behavior cache | Postgres tables (v1). Redis is a v2 optimization, do not add yet. | Keep infra minimal. |
| Dashboard | **React + Vite + TypeScript**, Tailwind, Recharts | Renders the alert UI from the deck. |
| Packaging | **Helm** chart; Docker multi-stage builds | "One helm command" is the promise. |
| Local test cluster | **kind** (Kubernetes-in-Docker) with a real Linux node | eBPF needs a real kernel; kind on a Linux host works, Docker Desktop on macOS does NOT (see §11.4). |

**Prerequisites to install before Phase 1:**
```bash
# On an x86-64 Linux host / VM (Ubuntu 22.04+ recommended). NOT macOS.
sudo apt-get update
sudo apt-get install -y clang llvm libelf-dev libbpf-dev linux-headers-$(uname -r) \
                        build-essential pkg-config bpftool git curl
# Go
curl -LO https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz && export PATH=$PATH:/usr/local/go/bin
# Node (for test workloads)
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash - && sudo apt-get install -y nodejs
# kind + kubectl + helm
go install sigs.k8s.io/kind@latest
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && sudo install kubectl /usr/local/bin/
curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
# Verify eBPF support
grep -E "CONFIG_BPF=y|CONFIG_BPF_SYSCALL=y|CONFIG_BPF_JIT=y" /boot/config-$(uname -r)
```
**DoD for prerequisites:** all four grep lines print `=y`; `go version`, `node --version`, `clang --version`, `bpftool version`, `kind version`, `helm version` all succeed.

---

## 3. Repository structure

Create this exact layout in Phase 0. Every later phase fills in files here.

```
goodman/
├── README.md
├── plan.md                      # this file
├── Makefile                     # top-level build/test targets
├── go.mod
├── go.sum
│
├── bpf/                         # eBPF C source (kernel side)
│   ├── goodman.bpf.c            # the eBPF program
│   ├── goodman.h                # shared structs (event layout) — used by C AND Go
│   └── vmlinux.h                # generated: bpftool btf dump ... (do not hand-edit)
│
├── cmd/
│   ├── sensor/main.go           # the eBPF loader + attributor (runs as DaemonSet)
│   ├── collector/main.go        # receives events, runs diff engine, serves API
│   └── goodmanctl/main.go       # dev CLI: tail alerts, dump fingerprints
│
├── internal/
│   ├── loader/                  # cilium/ebpf load + attach + ringbuf reader
│   │   └── loader.go
│   ├── attribute/               # THE HARD PART — stack -> package
│   │   ├── maps.go              # parse /proc/<pid>/maps
│   │   ├── perfmap.go           # parse /tmp/perf-<pid>.map  (Tier 1 JIT resolver)
│   │   ├── resolve.go           # address -> symbol -> file path
│   │   └── package.go           # file path -> (package, version)
│   ├── model/                   # shared data types (Event, Behavior, Fingerprint, Alert)
│   │   └── types.go
│   ├── store/                   # Postgres access
│   │   ├── store.go
│   │   └── migrations/*.sql
│   ├── fingerprint/             # aggregate events into behavior sets
│   │   └── fingerprint.go
│   ├── diff/                    # baseline vs current -> drift
│   │   └── diff.go
│   └── api/                     # HTTP handlers for the dashboard
│       └── api.go
│
├── dashboard/                   # React + Vite frontend
│   ├── package.json
│   ├── index.html
│   └── src/...
│
├── deploy/
│   ├── docker/
│   │   ├── sensor.Dockerfile
│   │   └── collector.Dockerfile
│   └── helm/goodman/            # the Helm chart
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
│           ├── daemonset.yaml   # sensor
│           ├── deployment.yaml  # collector + api
│           ├── configmap.yaml
│           └── rbac.yaml
│
└── test/
    ├── workload/                # a benign Node app used as the "victim service"
    │   ├── package.json
    │   └── server.js
    ├── fixtures/                # simulated "malicious" package versions (benign, for testing)
    │   ├── good-pkg-1.0.0/
    │   └── good-pkg-1.0.1/      # same pkg, adds a NEW benign network call = drift
    └── e2e/                     # scripts that assert alerts fire
        └── drift_test.sh
```

**Phase 0 DoD:** the tree exists, `go mod init github.com/<you>/goodman` done, `git init` done, `make help` prints available targets, empty `README.md` describes the product in 3 sentences.

---

## 4. Shared data model (build this before anything moves data)

Create `internal/model/types.go` and `bpf/goodman.h` together — the C struct and Go struct **must** have identical memory layout (same field order, sizes, alignment).

### 4.1 `bpf/goodman.h` (shared kernel/user struct)
```c
#ifndef GOODMAN_H
#define GOODMAN_H

#define TASK_COMM_LEN   16
#define MAX_STACK_DEPTH 32
#define PATH_MAX_LEN    256

enum event_type {
    EVENT_FILE_OPEN = 1,
    EVENT_NET_CONNECT = 2,
    EVENT_PROC_EXEC = 3,
};

struct event {
    __u32 pid;                       // process id (tgid)
    __u32 tid;                       // thread id
    __u8  type;                      // enum event_type
    char  comm[TASK_COMM_LEN];       // process name (e.g. "node")
    char  arg[PATH_MAX_LEN];         // file path, or "ip:port", or exec path
    __u64 stack[MAX_STACK_DEPTH];    // user-space instruction pointers (frame-pointer walk)
    __u32 stack_len;                 // number of valid entries in stack[]
    __u64 timestamp_ns;
};

#endif
```

### 4.2 `internal/model/types.go`
```go
package model

type EventType uint8
const (
    EventFileOpen   EventType = 1
    EventNetConnect EventType = 2
    EventProcExec   EventType = 3
)

// RawEvent mirrors struct event in bpf/goodman.h byte-for-byte.
type RawEvent struct {
    PID       uint32
    TID       uint32
    Type      uint8
    Comm      [16]byte
    Arg       [256]byte
    Stack     [32]uint64
    StackLen  uint32
    Timestamp uint64
}

// Attributed is a RawEvent after we've figured out which package caused it.
type Attributed struct {
    Service   string    // k8s service / pod name
    Package   string    // e.g. "@tanstack/react-router"
    Version   string    // e.g. "1.120.17"
    Type      EventType
    Behavior  string    // canonicalized: "READ /app/src/**" or "CONNECT 1.2.3.4:443"
    Timestamp uint64
}

// Fingerprint = the set of behaviors seen for one (service,package,version).
type Fingerprint struct {
    Service   string
    Package   string
    Version   string
    Behaviors map[string]BehaviorStat // key = Behavior string
    FirstSeen uint64
    LastSeen  uint64
    ObsCount  int
}
type BehaviorStat struct{ Count int; FirstSeen, LastSeen uint64 }

// Alert is emitted by the diff engine.
type Alert struct {
    ID          string
    Service     string
    Package     string
    OldVersion  string
    NewVersion  string
    Severity    string      // INFO | WARN | CRITICAL
    NewBehaviors []string   // behaviors present now but NOT in baseline
    DetectedAt  uint64
}
```

**§4 DoD:** `go build ./internal/model/...` compiles; a unit test round-trips a `RawEvent` through `encoding/binary` and back with no field drift.

---

## 5. THE HARD PART — attribution (§ read fully before coding)

This is where the product lives or dies. We ship **Tier 1** and treat **Tier 2** as an upgrade. Do not attempt Tier 2 until Tier 1 works end-to-end.

### 5.1 How attribution works
When the eBPF program fires on a syscall, it calls `bpf_get_stack(ctx, &stack, sizeof(stack), BPF_F_USER_STACK)`. This walks the user-space stack **using frame pointers** and returns an array of instruction-pointer addresses. This works for Node because Chromium/Node have shipped frame pointers on by default since 2022 (the deck's "why now" precondition).

We then, in user space, turn each address into a source location:
- **Native addresses** (inside `node`, `libc`, `.node` addons): resolve via `/proc/<pid>/maps` (which module + offset) then the module's ELF symbol table.
- **JIT addresses** (V8-compiled JS): resolve via `/tmp/perf-<pid>.map`.

### 5.2 Tier 1 — perf-map JIT resolution (BUILD THIS)
When Node runs with `--perf-basic-prof --interpreted-frames-native-stack`, V8 continuously writes `/tmp/perf-<pid>.map`. Each line is:
```
<hex_start_addr> <hex_size> <symbol>
```
For JS functions the symbol embeds the **source file path**, e.g.:
```
3ca9f8c04a20 1e0 LazyCompile:*handleRequest /app/node_modules/@tanstack/react-router/dist/esm/router.js:412:19
```
Algorithm (`internal/attribute/`):
1. For a given pid, load `/tmp/perf-<pid>.map` into a sorted interval list `[start, start+size) -> symbol`. Cache it; refresh if the file's mtime changed.
2. For each address in `event.stack`, binary-search the interval list. If found → extract the source file path with a regex like `\s(/\S+\.[cm]?js):\d+`.
3. Walk stack frames **outermost app frame → inward**. The **deepest frame whose path contains `/node_modules/`** is the attributed package. (If several node_modules frames are nested, the deepest is the actual actor; the ones above may just be the caller.)
4. If **no** frame is in node_modules, attribute to the application itself (`Package = "<app>"`). That is still useful signal.
5. Map path → package (see 5.4).

### 5.3 Tier 2 — in-kernel V8 unwinding (UPGRADE, LATER)
Removes the `--perf-basic-prof` flag requirement (true zero-config). Inside eBPF, chase V8's object pointers from the stack frame: `JSFunction → SharedFunctionInfo → ScopeInfo → function name`. A working reference implementation to adapt is `ustackjs` (kvakil). This is genuinely hard (V8 layout changes across versions, string type handling, GC races). **Do not start until Tier 1 is in a paying customer's cluster.** Keep the perf-map path as a permanent fallback.

### 5.4 File path → (package, version) — `internal/attribute/package.go`
```go
// PathToPackage turns "/app/node_modules/@scope/name/dist/x.js"
// into ("@scope/name", "1.2.3"). Handles nested + scoped packages.
func PathToPackage(pidRoot, path string) (pkg, version string, ok bool) {
    idx := strings.LastIndex(path, "/node_modules/")   // LAST = deepest/actual
    if idx == -1 { return "", "", false }
    rest := path[idx+len("/node_modules/"):]
    parts := strings.SplitN(rest, "/", 3)
    if len(parts) == 0 { return "", "", false }
    if strings.HasPrefix(parts[0], "@") && len(parts) >= 2 {
        pkg = parts[0] + "/" + parts[1]                 // scoped: @scope/name
    } else {
        pkg = parts[0]
    }
    // Read version from that package's package.json.
    // pidRoot handles container filesystems: /proc/<pid>/root + path.
    pkgJSON := filepath.Join(pidRoot, path[:idx], "node_modules", pkg, "package.json")
    version = readVersionField(pkgJSON)                 // json: .version
    return pkg, version, version != ""
}
```
**Container note:** inside k8s, the sensor runs on the host but the target's files live under `/proc/<pid>/root/...`. Always resolve package.json through `/proc/<pid>/root`. Cache `(pid,path)->(pkg,version)` — package.json does not change while a pid lives.

### 5.5 Behavior canonicalization — `internal/attribute/resolve.go`
Raw args are noisy (unique temp files, ephemeral ports). Canonicalize so the *same behavior* maps to the *same string*:
- File: collapse variable path segments. `/app/src/routes/user-42.js` → `READ /app/src/routes/**`. Keep sensitive paths **verbatim** (never collapse): `/var/run/secrets/**`, `~/.aws/credentials`, `/etc/shadow`, anything containing `token`, `secret`, `credential`, `.pem`, `.key`.
- Network: `CONNECT <ip>:<port>`; if you can resolve the pod's DNS cache, prefer `CONNECT <domain>:<port>`. Always keep link-local `169.254.169.254` (cloud metadata) verbatim and flag it.
- Exec: `EXEC <basename>` plus full argv hash.

**§5 DoD (Tier 1):** given a running Node app started with `--perf-basic-prof`, `goodmanctl attribute --pid <pid>` prints, for a deliberately triggered `fetch()` from inside a node_modules package, the correct `(package, version, "CONNECT host:443")`. Accuracy target: ≥ 80% of syscalls from node_modules code attributed to the right package on the test workload. Unattributed is acceptable; **mis**attribution is not — prefer "unknown" over a wrong package.

---

## 6. Phase 1 — eBPF capture (Roadmap Q1: "Build & Ship", CPU overhead < 2%)

### 6.1 Generate `vmlinux.h`
```bash
bpftool btf dump file /sys/kernel/btf/vmlinux format c > bpf/vmlinux.h
```

### 6.2 Write `bpf/goodman.bpf.c`
Hook three tracepoints. Skeleton for the file-open hook (repeat pattern for `connect`, `execve`):
```c
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include "goodman.h"

struct { __uint(type, BPF_MAP_TYPE_RINGBUF); __uint(max_entries, 1 << 24); } events SEC(".maps");

// Only trace processes we care about. Populated from user space (pid -> 1).
struct { __uint(type, BPF_MAP_TYPE_HASH); __type(key, __u32); __type(value, __u8);
         __uint(max_entries, 4096); } watched_pids SEC(".maps");

SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat(struct trace_event_raw_sys_enter *ctx) {
    __u32 tgid = bpf_get_current_pid_tgid() >> 32;
    if (!bpf_map_lookup_elem(&watched_pids, &tgid)) return 0;   // ignore everything else

    struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    e->pid = tgid;
    e->tid = (__u32)bpf_get_current_pid_tgid();
    e->type = EVENT_FILE_OPEN;
    e->timestamp_ns = bpf_ktime_get_ns();
    bpf_get_current_comm(&e->comm, sizeof(e->comm));
    const char *path = (const char *)ctx->args[1];             // openat: 2nd arg = filename
    bpf_probe_read_user_str(&e->arg, sizeof(e->arg), path);
    // THE KEY LINE: capture the user stack via frame pointers.
    long n = bpf_get_stack(ctx, e->stack, sizeof(e->stack), BPF_F_USER_STACK);
    e->stack_len = n > 0 ? n / sizeof(__u64) : 0;
    bpf_ringbuf_submit(e, 0);
    return 0;
}
char LICENSE[] SEC("license") = "GPL";
```
For `connect`, read the `sockaddr` from `ctx->args[1]` and parse `sin_addr`/`sin_port`. For `execve`, read `ctx->args[0]` (filename).

**Filtering for < 2% overhead:** only trace **watched pids** (Node/Python processes we've identified), and only the three syscall classes above. Do NOT trace `read`/`write` (far too high volume) in v1 — file *open* already tells us what the package touches. This is how you hit the CPU budget.

### 6.3 Build the eBPF object
```bash
clang -O2 -g -target bpf -D__TARGET_ARCH_x86 -I bpf -c bpf/goodman.bpf.c -o bpf/goodman.bpf.o
```

### 6.4 Load & attach — `internal/loader/loader.go` (cilium/ebpf)
- Use `bpf2go` to generate Go bindings from the `.o`, or load the `.o` at runtime with `ebpf.LoadCollectionSpec`.
- Attach each tracepoint with `link.Tracepoint(...)`.
- Read the ring buffer with `ringbuf.NewReader`; unmarshal each record into `model.RawEvent` with `binary.Read` (little-endian).
- Populate `watched_pids` by scanning `/proc` for processes whose `comm` is `node`/`python3` (and, in k8s, that belong to labeled pods). Refresh every few seconds.

**§6 DoD:**
1. `make bpf && go run ./cmd/sensor` on a host, start a Node app that opens a file / makes a connect / spawns a process → the sensor prints one `RawEvent` per action with a non-empty `stack`.
2. `pidstat`/`top` shows the target Node process CPU overhead from tracing is **< 2%** under a load test (e.g. `autocannon` hammering the test workload). Record the number in `README.md`. **This is the headline roadmap deliverable — measure it and write it down.**

---

## 7. Phase 2 — wire attribution into the sensor

- In `cmd/sensor`, for each `RawEvent`: run §5 attribution → produce `model.Attributed`.
- Determine `Service`: from the pid's cgroup path (k8s puts pod UID/container in the cgroup) or an env label; for local dev, use the process's working directory basename.
- Batch `Attributed` events and POST them to the collector (`/v1/events`) every 1–2s (gzip JSON). Never block the ring-buffer reader on network — hand off via a buffered channel; drop with a counter if the channel is full (and expose that drop counter).

**§7 DoD:** `goodmanctl tail` (which reads from the collector) shows a live stream of `service | package@version | behavior` lines while the test workload runs. At least 80% of node_modules-originated events carry a correct package (per §5 DoD).

---

## 8. Phase 3 — store + fingerprint engine

### 8.1 Postgres schema — `internal/store/migrations/001_init.sql`
```sql
CREATE TABLE fingerprints (
    id           BIGSERIAL PRIMARY KEY,
    service      TEXT NOT NULL,
    package      TEXT NOT NULL,
    version      TEXT NOT NULL,
    behaviors    JSONB NOT NULL DEFAULT '{}',  -- { "READ /app/**": {"count":N,"first":ts,"last":ts}, ... }
    first_seen   BIGINT NOT NULL,
    last_seen    BIGINT NOT NULL,
    obs_count    INT NOT NULL DEFAULT 0,
    is_baseline  BOOLEAN NOT NULL DEFAULT FALSE, -- promoted after the learning window
    UNIQUE (service, package, version)
);
CREATE INDEX idx_fp_pkg ON fingerprints (package, version);

CREATE TABLE alerts (
    id            TEXT PRIMARY KEY,
    service       TEXT NOT NULL,
    package       TEXT NOT NULL,
    old_version   TEXT,
    new_version   TEXT NOT NULL,
    severity      TEXT NOT NULL,
    new_behaviors JSONB NOT NULL,
    detected_at   BIGINT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'open'   -- open | acknowledged | resolved
);
```

### 8.2 Fingerprint aggregation — `internal/fingerprint/fingerprint.go`
On each incoming `Attributed` event: `UPSERT` into `fingerprints`, merging the behavior into the JSONB set (increment count, update last_seen, set first_seen if new). Increment `obs_count`.

### 8.3 Baseline promotion
A fingerprint becomes a **baseline** (`is_baseline = true`) when it has been observed for a **learning window**: default `obs_count ≥ 500` **AND** `last_seen - first_seen ≥ 24h` (configurable per §11 values). Rationale: enough behavioral coverage + enough wall-clock to see periodic jobs. Until then, do not alert on that (package,version) — it's still learning.

**§8 DoD:** after running the test workload for the (shortened, for dev) learning window, `SELECT * FROM fingerprints WHERE is_baseline` returns the workload's packages with a sane behavior set (e.g. `express` shows file reads under its own dir + a listen, no outbound connects).

---

## 9. Phase 4 — diff engine (the alert)

### 9.1 Logic — `internal/diff/diff.go`
Trigger on either event:
- **New version appears** for a (service, package): compare the *new version's* accumulating behavior set against the *previous version's baseline*.
- **New behavior on the same version** after it was promoted to baseline: compare live behavior against its own baseline.

```
newBehaviors = currentBehaviorSet - baselineBehaviorSet     // set difference
if newBehaviors is empty: no alert
severity = CRITICAL if any newBehavior matches a HIGH_RISK rule else WARN
```

### 9.2 High-risk rules (make CRITICAL)
Any new behavior that is:
- a READ of a secret path (`/var/run/secrets/**`, `~/.aws/credentials`, `**/*.pem`, `**token**`),
- a CONNECT to cloud metadata (`169.254.169.254`),
- a CONNECT to a **new external domain/IP** never in baseline,
- an EXEC of a new process.

These four are exactly the TanStack alert in the deck. Encode them as a small, config-driven rule list, not hard-coded `if`s, so customers can tune them.

### 9.3 Alert emission
Write to `alerts` table; expose over the API; (v2: webhook to Slack). The alert payload must reproduce the deck's format: baseline summary, drift list, old/new version, service, severity, timestamp.

**§9 DoD:** run the drift scenario (§10) → exactly one CRITICAL alert row is created, whose `new_behaviors` contains the metadata-IP connect and the secret read, and does NOT contain any behavior that was already in baseline. No alert fires for a version bump that changes nothing behaviorally (guard against false positives — this is what makes or breaks trust).

---

## 10. Phase 5 — test harness that reproduces a supply-chain attack (safely)

The deck's credibility rests on replaying the May-2026 attacks. **Build a *benign* simulation** — a test package whose new version performs behaviors that *look* like the attack pattern but do no harm (reads a fake dummy credentials file you created, connects to a **local mock** metadata server, POSTs to a local sink). Never write or run real malware, real exfiltration, or real C2. The point is to trigger the *behavioral drift*, not to attack anything.

### 10.1 Two versions of a test package — `test/fixtures/`
- `good-pkg-1.0.0/index.js`: on `require()`, reads only files under its own dir. Establish baseline.
- `good-pkg-1.0.1/index.js`: identical API, but on `require()` **also** reads `./FAKE_credentials_do_not_use.txt` (a dummy file you ship in the fixture) and does `fetch("http://127.0.0.1:9999/collect")` (a local sink you run). This is the benign stand-in for the node-ipc "fires at require()" pattern.

### 10.2 Test workload — `test/workload/server.js`
An Express app that `require`s `good-pkg`. Start it with `node --perf-basic-prof --interpreted-frames-native-stack server.js`.

### 10.3 E2E script — `test/e2e/drift_test.sh`
1. Deploy sensor + collector locally (or in kind).
2. Run workload with `good-pkg@1.0.0`, drive traffic, wait for baseline promotion (use a dev-shortened window).
3. Swap to `good-pkg@1.0.1`, restart workload.
4. Poll `/v1/alerts` and **assert** a CRITICAL alert appears within N seconds naming `good-pkg`, version `1.0.0 → 1.0.1`, with the new secret-read + local-sink connect.
5. Exit non-zero if no alert or a misattributed alert.

**§10 DoD:** `make e2e` runs the whole thing headless and exits 0. This is your demo and your regression test. Put it in CI.

---

## 11. Phase 6 — Kubernetes packaging (the "one helm command" promise)

### 11.1 Dockerfiles
- `sensor.Dockerfile`: multi-stage; build the Go binary + copy `bpf/goodman.bpf.o`; final image needs the object and CA certs. Runs privileged.
- `collector.Dockerfile`: plain Go binary + migrations.

### 11.2 DaemonSet — `deploy/helm/goodman/templates/daemonset.yaml`
The sensor must run on **every node**, with:
```yaml
spec:
  hostPID: true
  containers:
  - name: sensor
    securityContext:
      privileged: true                 # simplest; v2: drop to caps below
      # capabilities: { add: ["SYS_ADMIN","BPF","PERFMON","SYS_PTRACE"] }
    volumeMounts:
    - { name: sys,  mountPath: /sys,  readOnly: true }
    - { name: proc, mountPath: /host/proc }
    - { name: tmp,  mountPath: /host/tmp }   # to read /tmp/perf-<pid>.map of targets
  volumes:
  - { name: sys,  hostPath: { path: /sys } }
  - { name: proc, hostPath: { path: /proc } }
  - { name: tmp,  hostPath: { path: /tmp } }
```
The sensor resolves target files under `/host/proc/<pid>/root/...` and perf maps under `/host/tmp/perf-<pid>.map` (targets and sensor share the host `/tmp` when using `--perf-basic-prof`, which writes to the process's `/tmp`; in k8s you may need the target pod to share `/tmp` or use the host-pid view `/host/proc/<pid>/root/tmp/`). **Prefer resolving perf maps via `/host/proc/<pid>/root/tmp/perf-<pid>.map`** — that always sees the target's own `/tmp` regardless of mounts.

### 11.3 Injecting `--perf-basic-prof` (Tier 1 requirement)
Two options, document both in `values.yaml`:
- **Manual (v1 default):** instruct the customer to add `NODE_OPTIONS=--perf-basic-prof --interpreted-frames-native-stack` (or the node arg) to their app deployment. One line, no code change.
- **Automatic (v1.1):** ship a `MutatingAdmissionWebhook` that injects the env var into pods matching a label. Architect for it; build after first pilot.
- **Tier 2:** removes this entirely.

### 11.4 The install command (must match the deck)
```bash
helm install goodman goodman/goodman \
  --set cluster=prod \
  --set registries=npm,pypi
```
`values.yaml` keys: `cluster`, `registries`, `collector.image`, `sensor.image`, `learningWindow.obsCount`, `learningWindow.minAgeHours`, `postgres.dsn`, `attribution.tier` (`perfmap`|`v8native`).

### 11.5 Local cluster
Use `kind` **on a Linux host** (eBPF needs the real kernel; kind nodes share the host kernel — good). **Docker Desktop on macOS runs a LinuxKit VM without the headers/BTF we need — do not develop there.** If the only machine is a Mac, provision a Linux VM (multipass/UTM/cloud) and work there.

**§11 DoD:** on a kind cluster on Linux, `helm install goodman ./deploy/helm/goodman --set cluster=dev` brings up the DaemonSet + collector; deploying the §10 workload + swapping to the drift version produces a CRITICAL alert visible via `kubectl port-forward` to the dashboard. Uninstall is clean (`helm uninstall`).

---

## 12. Phase 7 — API + dashboard

### 12.1 API — `internal/api/api.go`
- `POST /v1/events` — sensor ingestion (batched, gzip).
- `GET  /v1/alerts?status=open` — list alerts.
- `POST /v1/alerts/{id}/ack` — acknowledge.
- `GET  /v1/fingerprints?service=&package=` — inspect baselines.
- `GET  /v1/healthz` — liveness.

### 12.2 Dashboard
Two screens are enough for v1:
1. **Alerts feed** — cards rendering the exact deck format: package@version, service, severity badge, "BASELINE (Ndays)" vs "DRIFT (last Nm)" behavior diff, and action buttons (`Rollback`/`Investigate` — in v1 these link out / copy a `kubectl` command; real Quarantine is v2).
2. **Fingerprint explorer** — search a package, see its learned behavior set per version.

Consult `/mnt/skills/public/frontend-design/SKILL.md` before styling so it looks intentional, not templated.

**§12 DoD:** dashboard renders the live CRITICAL alert from the §10 scenario, matching the deck's layout closely enough to demo to a customer.

---

## 13. Phase 8 — Python/PyPI fast-follow (architect now, build after first revenue)

Same pipeline, different resolvers:
- eBPF capture is **identical** (syscalls are language-agnostic).
- Attribution: CPython frames resolve differently — read `PyFrameObject` from memory (the OTel eBPF profiler does exactly this) or use a py-spy-style approach; map source file → `site-packages/<dist>/` → distribution name + version from `*.dist-info/METADATA`.
- Everything downstream (fingerprint, diff, alerts, dashboard) is unchanged because it operates on the abstract `Attributed` type.

Do not build until §9's DoD is solid and you have a paying Node customer.

---

## 14. Cross-cutting requirements (apply throughout)

- **Never misattribute.** Unknown/unattributed is fine and honest; a wrong package name destroys trust. Every resolver returns `ok bool`.
- **Overhead budget is a feature.** Re-measure the < 2% number after Phases 2, 6, 11. If it regresses, cut event volume (tighter syscall set, in-kernel dedup with a per-(pid,behavior) seen-map so identical behaviors aren't re-sent every time).
- **Privacy/data handling (SOC 2 groundwork, per roadmap Q2/Q3):** behavior strings can contain paths → treat as sensitive. Support on-prem/self-hosted collector so customer data never leaves their cluster (this is also a *sales* advantage for the "cross-tenant fingerprint network" — share only *hashed, path-stripped* behavior signatures, never raw paths). Log access; encrypt Postgres at rest; document a data-flow diagram. Start the SOC 2 Type I control list in Q2 as the roadmap says.
- **Kernel-version resilience:** CO-RE handles most struct differences, but test on ≥ 3 kernels (5.10, 5.15, 6.x) in CI using VMs. Fail gracefully with a clear message if BTF is missing.
- **Observability of the sensor itself:** expose Prometheus metrics — events/sec, attribution success rate, ringbuf drops, alert count, CPU self-usage.
- **Config over code:** learning window, high-risk rules, watched runtimes, and severity mapping all live in `values.yaml`/ConfigMap.

---

## 15. Build order checklist (the linear path)

```
[ ] 0.  Prereqs installed + verified (§2)
[ ] 0.  Repo skeleton created (§3)
[ ] 1.  Shared model: goodman.h + types.go round-trip test (§4)
[ ] 2.  eBPF capture: 3 tracepoints, stacks captured, <2% overhead measured (§6)   <-- roadmap headline
[ ] 3.  Attribution Tier 1: stack -> package@version, >=80% accuracy (§5, §7)
[ ] 4.  Store + fingerprint aggregation + baseline promotion (§8)
[ ] 5.  Diff engine + high-risk rules + alert emission (§9)
[ ] 6.  Benign attack-replay e2e test, `make e2e` green in CI (§10)               <-- your demo
[ ] 7.  Docker + Helm, install on kind, alert visible in-cluster (§11)            <-- "one helm command"
[ ] 8.  API + dashboard rendering the deck's alert (§12)
[ ] 9.  RE-MEASURE overhead; wire Prometheus metrics; SOC2 control list started (§14)
--- ship first free pilot here (roadmap: June end) ---
[ ] 10. MutatingWebhook auto-injection (§11.3)
[ ] 11. Python/PyPI resolver (§13)
[ ] 12. Tier 2 in-kernel V8 unwinding — true zero-config (§5.3)
--- these three unlock the "zero config" + broader-market story for Series A ---
```

## 16. How this maps to the pitch roadmap

- **Q1 "Build & Ship" (eBPF capture, <2% overhead, first demo, first free pilot):** checklist items 2–7. The e2e replay (item 6) *is* the "documented + replicated May-2026 attacks" deliverable from the traction slide.
- **Q2 "First Revenue" (V8 attribution PoC, diff alerts in a paying cluster, SOC 2 Type I):** item 3 is the attribution PoC; item 9 starts SOC 2; item 5 puts diff alerts live.
- **Q3 "Scale" (forensic content, repeatable sales):** the fingerprint explorer (item 8) generates the postmortem data your content strategy publishes.
- **Q4 "Network effects / Series A" (cross-tenant fingerprint dataset):** enabled by the hashed, path-stripped signature sharing designed in §14 — but only turn it on once self-hosted privacy controls are proven.

---

*Build Tier 1 attribution and the benign replay test first. Everything else is plumbing around those two.*
