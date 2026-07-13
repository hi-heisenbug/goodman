# Python Tier-1 spike (throwaway notes)

Non-product sandbox for CPython `PYTHONPERFSUPPORT=1` perf-map captures.

**Do not** wire this into `make build` or `internal/attribute/` until Phase 2
build is customer-gated.

See `docs/research/python-attribution.md` for the GO decision and measured
counts from a live 3.13.14 + `requests` run.
