# Tier-2 spike (throwaway)

Non-product sandbox for a user-space V8 pointer-chase prototype.

**Do not** wire this into `make build`, `internal/attribute/`, or `bpf/`.

Goal: given a live Node pid and one JIT instruction pointer, print the
JavaScript source path by reading `/proc/<pid>/mem` with offsets from V8
metadata — without using `/tmp/perf-<pid>.map`.

See `docs/research/tier2-attribution.md` for the PARK decision and the
commands to run when you pick this up.
