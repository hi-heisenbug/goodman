# Troubleshooting

Start with `make doctor` — it diagnoses most setup problems and tells you the fix.
Below are the failures it can't catch and how to resolve them.

## Build

**`make build` fails with a missing `goodman.bpf.o` / embed error**
The Go loader embeds the compiled eBPF object. Run `make bpf` first (the Makefile
wires this, but a partial build can miss it). Ensure `clang`, `llvm`, and
`bpftool` are installed.

**`clang` errors compiling `bpf/goodman.bpf.c`**
Check `bpf/vmlinux.h` exists (`make vmlinux` to regenerate) and that
`bpf/include/bpf/*.h` (vendored libbpf) are present. If you deleted the vendored
headers, `sudo apt-get install -y libbpf-dev` and add `-I /usr/include` to the
compile.

**`go build` complains about the Go version**
The module targets Go 1.23+. `go version` must be ≥ 1.23.

## Sensor won't start

**`load eBPF: remove memlock rlimit: operation not permitted`**
The sensor needs root/`CAP_BPF`. Run it with `sudo`, or in Kubernetes ensure the
DaemonSet is privileged. Check `make doctor`'s "Kernel eBPF support" section —
if `unprivileged_bpf_disabled` is `1` or `2`, unprivileged loading is off.

**`BPF verifier rejected program`**
Almost always a kernel/BTF mismatch. Confirm `/sys/kernel/btf/vmlinux` exists and
the kernel is ≥ 5.8. Regenerate `bpf/vmlinux.h` **on the target kernel** with
`make vmlinux` and rebuild. Include the full verifier log when filing an issue.

**`attach sys_enter_*: no such file or directory`**
One or more syscall tracepoints aren't available — the kernel was built without
`CONFIG_FTRACE`/tracepoints, or you're in a restricted container. Run the sensor
on the host (or a privileged DaemonSet with `hostPID`).

## No events / no attribution

**`goodmanctl tail` shows nothing**
1. Is the sensor watching the process? It traces built-in runtime comm names
   (`node`, `nodejs`, `MainThread`, `python`, `python3`) and `-comms` extras.
   Check `goodman_sensor_watched_pids` > 0.
2. Is the process actually making file-open, `connect`, or `execve` calls? Drive traffic.
3. In k8s, is `-proc-root=/host/proc` set and `/proc` mounted?

**Events show `<unknown>` or `<app>` instead of a package**
This is attribution falling back honestly, not a crash. Common causes:
- The Node process wasn't started with
  `--perf-basic-prof --interpreted-frames-native-stack`, so there's no
  `/tmp/perf-<pid>.map` for JIT frames. This is the **most common** cause.
- The syscall genuinely came from the app's own code (`<app>`) or from native
  code with no `node_modules` frame (`<unknown>`).
- The perf map exists but is stale — Goodman refreshes on mtime/size change; give
  it a moment after the process JITs new code.

Verify the perf map exists:
```bash
ls -l /proc/<pid>/root/tmp/perf-<pid>.map
```

**Attribution names the wrong package**
This should never happen — file a bug. Include the event's behavior, the expected
package, and the relevant `perf-<pid>.map` lines. Goodman prefers `<unknown>` to a
wrong answer by design.

## No alerts

**A drifted version doesn't alert**
- Is the *previous* version a baseline yet? Goodman only alerts once a baseline
  exists. Check `goodmanctl fingerprints` for `[BASELINE]`. During development,
  shorten the window: `GOODMAN_LEARN_OBS=50 GOODMAN_LEARN_MIN_AGE=1s`.
- Baselines need both enough observations **and** enough wall-clock age. A brand
  new deployment hasn't learned anything to drift from yet.

