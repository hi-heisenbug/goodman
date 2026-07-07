# AGENTS.md

Guidance for coding agents (and humans who like precise maps) working in this
repository. This file follows the [agents.md](https://agents.md) convention:
read it before making changes. It is the single source of truth for how to
build, test, and extend Goodman without breaking its invariants.

> **TL;DR for an agent starting a task**
> 1. `make doctor` — confirm the toolchain and kernel are usable.
> 2. `make build && make test` — must be green before you start.
> 3. Make your change. Keep the C struct and Go struct in lockstep (see §"The one rule you must never break").
> 4. Dashboard changes: `make dashboard` and commit `internal/api/ui/dist/`.
> 5. `make vet && make test && make smoke` — smoke needs **no root** and exercises the whole backend.
> 6. Live eBPF changes: `sudo make e2e` (needs root; can't run in an unprivileged sandbox).

---

## What Goodman is (in one paragraph)

Goodman attributes each security-relevant Linux syscall to the **npm/PyPI
package** that caused it, learns a per-`(service, package, version)` behavioral
baseline, and raises an alert when a dependency's behavior drifts. An eBPF
program captures the syscall plus the user-space call stack; user space resolves
that stack to a JavaScript source path, maps it to a package, and diffs live
behavior against the baseline. The hard part is **attribution** (`internal/attribute/`);
everything else is plumbing around it.

Read [`docs/architecture.md`](docs/architecture.md) for the full design and
[`docs/attribution.md`](docs/attribution.md) for the attribution algorithm.

---

## Production quality bar

Goodman is an OSS security product, not a demo repository. Changes should leave
the project understandable to a new contributor, runnable from a fresh clone, and
safe to deploy in a real cluster.

- Keep the full runtime path working: sensor → attribution → collector → store →
  diff → API/SSE → dashboard.
- Prefer a smaller correct change over a broad rewrite. This code has kernel,
  database, API, and UI boundaries; accidental churn is expensive.
- Keep source, generated build artifacts, tests, docs, Docker, and Helm in sync
  when a change crosses those boundaries.
- Never commit credentials, sudo passwords, local machine paths, temporary
  browser profiles, or one-off debugging state.
- The dashboard brand is **HEISENBUG**, while the Go module remains
  `github.com/goodman-sec/goodman`. Do not rename packages, module paths, or
  public API surfaces as part of cosmetic work.

---

## Repository map

```
goodman/
├── bpf/                      # kernel side (eBPF C, CO-RE)
│   ├── goodman.bpf.c         # the eBPF program: 3 tracepoints + user-stack capture
│   ├── goodman.h             # struct event — MUST match internal/model RawEvent byte-for-byte
│   ├── vmlinux.h             # generated from kernel BTF (make vmlinux); do not hand-edit
│   └── include/bpf/          # vendored libbpf headers (v1.5.0)
│
├── cmd/                      # binaries (one main package each)
│   ├── sensor/               # eBPF loader + attributor → posts to collector (runs as root/DaemonSet)
│   ├── collector/            # ingest + fingerprint + diff + API + embedded dashboard
│   └── goodmanctl/           # dev/ops CLI: tail, alerts, ack, fingerprints, attribute
│
├── internal/                 # the pipeline (import graph flows left→right below)
│   ├── model/                # shared types: RawEvent, Attributed, Fingerprint, Alert. NO deps.
│   ├── loader/               # cilium/ebpf load+attach+ringbuf; embeds goodman.bpf.o
│   ├── attribute/            # THE HARD PART: stack → package@version + canonical behavior
│   │   ├── maps.go           #   parse /proc/<pid>/maps (native frames)
│   │   ├── perfmap.go        #   parse /tmp/perf-<pid>.map (V8 JIT frames, Tier 1)
│   │   ├── resolve.go        #   orchestrates resolution + service detection
│   │   ├── package.go        #   path → (package, version) via package.json
│   │   └── canonical.go      #   raw arg → stable behavior string
│   ├── store/                # database/sql over Postgres AND SQLite (one codepath)
│   │   └── migrations/       #   *.postgres.sql and *.sqlite.sql (dialect-suffixed)
│   ├── fingerprint/          # aggregate Attributed events → behavior sets; promote baselines
│   ├── diff/                 # baseline vs live → drift; config-driven high-risk rules
│   └── api/                  # chi HTTP handlers + SSE + Prometheus + serves the UI
│       └── ui/               # embed.go + dist/ (built dashboard, committed)
│
├── dashboard/                # React + Vite + TypeScript source (built into internal/api/ui/dist)
├── deploy/                   # docker/ (multi-stage) + helm/goodman/ (chart)
├── test/                     # workload/ (victim Node app), fixtures/ (benign drift pkgs), e2e/
├── scripts/                  # setup.sh, preflight.sh
└── docs/                     # architecture, getting-started, deployment, configuration, api, …
```

**Data flow:** kernel → `loader` (RawEvent) → `attribute` (Attributed) → HTTP →
`api` → `fingerprint` (Update) → `diff` (Alert) → `store` → API → dashboard.

---

## The one rule you must never break

`bpf/goodman.h` `struct event` and `internal/model/types.go` `RawEvent` describe
the **same bytes** crossing the kernel/user-space boundary. They must have
identical layout: same field order, sizes, and alignment.

- If you add/remove/reorder a field in one, do it in the other **in the same change**.
- `internal/model/types_test.go` asserts the exact size and every field offset.
  If it fails, the structs have drifted — fix the layout, do not edit the test
  to match.
- The padding is explicit (`_pad[3]` / `Pad [3]byte`) so the compiler inserts
  none implicitly. Keep it that way.

This is the highest-severity invariant in the codebase. A silent layout drift
produces garbage attribution with no crash.

---

## Build, test, run

All targets are in the `Makefile`; run `make help` for the list.

| Command | What it does | Needs root? |
|---|---|---|
| `make doctor` | Preflight: checks tools, kernel BTF, headers, prints guidance | no |
| `make bpf` | Compile `bpf/goodman.bpf.c` → `.o` (also copied into the loader pkg) | no |
| `make dashboard` | `npm install && vite build`, copy into `internal/api/ui/dist` | no |
| `make build` | Build `sensor`, `collector`, `goodmanctl` into `bin/` | no |
| `make test` | `go test ./...` (unit tests, all packages) | no |
| `make smoke` | Backend end-to-end via synthetic events → asserts one CRITICAL alert | **no** |
| `make e2e` | **Real eBPF** drift replay: sensor + Node workload → alert | **yes** |
| `make docker` | Build both container images | no (docker daemon) |
| `make helm-lint` | Lint the Helm chart | no |
| `make vet` | `go vet ./...` | no |
| `make clean` | Remove build artifacts | no |

**When you finish a change, the bar is:** `make vet && make test && make smoke`
all green. If you touched anything under `bpf/` or `internal/loader/`, note in
your summary that `sudo make e2e` still needs to be run by a human on a real
kernel, and why (unprivileged sandboxes can't load BPF — see below).

For dashboard/UI changes, the finish bar is higher:

```bash
make dashboard
(cd dashboard && npm audit --audit-level=moderate)
make build
make vet
make test
make smoke
```

Also visually verify at desktop and mobile widths with real-shaped API data.
The dashboard must render live alerts/fingerprints from `/v1/*`; a static
screenshot or hard-coded sample data is not an acceptable verification.

### Why `make e2e` may not run where you are

Loading an eBPF program needs `CAP_BPF`/root. Many CI and agent sandboxes set
`kernel.unprivileged_bpf_disabled=2` and gate `sudo` behind a password. In that
environment:

- `make smoke` still fully exercises store → fingerprint → diff → API → dashboard
  with synthetic `Attributed` events. Use it as your fast feedback loop.
- The kernel-side artifact is still checked without root by
  `internal/loader/loader_test.go`, which parses the embedded `.o` and asserts
  the three tracepoint programs and the ringbuf/hash/percpu maps exist and are
  typed correctly.
- The attribution logic is unit-tested against a **simulated `/proc`** tree in
  `internal/attribute/attribute_test.go` — you can change and verify attribution
  behavior with no kernel at all.

---

## Conventions

- **Language:** Go 1.23+ for all binaries; C (CO-RE) for the eBPF program;
  TypeScript + React for the dashboard.
- **Go style:** standard `gofmt`; errors wrapped with `fmt.Errorf("...: %w", err)`;
  no naked `panic` in library code. Keep `internal/model` dependency-free — it is
  imported by everything.
- **Never misattribute.** Every resolver returns an `ok bool` (or a sentinel like
  `<app>`/`<unknown>`). Prefer "unknown" over a wrong package name; a wrong
  package destroys user trust. This is a product invariant, not a style choice.
- **Don't block the ring-buffer reader.** The sensor hands events to a buffered
  channel and drops (with a counter) if full. Any new network/IO in the hot path
  must preserve this.
- **Rules over `if`s.** High-risk detection lives in a config-driven rule list
  (`internal/diff/diff.go`, `deploy/rules.example.json`), never hard-coded
  conditionals — customers tune it.
- **SQL must run on both dialects.** `internal/store` uses `$N` placeholders and
  `ON CONFLICT ... DO UPDATE`, which work in both Postgres and SQLite. Add
  migrations as *both* `NNN_name.postgres.sql` and `NNN_name.sqlite.sql`.
- **The dashboard is committed built.** `internal/api/ui/dist/` is checked in so
  `go build` works on a fresh clone without Node. If you change `dashboard/src`,
  run `make dashboard` and commit the rebuilt `dist/`.
- **Dashboard design system:** HEISENBUG uses local `@fontsource` fonts only:
  headings/index labels are DM Sans 700; body/control text is Inter 400/600/700.
  Keep the light-mode palette aligned with the supplied brand system:
  `#93cb52`, `#1c9770`, `#bef3e2`, `#f2eeee`, `#464646`.
- **Dashboard UX bar:** keep controls data-backed, responsive, and operational.
  Avoid landing-page/marketing layouts, decorative gradient blobs, nested cards,
  text overflow, and mobile horizontal scroll. Cards should stay at 8px radius or
  less unless the whole design system changes.

---

## How to make common changes

**Add a new captured syscall (e.g. `sys_enter_unlinkat`)**
1. Add an `enum event_type` value in `bpf/goodman.h` and mirror it in
   `internal/model/types.go` (`EventType` const + `String()`).
2. Add a `SEC("tracepoint/syscalls/sys_enter_<name>")` handler in
   `bpf/goodman.bpf.c` using the `reserve_event`/`watched` helpers; read the
   relevant `ctx->args[...]` into `e->arg` (mask indices — see the connect
   handler for the pattern the verifier requires).
3. Attach it in `internal/loader/loader.go` (`New()` tracepoint map).
4. Canonicalize it in `internal/attribute/canonical.go`.
5. `make bpf && make build && make test`.

**Add a high-risk rule** — edit `deploy/rules.example.json` (or `DefaultRules`
in `internal/diff/diff.go`). Patterns are case-insensitive regexes matched
against the canonical behavior string (`READ …`, `CONNECT …`, `EXEC …`).

**Add an API endpoint** — add the route in `internal/api/api.go` `Router()`,
implement the handler, document it in [`docs/api.md`](docs/api.md), and add a
`goodmanctl` subcommand if it's operator-facing.

**Change attribution** — work in `internal/attribute/`; extend the simulated
`/proc` fixtures in `attribute_test.go` to cover the new case. Do not weaken the
"never misattribute" guarantee.

**Change the wire event** — see "The one rule you must never break" above.

---

## Common mistakes to avoid

### eBPF and attribution

- Do not edit `bpf/vmlinux.h` by hand. It is generated. Regenerate it with
  `make vmlinux` only when intentionally targeting a different kernel.
- Do not "fix" `internal/model/types_test.go` when `RawEvent` layout fails.
  Fix the C and Go structs so they match.
- Do not make attribution guessier to reduce `<unknown>` results. A wrong package
  name is worse than an unknown package.
- Do not read `/tmp/perf-<pid>.map` directly from the host namespace. Goodman
  reads `/proc/<pid>/root/tmp/perf-<pid>.map` so containerized workloads resolve
  against their own mount namespace.
- Do not add blocking network, filesystem, or database work to the sensor hot
  path. The ring-buffer reader must stay fast and lossy under pressure.

### Storage, API, and rules

- Do not add a migration for only one database. Every schema change needs both
  `.postgres.sql` and `.sqlite.sql`.
- Do not introduce Postgres-only SQL into shared store code unless the SQLite
  path is explicitly separated and tested.
- Do not hard-code new high-risk detections as scattered `if` statements. Use the
  configurable rule list so operators can tune behavior.
- Do not change API response shapes without updating `docs/api.md`, the CLI if
  relevant, and the dashboard types.

### Dashboard and frontend

- Do not edit `dashboard/src` and forget `make dashboard`. The collector serves
  committed files from `internal/api/ui/dist`, not the Vite source tree.
- Do not add CDN fonts or remote design assets. The dashboard must work in
  offline/private deployments; use local `@fontsource` assets.
- Do not replace live API/SSE behavior with mock data in production components.
  Mock data is fine only in tests or local visual harnesses.
- Do not let long package names, versions, paths, behavior strings, or action
  buttons create mobile horizontal scroll. Check mobile width before merging.
- Do not use a landing-page layout for the product UI. Operators need dense,
  scannable alert and fingerprint workflows.

### Testing and release hygiene

- Do not treat `make smoke` as redundant. It catches real collector, store,
  fingerprint, diff, API, and alert behavior without root.
- Do not claim live eBPF coverage if `sudo make e2e` did not run. Say clearly
  when only the no-root test path was verified.
- Do not leave temporary servers, collectors, sensors, Chrome instances, or test
  databases running after local verification.
- Do not commit stale Vite hashed assets. If `dashboard/dist` changes, old hashed
  files in `internal/api/ui/dist/assets/` should usually disappear and new ones
  should appear.
- Do not change repo scripts just because a local agent/container lacks a tool.
  For example, use `grep` locally if `rg` is missing, but do not weaken project
  tooling for that environment.

### Known non-bugs

- The `put_u16_dec` "loop not unrolled" clang warning is expected and harmless;
  it is a bounded loop the kernel verifier accepts on supported kernels.
- `make e2e` needs root or the right BPF capabilities. In unprivileged sandboxes,
  use `make smoke` and state that the live kernel path was not run.
- Live e2e chooses dynamic collector/workload/sink ports by default. For a
  port-sensitive reproduction, set `GOODMAN_E2E_COLLECTOR_PORT`,
  `GOODMAN_E2E_WORKLOAD_PORT`, or `GOODMAN_E2E_SINK_PORT`.
- Node may report its process comm as `node`, `nodejs`, or `MainThread`. Keep
  those names covered when changing watch logic.
- Simple headless screenshot commands may hang on pages with SSE. Use bounded
  browser automation that waits for rendered DOM text, captures, then exits.

---

## Where to read more

| Doc | Purpose |
|---|---|
| [`docs/getting-started.md`](docs/getting-started.md) | Local setup, first run, first alert |
| [`docs/architecture.md`](docs/architecture.md) | Components, data flow, design decisions |
| [`docs/attribution.md`](docs/attribution.md) | The stack→package algorithm (Tier 1/2) |
| [`docs/configuration.md`](docs/configuration.md) | Every flag, env var, and Helm value |
| [`docs/deployment.md`](docs/deployment.md) | Kubernetes / Helm production guide |
| [`docs/api.md`](docs/api.md) | REST + SSE + metrics reference |
| [`docs/development.md`](docs/development.md) | Dev loop, testing strategy, releasing |
| [`docs/troubleshooting.md`](docs/troubleshooting.md) | Common failures and fixes |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Human contributor workflow |
| [`plan.md`](plan.md) | The original build plan (phases + DoD) |
