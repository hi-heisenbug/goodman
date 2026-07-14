# Attack replay corpus

> "Would Goodman have caught &lt;that npm attack&gt;?" This corpus answers with a
> runnable command.

The replay corpus reproduces real npm supply-chain attacks as benign,
self-contained fixtures and asserts that Goodman raises exactly the expected
CRITICAL alert for each. It is a regression suite, a live demo, and sales
evidence in one artifact.

The corpus also includes the 2026 Mini-Shai-Hulud behavior profile: credential
reads, cloud metadata, outbound C2, and a forked shell exec. The synthetic
replay validates rule/diff behavior; `sudo make e2e` is the live-kernel gate for
the `sched_process_fork` propagation that keeps the short-lived exec visible.

```bash
make replay
```

Each scenario runs against a fresh in-memory pipeline (store, fingerprint,
diff), so it needs no root, no kernel, and no network. The fixtures contain
**no malicious code**, only the canonical behavior strings Goodman would
observe (a file read, an outbound connect, a process exec), replayed as
attributed events.

## What each scenario proves

| Scenario | Incident | What Goodman catches | Rules |
|---|---|---|---|
| `event-stream` | event-stream / flatmap-stream (Nov 2018) | A learned-benign dependency ships a new version that reads a crypto wallet file and connects to an attacker host. | secret-read, new-outbound-connect |
| `eslint-scope` | eslint-scope (Jul 2018) | A brand-new malicious version reads `~/.npmrc` (npm token) with **no prior baseline**, caught in minute one by the always-on secret-read rule. | secret-read |
| `ua-parser-js` | ua-parser-js (Oct 2021) | A hijacked version adds process execution (a dropped miner) and download traffic on top of a clean baseline. | new-exec, new-outbound-connect |
| `node-ipc` | node-ipc / peacenotwar (Mar 2022) | Protestware adds a geo-IP lookup and file access far outside the package's own directory. | new-outbound-connect |
| `mini-shai-hulud` | Mini-Shai-Hulud behavior profile (Apr-May 2026) | Credential reads, metadata access, C2, and a forked shell helper. | secret-read, cloud-metadata, new-outbound-connect, new-exec |

The `eslint-scope` case is the important one for the product story: it has no
baseline at all, so it exercises the **always-on rule path** that closes the
baseline-poisoning gap. The other three exercise version-to-version drift
against an established baseline.

## Scenario format

Scenarios are JSON files in
[`test/replay/scenarios/`](../test/replay/scenarios). To add one, drop in a
new file and the runner discovers it automatically. Omit `baseline` to test the
no-baseline always-on path.

```jsonc
{
  "name": "example",
  "incident": "human-readable incident name + date",
  "reference": "advisory or postmortem URL",
  "summary": "one paragraph: what happened and what Goodman sees",
  "service": "web",
  "package": "the-package",
  "baseline": {                 // optional; omit for the always-on path
    "version": "1.0.0",
    "behaviors": ["READ /app/node_modules/the-package/**"]
  },
  "malicious": {
    "version": "1.0.1",
    "behaviors": ["READ /app/node_modules/the-package/**", "CONNECT 1.2.3.4:443"]
  },
  "expect": {
    "severity": "CRITICAL",
    "old_version": "1.0.0",
    "new_version": "1.0.1",
    "new_behaviors": ["CONNECT 1.2.3.4:443"],
    "matched_rules": ["new-outbound-connect"]
  }
}
```

The behaviors are Goodman's canonical form (see
[`docs/attribution.md`](attribution.md)); they are representative of the real
incident, not captured from live malware.
