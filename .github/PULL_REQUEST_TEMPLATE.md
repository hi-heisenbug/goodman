## Summary

<!-- What does this PR do, and why? Link any related issues (e.g. Closes #123). -->

## Type of change

- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Breaking change (fix or feature that changes existing behavior)
- [ ] Documentation only
- [ ] Refactor / internal change (no user-facing behavior change)

## Testing

- [ ] `make test` passes
- [ ] `make smoke` passes
- [ ] `sudo make e2e` passes (required if this PR touches the eBPF, capture, or
      attribution path)

## Checklist

- [ ] Documentation updated for any user-facing change
- [ ] Dashboard rebuilt and built assets committed (required if `dashboard/src`
      changed)
- [ ] Wire struct layout invariant preserved — `bpf/goodman.h` `struct event`
      and `internal/model/types.go` `RawEvent` still byte-for-byte identical
      (required if the wire event changed)