**Too many alerts / false positives**
The learning window is probably too short for your workload, so the baseline is
incomplete and normal behavior looks like drift. Raise `learn-obs` /
`learn-min-age`. Tune the high-risk rules if a benign behavior is being escalated
to CRITICAL (see [configuration.md](configuration.md#high-risk-rules)).

## Collector / store

**`connect postgres: …`**
Check the DSN and network reachability. The collector pings the store at startup
and exits if it can't connect. For a quick local run, drop the DSN to use SQLite:
`GOODMAN_DSN=goodman.db`.

**Baselines disappeared after a collector restart**
With SQLite, the chart defaults to a PVC at `/data`
(`store.persistence.enabled=true`). If you set `enabled=false`, data is on
`emptyDir` and is wiped on reschedule. Confirm the PVC is Bound and the pod
mounts it (`kubectl describe pod …-collector`). Postgres does not need this PVC.

**Events missing while the collector was down**
Sensors spool failed batches in memory (`GOODMAN_SPOOL_EVENTS`, default 50k)
and retry on the next flush. If the outage outlasts that budget,
`goodman_sensor_spool_dropped_total` increments and oldest events are gone.
Channel drops (`goodman_sensor_events_dropped_total`) are a different path —
ring-buffer pressure, not collector reachability.

**Dashboard shows "connecting" and no data**
The SSE stream connects on first event. If it stays "connecting", confirm the
collector is reachable (`curl http://<collector>:8844/v1/healthz`) and that events
are being ingested (`goodman_collector_events_ingested_total`).

**Dashboard source changed but the embedded UI did not**
Run `make dashboard`. The collector serves `internal/api/ui/dist`, not
`dashboard/src`. A common agent mistake is to edit React/CSS, run only `npm run
build`, and forget to copy the new `dist` into the embedded Go package.

**Dashboard looks right locally but CI or `go build` serves old assets**
Check `git status --short internal/api/ui/dist dashboard/src`. The hashed Vite
asset filenames should change when the source bundle changes, and the old hashed
assets should be removed. Commit source and rebuilt embedded assets together.

**Headless browser screenshots hang**
Long-lived `/v1/stream` SSE connections can keep simple `chromium --screenshot`
commands from exiting in some environments. Use Chrome DevTools Protocol or
another bounded browser automation path: wait until DOM text from mock `/v1/*`
data appears, capture the screenshot, then close the browser. If visual checks
are impossible, still run `make dashboard` and assert rendered DOM text from a
served production bundle.

## E2E harness

**`sudo make e2e` fails because a port is in use**
The live e2e harness chooses free ports dynamically by default. To pin ports for
debugging or to avoid a known conflict, set:

```bash
GOODMAN_E2E_COLLECTOR_PORT=8844 \
GOODMAN_E2E_WORKLOAD_PORT=3000 \
GOODMAN_E2E_SINK_PORT=9999 \
sudo make e2e
```

**Sensor never watches the Node workload**
Check `/tmp/goodman-e2e-sensor.log` for `watching pid ...`. Node can report its
comm as `node`, `nodejs`, or `MainThread` depending on distro/runtime. Keep all
of those covered if you change comm filtering.

**Need to keep e2e logs after a passing run**
Set `GOODMAN_E2E_KEEP_LOGS=1` before running. The harness preserves
`/tmp/goodman-e2e-*.log`, perf maps, and temporary state on failure by default,
but this flag is useful when collecting evidence across reruns.

## Kubernetes

**Sensor pods CrashLoopBackOff**
`kubectl logs ds/goodman-sensor`. Usually the node kernel lacks BTF or the pod
isn't actually privileged (a PodSecurity policy blocking it). Confirm the node
kernel with `make doctor` on the node, and that privileged pods are allowed.

**Still stuck?** Open an issue with your `make doctor` output, `uname -r`, kernel
config lines, and the relevant sensor/collector logs. See
[CONTRIBUTING.md](../CONTRIBUTING.md) and [SECURITY.md](../SECURITY.md) (for
anything that might be a vulnerability, report privately, don't open a public issue).
