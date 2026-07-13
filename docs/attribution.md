# Attribution — the hard part

Attribution is turning a raw kernel event (a pid and a stack of instruction-pointer
addresses) into a precise answer: **which `package@version` caused this syscall,
and what did it do?** This is where Goodman lives or dies. This document explains
the algorithm, the two tiers, and the guarantees.

All the code lives in `internal/attribute/`.

## Why npm and PyPI (not every ecosystem yet)

Other package ecosystems matter — RubyGems, Maven/Gradle, Go modules, Cargo,
NuGet, Composer, system packages, and so on. Goodman focuses on **npm and PyPI
first** on purpose; that is a product wedge, not a claim that only those
registries exist.

1. **Where the attacks land.** High-profile runtime supply-chain incidents
   (malicious postinstalls, dependency hijacks, typosquats) have concentrated on
   Node/npm and Python/PyPI. That is the market the first wedge sells into.
2. **Attribution is per-runtime.** The kernel only sees syscalls plus a user
   stack. Naming `package@version` needs a language-specific path: V8 perf maps
   → `node_modules` → `package.json` for npm; CPython `PYTHONPERFSUPPORT` /
   `py::` symbols → `site-packages` → `.dist-info` for PyPI. There is no free
   “support all registries” switch.
3. **Correctness over breadth.** A wrong package name destroys trust (see
   [never misattribute](#the-guarantee-never-misattribute)). Shipping two solid
   Tier-1 stories beats a half-working soup of ecosystems.

**Later expansion** looks the same for each new ecosystem: watch the right
process names → resolve frames to source paths → map path to package+version →
reuse the existing baseline / drift / alert pipeline. Add a registry when
customers pull for it and a credible Tier-1 resolution path exists.

## The problem

When the eBPF program fires, it calls:

```c
bpf_get_stack(ctx, e->stack, sizeof(e->stack), BPF_F_USER_STACK);
```

This walks the user-space stack using **frame pointers** and returns an array of
raw instruction-pointer addresses — e.g. `[0x3ca9f8c04a30, 0x00400123, ...]`. On
their own these addresses mean nothing. Goodman has to turn each one back into a
source location.

There are two kinds of frame:

- **Native frames** — inside `node`, `libc`, or a `.node` addon. Resolved via
  `/proc/<pid>/maps` (which module + offset) and the module's ELF symbols.
  (`maps.go`)
- **JIT frames** — V8-compiled JavaScript. These live in anonymous, executable
  memory that no ELF file backs. Resolved via V8's perf map. (`perfmap.go`)

## Tier 1 — perf-map JIT resolution (what ships in v1)

When Node runs with `--perf-basic-prof --interpreted-frames-native-stack`, V8
continuously appends to `/tmp/perf-<pid>.map`. Each line is:

```
<hex_start_addr> <hex_size> <symbol>
```

and for JavaScript functions the symbol embeds the **source file path**:

```
3ca9f8c04a20 1e0 LazyCompile:*handleRequest /app/node_modules/@tanstack/react-router/dist/esm/router.js:412:19
```

The algorithm (`resolve.go` + `perfmap.go`):

1. Load `/tmp/perf-<pid>.map` into a sorted interval list `[start, start+size) →
   symbol`, cached and refreshed when the file's mtime or size changes (V8 appends
   as it JITs more code).
2. For each address in the event's stack, binary-search the interval list. If a
   JIT symbol is found, extract the source path with a regex
   (`\s(/\S+\.[cm]?js)(?::\d+)?(?::\d+)?$`).
3. Walk the frames from **innermost outward** and take the **deepest frame whose
   source path contains `/node_modules/`** — that is the actual actor. (Frames
   above it may just be the caller passing through.)
4. If no frame is inside `node_modules`, attribute to the application itself
   (`Package = "<app>"`, version from the app's own `package.json`).
5. If nothing resolves at all, attribute to `<unknown>`.

Then the source path is mapped to a package (`package.go`).

### Path → (package, version)

```go
PathToPackage("/proc/<pid>/root", "/app/node_modules/@scope/name/dist/x.js")
  → ("@scope/name", "1.2.3", true)
```

- The **last** `/node_modules/` segment wins, so nested dependencies
  (`a/node_modules/b`) attribute to the deepest one (`b`).
- Scoped packages (`@scope/name`) are handled.
- The version is read from that package's `package.json`, resolved through
  **`/proc/<pid>/root`** so it works inside container filesystems.
- Results are cached per package.json path — it doesn't change while a pid lives.

## Tier 2 — in-kernel V8 unwinding (future, not in v1)

Tier 2 removes the `--perf-basic-prof` requirement (true zero-config) by chasing
V8's object pointers inside eBPF: `JSFunction → SharedFunctionInfo → ScopeInfo →
function name`. It is genuinely hard (V8 layout changes across versions, string
type handling, GC races). It is **architected for but not built** in v1; the
perf-map path stays as a permanent fallback. See `plan.md` §5.3.

**Research status (2026-07):** a timeboxed spike parked Tier-2 as year-scale
work — see [`docs/research/tier2-attribution.md`](research/tier2-attribution.md).
The NODE_OPTIONS webhook remains the shipping answer to zero-config objections.
Do not start a production build until that doc's GO criteria are met.

## Python / PyPI — Tier 1 (perf trampoline)

CPython **3.12+** with `PYTHONPERFSUPPORT=1` appends to the same
`/tmp/perf-<pid>.map` format as Node (`perfmap.go`). Goodman reads it via
`/proc/<pid>/root/tmp/perf-<pid>.map` in containers. Symbols look like:

```
py::<qualname>:/usr/local/lib/python3.13/site-packages/requests/adapters.py
```

Resolution (`resolve.go`):

1. Reuse the perf-map interval lookup (Tier 1 path unchanged).
2. Parse `py::` symbols with a strict regex: only **absolute** `.py` paths count.
   `<frozen …>` and other non-path symbols are skipped (never-misattribute).
3. Walk the stack innermost-outward; the first frame under `site-packages/` or
   `dist-packages/` wins (same “deepest actor” rule as `node_modules`).
4. Map the path to `(package, version)` with `PathToPyPackage` (`package.go`):
   deepest site-packages segment, version from adjacent `*.dist-info/` (`METADATA`
   or the `name-version.dist-info` directory name).
5. Native C extensions under site-packages can also attribute via `/proc/<pid>/maps`
   when the mapped path contains `site-packages/` or `dist-packages/`.
6. Fallbacks match Node: `<app>` for project code, `<unknown>` when nothing resolves.

**Enable perf maps:** set `PYTHONPERFSUPPORT=1` on the workload. In Kubernetes the
admission webhook injects it alongside Node `NODE_OPTIONS` (`internal/admission`).

**Sensor watch list:** `WatchedComms` includes `python`, `python3`, `python3.12`,
`python3.13`, and common WSGI/ASGI entrypoints (`gunicorn`, `celery`, `uwsgi`,
`uvicorn`). Processes that rename themselves (e.g. `setproctitle`) may need
`-comms` / `GOODMAN_EXTRA_COMMS` — see [`docs/configuration.md`](configuration.md).

Background: [`docs/research/python-attribution.md`](research/python-attribution.md),
implementation notes: [`docs/research/python-attribution-impl.md`](research/python-attribution-impl.md).

## Behavior canonicalization

Raw syscall arguments are noisy — unique temp files, ephemeral ports. The same
*logical* behavior must map to the same string so it aggregates cleanly.
(`canonical.go`)

| Raw | Canonical |
|---|---|
| `open* /app/src/routes/user-42.js` | `READ /app/src/routes/**` |
| `open* /app/node_modules/express/lib/view.js` | `READ /app/node_modules/express/**` |
| `open* /etc/hosts` | `READ /etc/hosts` (shallow paths kept verbatim) |
| `connect 140.82.113.6:443` | `CONNECT 140.82.113.6:443` |
| `execve /usr/bin/curl` | `EXEC curl` |

**Sensitive paths are never collapsed** — collapsing would hide exactly the reads
Goodman must alert on. Any path containing `secret`, `token`, `credential`,
`password`, `shadow`, `.pem`, `.key`, `.aws`, `.ssh`, `.npmrc`, `.env`, `id_rsa`,
or under `/var/run/secrets/` and `/run/secrets/` is kept verbatim. The cloud
metadata IP `169.254.169.254` is likewise always kept verbatim and flagged.

## Service detection

The `service` label for an event (`resolve.go` `detectService`) is:

1. In Kubernetes (cgroup path contains `kubepods`): the pod name from the
   process's `HOSTNAME` env var.
2. Locally: the basename of the process's working directory.
3. Fallback: `pid-<pid>`.

## The guarantee: never misattribute

> Unattributed is acceptable and honest. **Misattribution is not** — a wrong
> package name destroys trust.

Every resolver returns an `ok bool` or a sentinel. When Goodman can't confidently
name the package, it says `<app>` or `<unknown>` rather than guessing. This is a
product invariant enforced throughout `internal/attribute` and asserted by the
tests in `attribute_test.go`, which run the full resolver against a **simulated
`/proc`** tree — meaning you can develop and verify attribution logic with no
kernel at all.

## Accuracy target

≥ 80% of syscalls originating from `node_modules` code should be attributed to the
correct package on the test workload. Unattributed events are fine; wrong ones are
a bug.
