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

**`attach sys_enter_openat: no such file or directory`**
The syscall tracepoints aren't available — the kernel was built without
`CONFIG_FTRACE`/tracepoints, or you're in a restricted container. Run the sensor
on the host (or a privileged DaemonSet with `hostPID`).

## No events / no attribution

**`goodmanctl tail` shows nothing**
1. Is the sensor watching the process? It only traces `node`/`python3` (and
   `-comms` extras). Check `goodman_sensor_watched_pids` > 0.
2. Is the process actually making `openat`/`connect`/`execve` calls? Drive traffic.
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

**Dashboard shows "connecting" and no data**
The SSE stream connects on first event. If it stays "connecting", confirm the
collector is reachable (`curl http://<collector>:8844/v1/healthz`) and that events
are being ingested (`goodman_collector_events_ingested_total`).

## Kubernetes

**Sensor pods CrashLoopBackOff**
`kubectl logs ds/goodman-sensor`. Usually the node kernel lacks BTF or the pod
isn't actually privileged (a PodSecurity policy blocking it). Confirm the node
kernel with `make doctor` on the node, and that privileged pods are allowed.

**Still stuck?** Open an issue with your `make doctor` output, `uname -r`, kernel
config lines, and the relevant sensor/collector logs. See
[CONTRIBUTING.md](../CONTRIBUTING.md) and [SECURITY.md](../SECURITY.md) (for
anything that might be a vulnerability, report privately, don't open a public issue).
