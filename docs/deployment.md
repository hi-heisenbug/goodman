# Deployment (Kubernetes)

Goodman deploys as a privileged **DaemonSet** (the sensor, one per node) plus a
**Deployment** (the collector + dashboard). The promise is one Helm command.

## Prerequisites

- A Kubernetes cluster whose nodes run **Linux x86-64, kernel ≥ 5.8 with BTF**.
- Nodes must allow privileged pods (the sensor loads eBPF).
- `helm` ≥ 3.

> **Local testing:** use `kind` **on a Linux host** — kind nodes share the host
> kernel, which is what eBPF needs. `make kind-e2e` automates the whole
> build → load → install → drift-test loop. Docker Desktop on macOS runs a
> LinuxKit VM without the headers/BTF Goodman needs — do not develop there.

## Install

Fast path from a checkout:

```bash
scripts/install-k8s.sh --cluster prod
```

That command installs Goodman into the current `kubectl` context using the
release images, creates the namespace if needed, waits for readiness, and prints
the dashboard and workload-attribution commands.

Equivalent raw Helm command:

```bash
helm install goodman deploy/helm/goodman \
  --set cluster=prod \
  --set-string registries='npm\,pypi' \
  --set collector.image=ghcr.io/hi-heisenbug/collector:0.1.0 \
  --set sensor.image=ghcr.io/hi-heisenbug/sensor:0.1.0
```

Then open the dashboard:

```bash
kubectl port-forward svc/goodman-collector 8844:8844
# open http://localhost:8844
```

For a production datastore, point at Postgres instead of the default embedded
SQLite:

```bash
helm install goodman deploy/helm/goodman \
  --set cluster=prod \
  --set collector.image=ghcr.io/hi-heisenbug/collector:0.1.0 \
  --set sensor.image=ghcr.io/hi-heisenbug/sensor:0.1.0 \
  --set postgres.dsn='postgres://goodman:secret@db:5432/goodman?sslmode=require'
```

See [configuration.md](configuration.md#helm-values) for every value.

## Enabling Tier-1 attribution on your workloads

Tier-1 attribution needs each watched Node process started with a profiling flag.
This is **not a code change** — it's one environment variable on your app's
Deployment:

```yaml
env:
  - name: NODE_OPTIONS
    value: "--perf-basic-prof --interpreted-frames-native-stack"
```

Patch selected Deployments:

```bash
scripts/enable-node-attribution.sh --namespace checkout --selector app=api
```

Patch every Deployment in a namespace:

```bash
scripts/enable-node-attribution.sh --namespace checkout --all
```

The Helm install NOTES also print the raw `kubectl set env` commands (toggle
with `attribution.requireNodeOptions`). Automatic injection via a
`MutatingAdmissionWebhook` is a v1.1 item (architected, not yet built); Tier-2
in-kernel V8 unwinding removes the flag entirely.

## What the chart creates

| Resource | Purpose |
|---|---|
| DaemonSet `goodman-sensor` | privileged, `hostPID`, mounts host `/proc`, `/sys`, `/sys/fs/bpf`; tolerates all taints so it runs on every node |
| Deployment `goodman-collector` | the collector + embedded dashboard, with readiness/liveness on `/v1/healthz` |
| Service `goodman-collector` | ClusterIP (configurable) on port 8844 |
| ServiceAccount + ClusterRole + Binding | lets the sensor read pod/node metadata for service attribution |
| ConfigMap `goodman-config` | cluster, registries, learning window, optional rules JSON |

The sensor resolves target files under `/host/proc/<pid>/root/…` and perf maps
under `/host/proc/<pid>/root/tmp/perf-<pid>.map`, which sees each target's own
`/tmp` regardless of mount namespaces.

## Building and publishing images

Release images are built and pushed by the `Images` GitHub Actions workflow
when a `vX.Y.Z` tag is pushed. Maintainers do not need local GHCR credentials
for normal releases.

For local validation only:

```bash
make docker REGISTRY=goodman TAG=dev
```

The sensor image rebuilds the eBPF object from source inside the build stage, so
images are reproducible from a clean checkout. Both final images are distroless
and small (collector ~19 MB, sensor ~12 MB).

## Security posture

- **API authentication is on by default.** The chart creates a
  `<release>-auth` Secret with random `ingest-token` and `api-token` values on
  first install (stable across upgrades). Sensors present the ingest token on
  every batch; the dashboard, `goodmanctl`, and API clients need the API
  token. Read it with:

  ```bash
  kubectl get secret goodman-auth -o jsonpath='{.data.api-token}' | base64 -d
  ```

  Bring your own tokens with `auth.ingestToken`/`auth.apiToken`, or point
  `auth.existingSecret` at a Secret you manage (keys `ingest-token`,
  `api-token`).
- **TLS.** Set `collector.tls.secretName` to a `kubernetes.io/tls` Secret
  (e.g. issued by cert-manager) and the collector serves HTTPS; the sensor
  DaemonSet automatically switches to `https://` and pins the Secret's
  `ca.crt`.
- **Alert forwarding.** Set `notifications.webhookUrl` (plus
  `notifications.format=slack` for Slack-compatible endpoints) to page on
  drift without polling the API.
- **Retention.** Set `retention` (e.g. `720h`) to prune resolved alerts and
  bound datastore growth; open and acknowledged alerts are never pruned.
- The **sensor** runs privileged because loading eBPF and reading host `/proc`
  requires it. The chart runs it as UID 0 inside a privileged DaemonSet. v2 will drop to the minimal capability set
  (`SYS_ADMIN`, `BPF`, `PERFMON`, `SYS_PTRACE`).
- The **collector** runs as non-root with all Linux capabilities dropped. The
  chart sets `fsGroup: 65532` so the SQLite `/data` volume is writable by the
  distroless non-root user.
- Behavior strings can contain file paths — treat the collector's data as
  sensitive. For data residency, run the collector **in your own cluster** so
  behavior data never leaves it; the future cross-tenant fingerprint network
  shares only hashed, path-stripped signatures. See `plan.md` §14.

## Uninstall

```bash
helm uninstall goodman
```

This removes the DaemonSet, Deployment, Service, and RBAC cleanly. With the
default SQLite store, fingerprint/alert data lives on the collector pod's
`emptyDir` and is discarded; with Postgres it persists in your database.
