# Tier-2 (flagless) attribution — research spike

> **Status:** PARK (year-scale). Decision date: 2026-07-13.
> **Timebox:** Phase 0 of `plan-deferred.md` (5-day research spike).
> **Live prototype:** not run in this session; decision is based on
> Goodman's Tier-1 code, public V8/llnode facts, and the never-misattribute
> invariant. Commands to reproduce a prototype later are at the bottom.

## Question

Is in-kernel V8 stack → source-path attribution (no `--perf-basic-prof`) a
**quarter of work** or a **year**?

## Decision: PARK (year-scale)

**Do not start a production Tier-2 build.** Keep Tier-1 + the NODE_OPTIONS
admission webhook as the shipping answer. Revisit only if:

1. two or more deals stall *specifically* on NODE_OPTIONS (cannot restart
   pods / perf-flag overhead / non-K8s), **and**
2. a fresh user-space prototype (commands below) resolves script names on
   ≥2 Node LTS versions using **V8 metadata offsets**, not hand RE.

### Why PARK

| Factor | Finding |
|---|---|
| User-space feasibility | In principle workable via `/proc/<pid>/mem` + V8 tagged pointers (llnode / `v8dbg_` constants do this offline). Not proven here on Node 18/20/22. |
| Offset stability | V8 heap layout moves across minor releases. A per-Node-LTS offset table *might* be shippable if taken from V8 postmortem metadata; hand-maintained RE per release is not. Without a measured metadata path → year-scale. |
| eBPF verifier budget | A full `JSFunction → SharedFunctionInfo → Script → source` chase needs many `bpf_probe_read_user` hops, bounded string copies, and GC-race tolerance. Fitting that while guaranteeing **never-misattribute** (torn read → `<unknown>`, never a wrong package) is the hard part — likely multiple kernel/verifier iterations. |
| Moat vs objection | The webhook already removes most NODE_OPTIONS friction. Tier-2 deepens the moat but is not required for the first paid pilot. |
| Fallback | Tier 1 must remain the permanent default; Tier 2 would ship dark (flag off). That doubles the support surface until confidence is high. |

**Effort estimate if forced GO later:** 2–4 engineer-quarters for a flag-gated
MVP on 2 LTS lines, assuming metadata offsets work; 12+ months if offsets
require ongoing reverse engineering.

## Current Tier-1 baseline (what we already have)

From `docs/attribution.md` and `internal/attribute/`:

1. Kernel captures user stacks via frame pointers (`bpf_get_stack`).
2. JIT frames resolve through V8's `/tmp/perf-<pid>.map` when
   `--perf-basic-prof-only-functions --interpreted-frames-native-stack` is set.
3. Deepest `/node_modules/` frame wins; else `<app>`; else `<unknown>`.
4. Paths resolve through `/proc/<pid>/root` for containers.

Tier 2 would replace step 2 for processes **without** the perf map, by
walking V8 objects from on-stack pointers.

## Investigation notes (desk research)

### 1. User-space prototype path

Approach (not executed here):

1. Start Node with a known script; obtain a JSFunction address from a sampled
   stack (or from the isolate's heap roots via a debugger).
2. Open `/proc/<pid>/mem`, read tagged pointers with the same layout llnode
   uses (`JSFunction` → `shared` → `script` → `name` / `source`).
3. Confirm the script name string matches the file on disk.

If step 2–3 fail on a stock Node LTS binary without private symbols, in-kernel
is dead on arrival.

### 2. V8 layout drift (Node 18 / 20 / 22)

- Node ships specific V8 versions; constants differ across those lines.
- Tools like **llnode** and Chrome's postmortem support rely on `v8dbg_` /
  debug metadata when available — that is the only acceptable offset source
  for a security product (never-misattribute).
- Acceptable product shape: embed a small table keyed by
  `(node_major, v8_version)` generated at build time from V8 headers/metadata.
- Unacceptable: "we RE'd 20.11 and hope 20.18 matches."

### 3. eBPF feasibility sketch

Rough program shape (not implemented):

```
for each user IP in stack (bounded ≤ 32):
  if maps says anonymous-exec:
    candidate = read_user(IP - const) as JSFunction*   // fragile
    shared = read_user(candidate + off_shared)
    script = read_user(shared + off_script)
    name   = read_user_str(script + off_name, ≤ 128)
    if name looks like a path → keep; else discard frame
```

Risks: wrong object type → garbage string → **misattribution**. Mitigation:
strict tagged-pointer checks, type-info fields, and on any doubt return
`<unknown>`. Verifier: every read needs explicit bounds; string loops must be
capped. Expect several redesigns.

### 4. Fallback interplay

- Default: Tier 1 only (today).
- Future flag: `-attribution-tier=v8native` (dark), fall back to perf-map when
  present, else `<unknown>`.
- Never prefer a low-confidence Tier-2 name over `<unknown>`.

## Reproduce later (human commands)

```bash
# Terminal A — victim with a stable handleRequest-style function
node --perf-basic-prof-only-functions --interpreted-frames-native-stack -e '
  const http = require("http");
  http.createServer((req,res)=>res.end("ok")).listen(9999);
  setInterval(()=>{}, 1000);
'

# Terminal B — after installing a Node that matches production LTS
# 1) Find the pid and confirm perf map exists:
ls -l /tmp/perf-$(pgrep -n node).map

# 2) Prototype (throwaway): see hack/tier2-spike/README.md
#    Goal: from one on-stack JIT IP, print the script path WITHOUT reading
#    the perf map — only /proc/<pid>/mem + V8 offsets from metadata.

# 3) Repeat on Node 18 and Node 22 containers. If either fails without
#    hand-tuned offsets → PARK remains correct.
```

## GO criteria (all required)

1. User-space prototype resolves a real `.js` path on ≥2 Node LTS majors.
2. Offsets come from V8 metadata / generated tables, not manual RE.
3. eBPF sketch has a credible never-misattribute story under GC races.
4. Maintainer believes the estimate is ≤1 quarter for an MVP behind a flag.

Today **1–3 are unproven** → **PARK**.

## Related

- Product attribution design: [`docs/attribution.md`](../attribution.md)
- Sequencing: [`plan-deferred.md`](../../plan-deferred.md) Phase 0
- Throwaway prototype notes: [`hack/tier2-spike/README.md`](../../hack/tier2-spike/README.md)
