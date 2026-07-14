# Implementation plan: Python Tier-1 attribution (Phase 2 build half)

> **Status:** implementation plan (2026-07-13). The research spike is
> [`python-attribution.md`](python-attribution.md) — **GO**, live-verified on
> CPython 3.13. This document turns that GO into an executable build plan for
> `plan-deferred.md` Phase 2. It changes **no kernel code**: `bpf/` and
> `struct event` are untouched; everything happens in user space.
>
> **Gate reminder:** the build half ships for a named Python customer (or a
> clear pipeline stall on "we're mostly Python"). The plan below is written so
> an agent can execute it in ~1 week when the trigger fires. Python attribution
> itself did not require a wire change; a later enforcement hardening added
> `RawEvent.DirFD` for shared file/exec path resolution.

## What already exists (do not rebuild)

| Piece | Where | Status |
|---|---|---|
| Kernel capture for Python pids | `internal/loader/loader.go` — `python`/`python3` in `WatchedComms` | done |
| Perf-map file reader (`<hex_addr> <hex_size> <symbol>`) | `internal/attribute/perfmap.go` `PerfMap` | done — CPython emits the same format; sizes are hex; **one reader, never fork it** |
| Container namespace handling | `PerfMapPath` (`/proc/<pid>/root/tmp/perf-<nspid>.map`) + `NSPID` | done — runtime-agnostic |
| Native-frame fallback | `internal/attribute/maps.go` (`/proc/<pid>/maps`) | done — C extensions resolve here like Node native addons |
| Service detection, thread context, canonical framework | `internal/attribute/resolve.go`, `canonical.go` | done |

What is missing is everything after the symbol lookup: the `py::` symbol
parser, the site-packages → PyPI package mapping, canonical path collapse for
site-packages, and `PYTHONPERFSUPPORT=1` injection.

## 1. Symbol parser — `internal/attribute/resolve.go`

Today `sourcePathOf` matches only JS paths:

```go
// internal/attribute/resolve.go (today)
var jsPathRe = regexp.MustCompile(`\s(\/[^\s]+?\.[cm]?js)(?::\d+)?(?::\d+)?$`)

// sourcePathOf extracts the JS source file path from a perf-map symbol name.
func sourcePathOf(sym string) (string, bool) {
	m := jsPathRe.FindStringSubmatch(sym)
	if m == nil {
		return "", false
	}
	return m[1], true
}
```

CPython trampoline symbols are `py::<qualname>:<filename>` (verified live —
see the spike doc). Add a second branch **after** the JS regex; the JS regex
stays first and byte-for-byte untouched:

```go
// CPython 3.12+ perf trampoline: "py::<qualname>:<filename>".
// Accept only absolute .py paths; "<frozen importlib._bootstrap>" and
// friends must never become a package (never-misattribute).
var pySymRe = regexp.MustCompile(`^py::.+:(/[^\s]+\.py)$`)

func sourcePathOf(sym string) (string, bool) {
	if m := jsPathRe.FindStringSubmatch(sym); m != nil {
		return m[1], true
	}
	if strings.HasPrefix(sym, "py::") {
		if m := pySymRe.FindStringSubmatch(sym); m != nil {
			return m[1], true
		}
	}
	return "", false
}
```

Rejection rules (all fall through to `ok=false`, leaving the frame
unresolved — same behavior these symbols get today):

- `<frozen importlib._bootstrap>`, `<frozen io>`, … (not absolute paths)
- `<string>`, `<stdin>`, empty filenames
- relative paths (a cwd guess is a misattribution risk; revisit only with
  evidence from a real workload)

Two adjacent call sites also need the Python branch:

