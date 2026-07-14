# Development

The dev loop, testing strategy, and how to cut a release. If you're making a
substantial change, read [`AGENTS.md`](../AGENTS.md) first — it has the repo map,
invariants, production quality bar, and common mistakes to avoid.

For first-time setup, running the dashboard, using `goodmanctl`, and installing
Goodman on Kubernetes, use [Setup and usage](setup-and-usage.md).

## The loop

```bash
make doctor                       # one-time: confirm your toolchain + kernel
make build                        # compile eBPF + all binaries
make test                         # unit tests
make smoke                        # backend end-to-end, NO root
sudo make e2e                     # full live eBPF drift replay (needs root)
```

Fast inner loop while editing Go: `make test`. While editing attribution logic
specifically, the tests run against a simulated `/proc`, so `go test
./internal/attribute/` gives you full coverage with no kernel.

## Testing strategy

Goodman is testable at three levels, in order of how much they need:

1. **Pure unit tests, no kernel** — the bulk of coverage.
   - `internal/model` — asserts the `RawEvent` wire layout (size + every field
     offset) so the C/Go structs can't silently drift.
   - `internal/attribute` — runs the full resolver against a **simulated `/proc`**
     tree (fake `maps`, `status`, perf maps, and container rootfs). You can add a
     new attribution case here without root.
   - `internal/fingerprint` + `internal/diff` — the aggregation → promotion →
     drift pipeline on an in-memory SQLite store, including the
     no-false-positive guarantee for behaviorally identical version bumps.
   - `internal/loader` — parses the embedded eBPF object (no privileges) and
     asserts the tracepoint programs, LSM programs (when present in the
     object), and maps are present and typed correctly.
   - `internal/enforce` — literal deny-verdict compilation from `action: block`
     rules (no kernel).

2. **Backend end-to-end, no root** — `make smoke`. Starts the real collector and
   drives synthetic `Attributed` events through the real store, fingerprint, and
   diff engines, then asserts exactly one CRITICAL alert with correct drift and no
   baseline leakage. This is the fastest way to validate a backend change fully.

3. **Full live e2e, needs root** — `make e2e`. The real eBPF sensor captures real
   syscalls from a real Node workload and the assertion is made against the alert
   the pipeline actually produced. This is both the demo and the regression test
   for the capture + attribution path.

CI (`.github/workflows/ci.yml`) runs levels 1–2 plus a dashboard build and a Helm
lint/template on every push and PR. Level 3 needs a privileged runner and is run
by maintainers.

## Invariants you must not break

- **Wire-struct layout.** `bpf/goodman.h` `struct event` ≡ `internal/model`
  `RawEvent`, byte for byte. `types_test.go` enforces it. Change both together;
  `DirFD` and both explicit padding fields are part of the contract.
- **Never misattribute.** Resolvers return `ok`/sentinels; prefer `<unknown>` over
  a wrong package.
- **Don't block the ring-buffer reader.** Hot-path IO goes through the buffered
  channel with drop-counting.
- **Rules over hard-coded ifs** for high-risk detection.
- **SQL runs on both Postgres and SQLite.** Add migrations as both dialect files.

See [`AGENTS.md`](../AGENTS.md) for the full rationale and step-by-step recipes.

## Working on the dashboard

```bash
cd dashboard
npm install
npm run dev      # Vite dev server on :5173, proxies /v1 to a collector on :8844
```

Run a collector separately (`./bin/collector`) so the dev server has an API to
talk to. When done:

```bash
make dashboard   # builds and copies dist/ into internal/api/ui/dist
```

The built `dist/` is **committed** so `go build` works without Node. If you
changed `dashboard/src`, rebuild and commit the new `dist/` in the same PR.

The production dashboard is the Goodman UI by Heisenbug, not a demo shell. Keep
the React views wired to `/v1/alerts`, `/v1/fingerprints`, `/v1/stream`, and the
alert action endpoints. Use local `@fontsource` DM Sans/Inter assets and the
brand palette documented in [`AGENTS.md`](../AGENTS.md#conventions); do not add
CDN fonts or hard-coded mock data.

For UI changes, verify:

```bash
make dashboard
(cd dashboard && npm audit --audit-level=moderate)
```

Then serve the built `dashboard/dist` with mock `/v1/*` responses or a real
collector and check desktop and mobile layouts. Watch specifically for mobile
horizontal overflow, clipped long package/behavior strings, stale built assets,
and SSE-related screenshot tooling hangs.

## Regenerating vmlinux.h

Only needed when targeting a different kernel:

```bash
make vmlinux     # bpftool btf dump file /sys/kernel/btf/vmlinux format c > bpf/vmlinux.h
```

`vmlinux.h` is generated and large; never hand-edit it.

## Cutting a release

Follow the checklist in [`docs/release.md`](release.md) (v0.2.0 gate). Summary:

1. Update `CHANGELOG.md` (move `[Unreleased]` items into a dated version).
2. Bump `appVersion`/`version` in `deploy/helm/goodman/Chart.yaml`.
3. Verify no-root gates + `sudo make e2e` on a real kernel (LSM kernel if
   enforcement changed).
4. Tag: `git tag vX.Y.Z && git push origin vX.Y.Z`.
5. Confirm the `Images` GitHub Actions workflow published GHCR images.
6. Create the GitHub release; attach the demo video if it changed.

The release tag is the source of truth for container publishing. Do not require
maintainers to push GHCR images from a laptop. For emergency re-publishes, run
the `Images` workflow manually with the intended image tag.
