# OpenClaw integration

Goodman can watch an OpenClaw Gateway as a Node workload, resolve npm package
frames, and identify versioned ClawHub skill code when a Node stack enters that
skill's installed directory.

## Check the integration without changing the host

Run this from a Goodman checkout:

```bash
scripts/integrate-openclaw.sh --dry-run
```

Dry-run never installs OpenClaw or contacts a registry. With
`--install-openclaw`, it shows the exact managed-prefix installation it would
perform. It also reports whether the `openclaw` CLI and a running Gateway are
present, shows the files it would write, and prints the collector, sensor,
Gateway, and complete-export commands.

## Integrate a Linux host

Build Goodman, then run one command:

```bash
make build
scripts/integrate-openclaw.sh
```

The script writes:

- `~/.config/goodman/openclaw.env`, with the collector URL, stable
  `GOODMAN_SERVICE=openclaw` label, token environment names, and Node flags
- `~/.local/bin/openclaw-goodman`, a launcher that adds the Tier-1 V8 perf-map
  flags without duplicating existing `NODE_OPTIONS`

The script prints the exact commands for the current configuration. The normal
local sequence is:

```bash
# terminal 1: collector
source ~/.config/goodman/openclaw.env
GOODMAN_DSN=goodman.db ./bin/collector -listen :8844

# terminal 2: sensor
source ~/.config/goodman/openclaw.env
sudo --preserve-env=GOODMAN_INGEST_TOKEN \
  ./bin/sensor -collector "$GOODMAN_COLLECTOR_URL"

# terminal 3: OpenClaw Gateway
~/.local/bin/openclaw-goodman gateway --port 18789 --verbose
```

To install OpenClaw into a user-local Goodman prefix and configure its user
systemd unit in the same run:

```bash
scripts/integrate-openclaw.sh \
  --install-openclaw --systemd-user --restart
```

The systemd path asks OpenClaw to create the Gateway unit when it is missing,
merges the Tier-1 flags into an existing literal `NODE_OPTIONS`, writes a
drop-in, reloads the user manager, and optionally restarts the service. An
existing process cannot gain new environment variables without a restart.

Set tokens in the shell when the collector requires auth:

```bash
export GOODMAN_INGEST_TOKEN='<sensor token>'
export GOODMAN_API_TOKEN='<operator token>'
```

The generated file references those variables but never writes their values.

Useful overrides:

```bash
scripts/integrate-openclaw.sh \
  --collector https://goodman.example.com \
  --service openclaw-prod
```

## Kubernetes

Patch OpenClaw Deployments with the Node flags and stable service label:

```bash
scripts/integrate-openclaw.sh \
  --k8s --namespace agents --selector app=openclaw
```

Use `--all` instead of `--selector` to patch every Deployment in the namespace.
Add `--dry-run` to print the planned changes without applying them. The helper
preserves existing literal `NODE_OPTIONS` values, adds each Goodman flag only
once, and refuses to replace a `valueFrom` reference. Kubernetes rolls only the
changed pod templates.

## What Goodman can attribute

OpenClaw itself ships as the `openclaw` npm package and runs its Gateway on
Node. Goodman follows V8 perf-map frames into `node_modules`, then reads the
owning `package.json` through `/proc/<pid>/root`.

Current ClawHub releases install skills into directories rather than npm
package directories. Goodman accepts a skill identity only when both records
exist and agree:

```text
<workspace>/skills/<skill>/.clawhub/origin.json
<workspace>/.clawhub/lock.json
```

When a captured Node stack contains JavaScript from that skill directory,
Goodman requires one of ClawHub's accepted card markers (`SKILL.md`, `skill.md`,
`skills.md`, or `SKILL.MD`) plus matching slug, owner, version, install
timestamp, registry, source, and optional artifact provenance in the origin and
workspace lock. It then reports the owner-qualified identity, for
example `@acme/calendar-sync@1.2.3`. Origin-only, lock-only, mismatched, local,
or malformed installs do not receive a made-up version or package name.

Many skills contain instructions that ask OpenClaw to run an external binary.
Those calls may have no JavaScript frame from the skill directory. Goodman then
reports the responsible npm/PyPI frame, `<app>`, or `<unknown>` according to the
normal attribution rules. It does not infer a skill name from a directory or a
prompt.

The integration sets `GOODMAN_SERVICE` in the OpenClaw process so local hosts
use a stable `openclaw` service label instead of a working-directory basename.
The sensor still reads events through its existing non-blocking path.

Goodman's built-in watch list includes Linux comm values `node`, `nodejs`,
`MainThread`, and the current OpenClaw Gateway value `openclaw-gatewa`
(`openclaw-gateway` truncated to Linux's 15 visible bytes). If a future release
reports another value, verify `/proc/<pid>/comm` and pass the exact value
through the sensor's `-comms` flag or `GOODMAN_EXTRA_COMMS`.

Verify the complete runtime contract on a real Linux kernel:

```bash
make build
sudo make e2e-openclaw
```

Or run the OpenClaw proof together with the original drift/LSM proof in a
disposable privileged container on a Linux host:

```bash
make docker-e2e
```

## No-root demo

The product demo includes a fictional OpenClaw service and ClawHub skill:

```bash
bash scripts/setup-everything.sh demo --check
bash scripts/setup-everything.sh demo
```

Open `http://127.0.0.1:8844/#alerts` and find:

```text
service=openclaw
package=@goodman-demo/calendar-sync
version=1.2.3
READ /home/openclaw/.openclaw/credentials.json
CONNECT 203.0.113.77:443
```

The package is fictional. The scenario exercises the real collector, store,
fingerprint, diff, alert, API, and dashboard path without installing OpenClaw
or loading eBPF. The Mini-Shai-Hulud replay still runs after the demo delay.

## Consume Goodman without the dashboard

Fetch one versioned JSON document containing every alert state, all
fingerprints, stored reachability reports, coverage, and enforcement state:

```bash
source ~/.config/goodman/openclaw.env
./bin/goodmanctl export -o goodman-export.json
```

The HTTP equivalent is:

```bash
curl -fsS \
  -H "Authorization: Bearer $GOODMAN_API_TOKEN" \
  "$GOODMAN_COLLECTOR_URL/v1/export"
```

The schema identifier is `goodman.export/v1`. Raw attributed events are not
persisted; consume `/v1/stream` for best-effort live delivery or configure alert
webhooks for retrying push delivery. See [API reference](api.md).