- **`Attribute()`** — the frame loop currently keys the package decision on
  `strings.Contains(src, "/node_modules/")`. Add a sibling branch: when the
  resolved source path contains `/site-packages/` or `/dist-packages/`, call
  `PathToPyPackage` (§2). The "deepest resolving frame wins" walk order, the
  `<app>` fallback, and the thread-context logic do not change. Python app
  code (an absolute `.py` path outside site-packages) sets `appSource` →
  `<app>` exactly like JS app code; `appVersion` reads `package.json` and will
  return `""` for Python apps — that is fine, do not invent a
  `pyproject.toml` parser in this phase.
- **`ResolveStack()`** (goodmanctl `attribute -pid` output) — the native-maps
  branch sets `f.Source` only for `/node_modules/` paths; extend it to
  site-packages/dist-packages paths so C-extension `.so` frames display.
- **`packageFromOpenedPath()`** — mirrors the JS behavior for `FILE_OPEN`
  events whose *argument* path is inside a dependency: extend the
  `/node_modules/` check with the site-packages equivalent.

## 2. `PathToPyPackage` — `internal/attribute/package.go`

New function beside `PathToPackage`, same contract shape:

```go
// PathToPyPackage turns ".../site-packages/requests/adapters.py" into
// ("requests", "2.34.2"). pidRoot is "/proc/<pid>/root" so dist-info is read
// from the target's own mount namespace.
func PathToPyPackage(pidRoot, path string) (pkg, version string, ok bool)
```

Algorithm:

