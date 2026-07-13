# Performance and overhead

Security teams will not deploy a privileged DaemonSet without overhead numbers.
This page gives the measured collector throughput, the per-event costs, and how
to reproduce them, plus the sensor-side methodology and the one metric that
tells you whether Goodman is actually working.

All numbers below are reproducible with:

```bash
make bench
```

## Collector ingest throughput

`BenchmarkIngestPipeline` drives the full collector hot path per event:
fingerprint aggregation, diff-engine evaluation against the high-risk rules,
and persistence. It uses the SQLite backend (the pilot default) with WAL.

Measured on an AMD Ryzen 5 5625U (12 threads), Go 1.23, SQLite backend:

| Metric | Value |
|---|---|
| Throughput | ~16,000 events/sec (single collector) |
| Per-event cost | ~62 µs/event |
| Allocations | ~27 B and ~0.03 allocs per event (amortized over a 200-event batch) |

This is a write-heavy shape: every batch re-touches 50 distinct fingerprints,
so it is close to a worst case for the store. A single pilot collector on
SQLite comfortably absorbs the event rate of a few hundred monitored Node
processes. For higher rates or HA, point `GOODMAN_DSN` at Postgres; the
pipeline logic is identical and the bottleneck moves to the database.

Because the sensor batches and ships events asynchronously and drops (with a
counter) when its buffer is full, a momentarily slow collector never blocks or
crashes a monitored workload; it sheds load and reports it.

## Sensor per-event cost

`BenchmarkCanonicalize` measures the userspace work done for every captured
syscall before batching (path/host canonicalization, including CIDR
aggregation):

| Metric | Value |
|---|---|
| Canonicalization | ~330 ns/event, 2 allocs |

The dominant sensor cost in production is not this but **stack resolution**
(walking `/proc/<pid>/maps` and the V8 perf map). That path needs a real kernel
and a running Node workload, so it is measured with `sudo make e2e` rather than
in a unit benchmark. Guidance for a pilot:

- The eBPF programs only fire for watched runtimes (`node`, `python`, ...), so
  unrelated processes cost nothing.
- Resolution results are cached per pid; steady-state cost is a map lookup.
- The ring-buffer reader is lossy under pressure by design (see the drop
  metrics below), so the sensor cannot stall a workload.

Budget the sensor DaemonSet at the chart's default request (50m CPU / 128Mi)
and cap (500m / 512Mi); raise the cap only if `goodman_sensor_ringbuf_drops`
climbs under your real load.

## The metric that matters: attribution quality

Goodman's value depends on attributing syscalls to the right package. The live
signal for this is the sensor's attribution outcome counter:

```
goodman_sensor_attributed_total{outcome="package"}   # resolved to a package
goodman_sensor_attributed_total{outcome="app"}        # first-party app code
goodman_sensor_attributed_total{outcome="unknown"}    # could not resolve
```

**Attribution success rate** = `package / (package + app + unknown)`. Track it
as the product KPI. A high `unknown` rate usually means workloads are missing
the `NODE_OPTIONS` perf-map flag (enable the admission webhook, see
[deployment](deployment.md)) or the sensor cannot read the target mount
namespace. Goodman deliberately reports `<unknown>` rather than guess; a wrong
package name is worse than an honest unknown.

## Load-shed and completeness signals

Watch these to know whether you are seeing everything:

| Metric | Meaning |
|---|---|
| `goodman_sensor_ringbuf_drops` | events the kernel dropped because the ring buffer was full |
| `goodman_sensor_channel_drops_total` | events the sensor dropped because the send buffer was full |
| `goodman_sensor_batches_total{result="error"}` | failed collector POSTs |

Sustained non-zero drops mean events are being shed under load: raise the
sensor CPU cap or investigate the collector before trusting completeness. All
three are zero in a healthy steady state.
