# Contributing to Goodman

Thanks for your interest in improving Goodman! This guide covers everything you
need to make a change and get it merged. It is meant for human contributors; if
you are an AI agent working in this repo, start with [AGENTS.md](AGENTS.md).

## Prerequisites

- Read [docs/getting-started.md](docs/getting-started.md) to set up your
  environment.
- Run `make doctor` to verify your toolchain (Go, clang/LLVM, libbpf headers,
  Node, and kernel support). Fix anything it flags before continuing.

## Development Loop

The fast inner loop for most changes:

```sh
make build && make test && make smoke
```

- `make build` compiles the eBPF program, the sensor, the collector, and
  `goodmanctl`.
- `make test` runs the Go unit tests.
- `make smoke` runs a quick end-to-end check that does not require live eBPF.

For full end-to-end coverage against a live eBPF program you need root:

```sh
sudo make e2e
```

Run this locally before touching anything in the capture or attribution path.

## The Wire Struct Invariant (CRITICAL)

The kernel and userspace share the event layout by memory, not by
serialization. The `struct event` in `bpf/goodman.h` and the `RawEvent` type in
`internal/model/types.go` **must stay byte-for-byte identical** — same fields,
same order, same sizes, same padding.

`internal/model/types_test.go` enforces this and will fail if the layouts
diverge. If you change one, change the other in the same commit and re-run
`make test`. A silent mismatch corrupts every event and every attribution
downstream.

## Code Style

- Format all Go with `gofmt` (or `goimports`). CI rejects unformatted code.
- Wrap errors with context: `fmt.Errorf("loading program: %w", err)`.
- Keep `internal/model` **dependency-free** — it defines shared types and must
  not import other internal packages or third-party libraries.
- Prefer small, focused functions and table-driven tests.

## The Product Rule: Never Misattribute

Goodman's core promise is that when it names a package, it is right. **Prefer
`unknown` over a wrong attribution.** If a syscall cannot be confidently
attributed to a specific package@version, attribute it to `unknown` rather than
guessing. A false attribution is worse than an honest gap.

## Database Migrations

Goodman supports both Postgres and SQLite. When you add a migration you must
add **both** dialects, as a matched pair:

- `NNNN_description.postgres.sql`
- `NNNN_description.sqlite.sql`

Use the same sequence number and description for both, and keep them
semantically equivalent. A migration that exists for only one backend will
break the other.

## Commit Messages

- Use an imperative, present-tense subject: "Add drift rule for outbound DNS",
  not "Added" or "Adds".
- Keep the subject concise (roughly 50 characters) and capitalized.
- Add a body explaining the *why* when the change is non-trivial.

## Pull Request Checklist

Before you open a PR, make sure:

- [ ] `go vet ./...`, `make test`, and `make smoke` are green.
- [ ] `sudo make e2e` passes if you touched the eBPF, capture, or attribution
      path.
- [ ] The wire struct invariant is preserved if you touched the wire event.
- [ ] Documentation is updated for any user-facing change.
- [ ] If you changed anything under `dashboard/src`, the dashboard is rebuilt
      and the built assets are committed.

See [.github/PULL_REQUEST_TEMPLATE.md](.github/PULL_REQUEST_TEMPLATE.md) — it is
applied automatically.

## More

- [AGENTS.md](AGENTS.md) — conventions and guardrails; required reading for
  automated agents and useful context for humans too.
- [docs/development.md](docs/development.md) — deeper development notes,
  architecture, and the full make target reference.

By contributing you agree that your contributions are licensed under the
project's [Apache 2.0 License](LICENSE), and that you will abide by our
[Code of Conduct](CODE_OF_CONDUCT.md).