1. Find the **last** `/site-packages/` or `/dist-packages/` segment (deepest
   actor, mirroring `splitNodeModules`'s `LastIndex` discipline). Extract the
   import root: the next path segment, or for single-module distributions the
   filename minus `.py` (e.g. `site-packages/six.py` → `six`).
2. Resolve `(distribution name, version)` from the adjacent
   `*.dist-info/` directories under `pidRoot + <site dir>`:
   - Parse directory names `<name>-<version>.dist-info`.
   - Map import root → distribution via each dist-info's `top_level.txt`
     (fall back to `RECORD` when absent). This is what makes `yaml` →
     `PyYAML` correct — import names and PyPI names differ.
   - If no `top_level.txt`/`RECORD` maps it, accept a PEP-503-normalized name
     match (lowercase, `-`/`_`/`.` equivalent) between import root and exactly
     one dist name.
3. **Ambiguity returns honestly:** zero or multiple candidate distributions,
   editable installs, missing dist-info → return `(importRoot, "", false)`.
   The caller then attributes package-without-version, exactly like the
   `splitNodeModules` fallback in `Attribute()` today. Never guess a version.

Add a `splitSitePackages(path) (importRoot, siteDir, rest string)` helper
mirroring `splitNodeModules`, shared by `resolve.go` and `canonical.go`.

**Cache discipline:** one dist-info index per site directory (a scan of
`*.dist-info` + `top_level.txt` reads), cached under the same
"cache misses too" rule as `readVersionField` — dist-info does not change
while a pid lives, and a per-event directory scan would be a hot-path stat
storm. Extend `FlushVersionCache()` to clear this index as well (it is called
on pid exit, when `/proc/<pid>/root` paths die).

## 3. Canonicalization — `internal/attribute/canonical.go`

`canonicalPath` collapses `node_modules` reads to `<dir>/node_modules/<pkg>/**`.
Add the Python mirror immediately after that branch:

- `…/site-packages/<import_root>/…` → `…/site-packages/<import_root>/**`
  (preserve the actual segment spelling, `site-packages` or `dist-packages`).
- The sensitive-path check stays **first** and unchanged: a read of
  `…/site-packages/foo/.aws/credentials`-shaped paths stays verbatim.
- Everything else (shallow-path passthrough, `dir/**` tail collapse) is
  untouched — Python app code paths already collapse correctly.

No changes to `CanonicalizeWith`, `aggregateConnect`, or EXEC handling:
CONNECT/EXEC behaviors are runtime-agnostic already.

## 4. Admission webhook — `internal/admission/admission.go`

Inject `PYTHONPERFSUPPORT=1` alongside `NODE_OPTIONS` (spike doc §4: env var
is the lowest-precedence, webhook-friendly enablement). The existing
`containerOps` handles the NODE_OPTIONS *flag-merge* case; PYTHONPERFSUPPORT
is simpler — exact-value, no merging:

- No `env` array → the single "create env" op must now carry **both** vars.
- `env` exists, no `PYTHONPERFSUPPORT` → append `{name, value: "1"}`.
- `PYTHONPERFSUPPORT` already present (any value, including `"0"`) or set via
  `valueFrom` → **leave untouched**. An explicit operator value is an opt-out
  we must respect, and valueFrom cannot be merged safely (same rule as
  NODE_OPTIONS today).
- Idempotent: re-admission of an already-mutated pod returns no ops.

Fail-open behavior (`Review` returns allowed on any decode error) and the
pure/unit-testable shape of `Mutate` are invariants — do not add cluster
calls. Extend `admission_test.go` with: both-vars injection into empty env,
append-only when NODE_OPTIONS exists, PYTHONPERFSUPPORT=0 untouched,
valueFrom untouched, idempotent re-admission, initContainers covered.

No Helm changes needed: the webhook targets namespaces via the existing
`goodman.io/inject=enabled` label selector; nothing new is configurable.

## 5. Risk: `WatchedComms` misses real Python entrypoints

This is the highest-probability field failure and it silently produces
*zero events*, not bad attribution. The sensor only watches pids whose
`/proc/<pid>/comm` is in:

```go
// internal/loader/loader.go (today)
var WatchedComms = map[string]bool{
	"node": true, "nodejs": true, "MainThread": true,
	"python3": true, "python": true,
}
```

Real Python deployments frequently do not present those comms:

| Deployment | Likely comm | Watched today? |
|---|---|---|
| `python3` / `python` direct | `python3`, `python` | yes |
| Versioned binary (`/usr/local/bin/python3.13`) | `python3.13` (fits the 15-char comm limit) | **no** |
| gunicorn master/workers | `gunicorn` (setproctitle can vary it further) | **no** |
| celery workers | `celery` | **no** |
| uwsgi | `uwsgi` | **no** |
| console-script entrypoints (uvicorn, flask, …) | usually the interpreter comm via shebang — varies by image | partially |

Plan:

1. Extend `WatchedComms` with the explicit high-confidence names:
   `python3.12`, `python3.13`, `gunicorn`, `celery`, `uwsgi`, `uvicorn`.
   Keep exact-match lookup — a substring/prefix match on `python` invites
   watching unrelated processes and is a bigger change to `RefreshWatched`
   than this phase needs.
2. Document the escape hatch that already exists: the sensor's
   `-comms` flag / `GOODMAN_EXTRA_COMMS` env (`cmd/sensor/main.go`) covers any
   customer-specific comm without a release. Add this to
   `docs/configuration.md` and the troubleshooting doc ("Python workload
   produces no events → check `/proc/<pid>/comm`, add it to `-comms`").
3. During customer rollout, verify against the *customer's actual image*:
   `kubectl exec … cat /proc/1/comm`. Note the AGENTS.md precedent: Node
   already needs three comm spellings (`node`/`nodejs`/`MainThread`); Python
   is worse, and setproctitle means the list is never exhaustive — hence the
   flag, docs, and runbook step rather than a heuristic.

Also note: gunicorn/celery pre-fork **workers** inherit
`PYTHONPERFSUPPORT=1` from the master's environment, and each worker pid
writes its own perf map, so the existing per-pid `PerfMap` state is correct —
but only if the worker comm is watched. The comm list is the whole risk.

## 6. Tests — `internal/attribute/attribute_test.go` (primary verification)

Extend the simulated-`/proc` tree (the pattern `TestAttributeEndToEnd`
already uses; no kernel, no root):

- **Python pid fixture:** `<tmp>/proc/<pid>/` with `root/tmp/perf-<pid>.map`
  containing live-shaped lines from the spike:
  - `py::<module>:/app/venv/lib/python3.13/site-packages/requests/__init__.py`
  - `py::_find_and_load:<frozen importlib._bootstrap>` (must resolve to
    nothing)
  - a `py::…:/app/work.py` app frame
  plus `root/app/venv/lib/python3.13/site-packages/requests/…`,
  `requests-2.34.2.dist-info/{METADATA,top_level.txt}`.
- **Assertions:**
  - a stack whose deepest resolving frame is the requests frame attributes
    `requests@2.34.2`;
  - dist-info removed → `requests@""` (package-without-version, `ok=false`
    path), never a guessed version;
  - only-frozen-frames stack + app frame → `<app>`;
  - a `yaml`-style import-root≠dist-name fixture (`top_level.txt` mapping)
    attributes the distribution name;
  - `TestCanonicalize` gains site-packages collapse + sensitive-path-verbatim
    cases;
  - new `TestPathToPyPackage` mirroring `TestPathToPackage` (scoped: nested
    site-packages, dist-packages spelling, single-module `six.py`).
- **Existing tests must pass unchanged** — especially every JS case; the JS
  regex and node_modules logic are not edited, only added-after.
- `internal/admission/admission_test.go` additions per §4.
- `internal/loader/loader_test.go` needs no change (no BPF object change),
  but the `WatchedComms` extension gets a trivial map-content test if one
  exists; otherwise skip.

## 7. File touch list

| File | Change |
|---|---|
| `internal/attribute/resolve.go` | `pySymRe` + `sourcePathOf` branch; site-packages branch in `Attribute`, `ResolveStack`, `packageFromOpenedPath` |
| `internal/attribute/package.go` | `PathToPyPackage`, `splitSitePackages`, dist-info index cache, `FlushVersionCache` extension |
| `internal/attribute/canonical.go` | site-packages collapse in `canonicalPath` |
| `internal/attribute/attribute_test.go` | Python fixtures + tests (§6) |
| `internal/loader/loader.go` | `WatchedComms` additions (§5) |
| `internal/admission/admission.go` + `admission_test.go` | `PYTHONPERFSUPPORT=1` injection |
| `docs/attribution.md` | Python Tier-1 section (replace the current one-paragraph pointer) |
| `docs/configuration.md` | `-comms`/`GOODMAN_EXTRA_COMMS` Python guidance |
| `docs/getting-started.md`, `docs/troubleshooting.md` | Python quickstart + "no events" runbook entry |
| `CHANGELOG.md` | entry |

Explicitly **not** touched: `bpf/`, `internal/model` (wire struct unchanged),
`internal/store`, `internal/diff`, dashboard (behavior strings and package
names flow through unchanged).

## 8. Verification

```bash
make vet && make test && make smoke
```

No `bpf/` change, so no `sudo make e2e` is *required* — but before a
Python-footprint customer installs, a human should run the live check from
the spike doc (venv + `PYTHONPERFSUPPORT=1` + `goodmanctl attribute -pid …`)
on a real kernel, plus the containerized NSPID follow-up listed in
[`python-attribution.md`](python-attribution.md) §Follow-ups.

## DoD (from plan-deferred.md Phase 2, made concrete)

- Simulated-`/proc` test attributes a site-packages frame to the right
  `pkg@version`, including an import-root≠dist-name case.
- Frozen/builtin symbols and missing dist-info degrade to
  `<app>`/package-without-version — never a wrong name.
- Webhook injects `PYTHONPERFSUPPORT=1` idempotently; valueFrom and explicit
  operator values untouched.
- `WatchedComms` covers versioned python + gunicorn/celery/uwsgi/uvicorn, and
  the `-comms` escape hatch is documented.
- `make vet && make test && make smoke` green; JS attribution tests unchanged.
