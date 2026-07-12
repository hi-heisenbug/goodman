# Goodman Documentation

This directory is the operating manual for Goodman. Start with the path that
matches what you are trying to do.

## Install And Run

| Guide | Use it when |
|---|---|
| [Setup and usage](setup-and-usage.md) | You want the full copy-paste path for local setup, dashboard usage, CLI, Kubernetes, and validation. |
| [Getting started](getting-started.md) | You want the first local build, no-root smoke test, and real eBPF demo. |
| [Deployment](deployment.md) | You want to run Goodman on Kubernetes with Helm. |
| [Configuration](configuration.md) | You need flags, environment variables, Helm values, storage, or rule tuning. |
| [Troubleshooting](troubleshooting.md) | A build, eBPF load, attribution, dashboard, or Helm step failed. |

## Understand The System

| Guide | Use it when |
|---|---|
| [Architecture](architecture.md) | You want the component model and data flow. |
| [Attribution](attribution.md) | You need to understand how Goodman maps a syscall to `package@version`. |
| [Replay corpus](replay-corpus.md) | You want to see Goodman catch real npm supply-chain attacks (`make replay`). |
| [API reference](api.md) | You are integrating with the collector REST API, SSE stream, or metrics. |
| [Development](development.md) | You are changing code, tests, dashboard assets, Docker, or Helm. |

## Agent And Contributor Entry Points

- [AGENTS.md](../AGENTS.md) is required reading for coding agents and useful for
  humans making substantial changes. It includes repository invariants,
  production quality expectations, Goodman dashboard rules, and common mistakes
  to avoid.
- [CONTRIBUTING.md](../CONTRIBUTING.md) explains the contributor workflow.
- [SECURITY.md](../SECURITY.md) explains responsible disclosure and threat model.

## Verification Commands

```bash
make doctor
make build
make vet
make test
make smoke
make replay
```

`sudo make e2e` is the real kernel path and requires root or the right eBPF
capabilities on a Linux host.
