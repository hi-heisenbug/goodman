# Devpost / judge notes

Copy blocks below into the Devpost form. Tone is intentional: this is what I
built, in my words. Codex helped; it did not invent the product.

## Project name

```
Goodman
```

## Elevator pitch (≤200 chars)

```
eBPF sensor: attribute open/connect/exec to the npm/PyPI package@version that did it, baseline it, alert on drift. Works for Node agent stacks like OpenClaw too.
```

## About the project

```markdown
## Inspiration

Supply-chain alerts that say "a Node process read your secrets" are almost
useless when that process has hundreds of dependencies. After event-stream,
eslint-scope, and the newer worm-style campaigns, I wanted the kernel answer
with a package name on it.

I build Goodman at Heisenbug. The name is mine: short, about the work. The bet
is attribution. If you can pin a syscall to `package@version`, you can learn a
baseline and notice when that package starts doing something new.

## What it does

Goodman loads CO-RE eBPF programs for `open`/`openat`/`connect`/`execve` on
watched Node and Python processes, captures the user stack, resolves it through
V8/CPython perf maps and `/proc/<pid>/maps`, and maps the deepest package frame
to a real version from `package.json` or PyPI metadata.

Then it fingerprints behavior per `(service, package, version)`, diffs against
the baseline, and fires always-on high-risk rules even while learning (so day-one
cred theft still alerts). Optional LSM block mode is fail-open and off by
default.

Wrong guess is worse than unknown. Resolvers return `<unknown>` when the stack
is unclear.

OpenClaw angle: OpenClaw is Node; ClawHub skills are npm packages. Same Tier-1
path. Demo direction is skill-level attribution (`skill-xyz@1.2.3` read the
file), one-command attach, and the `/v1/*` API for SIEMs or OpenClaw without
our dashboard.

## How we built it

Kernel C in `bpf/`, Go everywhere else. The scary invariant is the wire struct:
`bpf/goodman.h` and Go `RawEvent` must match byte-for-byte (offset tests).
Attribution lives in `internal/attribute/`. Collector does fingerprint + diff +
HTTP/SSE + embedded React UI. Deploy is Helm/Docker; store is SQLite or Postgres.

I used **Codex (GPT-5.6)** through the whole build: layout lockstep, attribution
edge cases (container root, dirfd), rules/auth/Helm surfaces, dashboard on live
`/v1/*`, and the no-root demo path (`make demo` / smoke / replay). Typical loop
was: state the invariant, take a patch, run tests, reject anything that
misattributes or blocks the ringbuf reader. I own every merge.

## Challenges we ran into

Attribution beat eBPF boilerplate. V8 JIT frames, mount namespaces, relative
paths, and "never invent a package name" took the most time. The sensor hot path
has to stay lossy-fast. LSM is easy to get wrong if you do not keep fail-open
and a separate attach path. Judges often cannot load BPF, so the no-root demo
and replay corpus had to be real, not a screenshot.

## Accomplishments that we're proud of

- syscalls that land on `package@version`, not just `comm=node`
- always-on rules during learn (closes baseline poisoning)
- replay of real npm incident behaviors
- Helm + auth + SSE dashboard that operators can actually use
- API that any client can consume without our UI

## What we learned

Stack → path → package.json looks small and is most of the product. Codex is
useful when you keep it on invariants and tests; useless when you ask it to
guess product rules. Prefer `<unknown>` over a clever wrong answer.

## What's next for Goodman

- OpenClaw / ClawHub skill attribution in the live demo
- one-command integrate onto an OpenClaw host
- lean harder on `/v1/alerts` + `/v1/stream` for SIEM / agent consumers
- pilot packaging for teams running agent stacks in Kubernetes
```

## Built with (tags)

```
eBPF, Go, Linux, CO-RE, Node.js, Python, npm, PyPI, V8, React, TypeScript,
Vite, PostgreSQL, SQLite, Kubernetes, Helm, Prometheus, Codex, GPT-5.6,
OpenClaw, REST, SSE
```

## Try it out links

| Label | URL |
|---|---|
| Repo | https://github.com/hi-heisenbug/goodman |
| Getting started | https://github.com/hi-heisenbug/goodman/blob/main/docs/getting-started.md |
| This page | https://github.com/hi-heisenbug/goodman/blob/main/docs/devpost.md |
| Video | https://vimeo.com/1211851029 |

## Video demo link

```
https://vimeo.com/1211851029
```

## Instructions for judges

```
Repo: https://github.com/hi-heisenbug/goodman
README has the Codex section and OpenClaw notes.

Fastest path (no root):

  git clone https://github.com/hi-heisenbug/goodman
  cd goodman
  make build && make test && make smoke
  make demo
  # open http://127.0.0.1:8844

Video: https://vimeo.com/1211851029
Optional live eBPF: sudo make e2e (needs BTF / CAP_BPF)

API: docs/api.md. GET /v1/alerts, /v1/stream, /v1/fingerprints

ChatGPT account UUID: 019f604e-f577-7240-9e86-7c999298ac00
```

## How Codex was used (short, for README cross-check)

See [README § Built with Codex](../README.md#built-with-codex-gpt-56). Summary:
pair programming on layout tests, attribution, diff/rules, API/Helm, dashboard,
and the no-root demo path. Human review on every change; never misattribute.
