# Security Policy

Goodman is a runtime security sensor. We take the security of the project and of
the systems it observes seriously, and we appreciate the work of security
researchers who report issues responsibly.

## Supported Versions

Security fixes are provided for the latest patch release of the supported minor
series.

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |
| < 0.1   | :x:                |

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Report vulnerabilities privately by email to **security@goodman.sh**. If you
would like to encrypt your report, request our PGP key at the same address.

Please include as much of the following as you can:

- A description of the vulnerability and its impact.
- The affected component and version (see **Scope** below).
- Step-by-step instructions to reproduce, including any proof-of-concept.
- The environment: kernel version (`uname -r`), deployment mode (local or
  Kubernetes), and relevant configuration.
- Any suggested remediation, if you have one.

## Our Commitment

- **Acknowledgement:** we will acknowledge receipt of your report within
  **3 business days**.
- **Assessment:** we will investigate, confirm the issue, and share our
  assessment and a remediation plan with you.
- **Fix target:** we aim to ship a fix for confirmed vulnerabilities within
  **90 days** of the initial report. We will keep you informed of progress and
  coordinate disclosure timing with you.
- **Credit:** with your permission, we will credit you in the release notes and
  advisory once a fix is published.

## Scope

The following components are in scope:

- **The sensor** — the eBPF-backed agent that captures and attributes syscalls.
- **The eBPF program** — the in-kernel capture code (`bpf/`) and its loader.
- **The attribution pipeline** — Tier-1 perf-map attribution of syscalls to a
  package@version.
- **The collector** — the ingestion service and its REST + SSE API.

Vulnerabilities in third-party dependencies should generally be reported to the
upstream project; if a dependency issue is exploitable specifically through
Goodman's use of it, we still want to hear about it.

## A Note on Detection vs. Enforcement

Goodman **detects and alerts**; in v1 it does **not enforce or block**. It
observes syscall behavior, attributes it to the responsible package, and raises
alerts on drift from a learned baseline. It is not an inline enforcement or
sandboxing mechanism and should not be relied upon to prevent an action from
occurring. Reports should be evaluated with this detection-only threat model in
mind.
