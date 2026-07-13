# Research: Python Tier-1 attribution (perf trampoline)

> **Status:** spike findings (2026-07-13). **Build call: GO when a named
> customer needs Python** — not speculative product merge.
> **Audience:** maintainer / agent picking up Phase 2 of `plan-deferred.md`.

## Question

Can Goodman attribute syscalls from CPython workloads to a PyPI
`package@version` by reusing the existing Tier-1 perf-map path (the same
machinery Node uses today), or is Python a separate project?

## Method (what was actually run)

Live capture on this workstation, not inference:

```text
Python 3.13.14
PYTHONPERFSUPPORT=1
venv + pip install requests
short script looping requests.get(...)
```

Observed artifact: `/tmp/perf-<pid>.map` on two runs (pids `261753`,
`263180`). Second run (venv + `requests`, ~2s workload) measured:

| Metric | Count |
|---|---:|
| Total map lines | 1651 |
| Containing `<frozen` | 218 |
| Filesystem-style (`py::` + `:/`) | 1397 |
| Containing `site-packages` | 352 |
| Under `site-packages/requests` | 145 |

Official docs and CPython source were cross-checked the same day.

Sources:

- [Support for Perf Maps](https://docs.python.org/3/c-api/perfmaps.html)
  (example entry `py::bar:/run/t.py`)
- [Python support for the Linux perf profiler](https://docs.python.org/3/howto/perf_profiling.html)
  (`PYTHONPERFSUPPORT`, `-X perf`, `sys.activate_stack_trampoline("perf")`)
- CPython `Python/perf_jit_trampoline.c` formats names as
  `py::<function_name>:<filename>`

## Confirmed facts

### 1. Perf map file format matches what Goodman already parses

Linux perf map lines are:

```text
<hex_addr> <hex_size> <symbol_name...>
```

Live examples from CPython 3.13 with `PYTHONPERFSUPPORT=1`:

```text
7f39b5d3f800 c py::<module>:/tmp/.../site-packages/requests/__init__.py
7f39b5d3f7e0 c py::<module>:/tmp/.../work.py
7fcd12cfb000 c py::_find_and_load:<frozen importlib._bootstrap>
7f3529fcf759 b py::bar:/run/t.py          # from upstream docs
```

Adjacent version metadata on that run: `requests-2.34.2.dist-info`.

`internal/attribute/perfmap.go` already parses this layout for V8. **Do not
fork a second reader.** Size fields are hex (`b`, `c`, `1e0`, …) — already
handled.

### 2. Symbol shape is different from V8 (parser work required)

| Runtime | Example symbol | Source path extraction today |
|---|---|---|
| V8 / Node | `LazyCompile:*fn /app/node_modules/pkg/x.js:12:3` | `sourcePathOf` / `jsPathRe` (`\.[cm]?js`) |
| CPython 3.12+ | `py::<qualname>:<filename>` | **no match** — returns `ok=false` |

So Python stacks are captured by the sensor today (`python`/`python3` are
already in `WatchedComms`) but attribute almost entirely to `<app>` /
`<unknown>` because `sourcePathOf` only understands JS paths.

### 3. Many symbols are not filesystem paths

A large fraction of trampoline entries use filenames like
`<frozen importlib._bootstrap>`, `<frozen io>`, etc. Those must **never**
be turned into package names. Rule for the parser:

- Accept only when the filename part looks like an absolute path
  (`/` or, carefully, a real relative path under the container root).
- Reject `<frozen …>`, empty filenames, and anything ambiguous → leave the
  frame unresolved (never-misattribute).

### 4. Flag injection is env-var shaped (webhook-friendly)

Enablement (precedence documented by CPython):

1. `sys.activate_stack_trampoline("perf")` (in-process)
2. `-X perf`
3. `PYTHONPERFSUPPORT=1`

For Kubernetes, injecting `PYTHONPERFSUPPORT=1` via the existing admission
webhook (alongside `NODE_OPTIONS`) is the right operator story — same
idempotency / `valueFrom` untouched rules as today.

### 5. Package boundary is site-packages + dist-info (not package.json)

Once a filesystem path is recovered, PyPI identity is:

- Path segment `…/site-packages/<import_root>/…` or `…/dist-packages/…`
- Version from adjacent `*.dist-info/` (`METADATA` `Name:` / `Version:`, or
  the directory name `<name>-<version>.dist-info`)

Ambiguity (editable installs, namespace packages, missing dist-info) must
return package-without-version or `<unknown>` — same honesty bar as npm.

Native C-extension frames still resolve through `/proc/<pid>/maps` the way
Node native addons do today; the trampoline only covers **pure-Python**
callables.

## Effort estimate

| Work | Estimate | Notes |
|---|---|---|
| `sourcePathOf` Python branch + tests | 0.5–1 day | Keep JS regex first and untouched |
| `PathToPyPackage` + dist-info reader | 1–2 days | Hardest honesty cases: namespace pkgs, `.dist-info` missing |
| Canonical path collapse for site-packages | 0.5 day | Mirror node_modules collapse |
| Webhook `PYTHONPERFSUPPORT=1` | 0.5 day | Extend admission tests |
| Docs + simulated `/proc` fixture | 0.5 day | Primary verification stays no-root |
| **Total build** | **~1 week** | Matches plan-deferred “cheap half” |

Not in scope for Tier-1: PyPy, embedding hosts that never write a perf map,
or walking CPython frame objects from eBPF (that would be a separate
research track).

## Decision

**GO (build when gated trigger fires).** The design in `plan-deferred.md`
Phase 2 is correct against live evidence and upstream docs:

- Reuse one `PerfMap`
- Add a Python symbol parser beside the JS one
- Map `site-packages` → dist-info for version
- Inject `PYTHONPERFSUPPORT=1` via admission

**Do not merge the build half without a named Python customer** (or a clear
pipeline stall on “we're mostly Python”). The spike alone is enough to tell
prospects: “CPython 3.12+ is a ~1 week add; here is the research.”

## Reproduce

```bash
python3 -m venv /tmp/gm-py && /tmp/gm-py/bin/pip install requests
cat >/tmp/gm-work.py <<'PY'
import os, time, requests
def tick():
    try: requests.get("http://127.0.0.1:9", timeout=0.05)
    except Exception: pass
print(os.getpid(), flush=True)
for _ in range(40):
    tick(); time.sleep(0.05)
PY
PYTHONPERFSUPPORT=1 /tmp/gm-py/bin/python /tmp/gm-work.py &
# then: head /tmp/perf-<pid>.map ; grep site-packages /tmp/perf-<pid>.map | head
```

## Follow-ups before coding

1. Confirm Debian/Ubuntu `dist-packages` vs venv `site-packages` path shapes
   in a container image the customer actually runs.
2. Confirm NSPID + `/proc/<pid>/root/tmp/perf-<nspid>.map` for a containerized
   Python workload the same way Node is tested today.
3. Decide how deep into dependency trees to attribute when both `requests`
   and `urllib3` appear on the same stack (deepest site-packages frame wins,
   mirroring deepest `node_modules` today).
