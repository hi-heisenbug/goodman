#!/usr/bin/env bash
# Install Goodman into a Kubernetes cluster with production defaults.
set -euo pipefail

cd "$(dirname "$0")/.."

RELEASE="${GOODMAN_RELEASE:-goodman}"
NAMESPACE="${GOODMAN_NAMESPACE:-goodman-system}"
CLUSTER="${GOODMAN_CLUSTER:-prod}"
IMAGE_REGISTRY="${GOODMAN_IMAGE_REGISTRY:-ghcr.io/goodman-sec}"
TAG="${GOODMAN_IMAGE_TAG:-0.1.0}"
CHART="${GOODMAN_CHART:-deploy/helm/goodman}"
POSTGRES_DSN="${GOODMAN_POSTGRES_DSN:-}"
WAIT=1
EXTRA_ARGS=()

usage() {
  cat <<'EOF'
Usage: scripts/install-k8s.sh [options]

Installs Goodman into the current kubectl context.

Options:
  --release NAME        Helm release name (default: goodman)
  --namespace NAME      Kubernetes namespace (default: goodman-system)
  --cluster NAME        Cluster label shown in Goodman (default: prod)
  --registry REGISTRY   Image registry/repo prefix (default: ghcr.io/goodman-sec)
  --tag TAG             Image tag for sensor and collector (default: 0.1.0)
  --chart PATH_OR_OCI   Helm chart path/ref (default: deploy/helm/goodman)
  --postgres-dsn DSN    Production Postgres DSN. Empty uses SQLite pilot mode.
  --set KEY=VALUE       Extra Helm --set value; repeatable.
  --values FILE         Extra Helm values file; repeatable.
  --no-wait             Do not wait for rollout readiness.
  -h, --help            Show this help.

Examples:
  scripts/install-k8s.sh --cluster prod
  scripts/install-k8s.sh --cluster prod --postgres-dsn "$GOODMAN_POSTGRES_DSN"
  scripts/install-k8s.sh --namespace security --tag 0.1.0
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --release) RELEASE="$2"; shift 2 ;;
    --namespace|-n) NAMESPACE="$2"; shift 2 ;;
    --cluster) CLUSTER="$2"; shift 2 ;;
    --registry) IMAGE_REGISTRY="$2"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    --chart) CHART="$2"; shift 2 ;;
    --postgres-dsn) POSTGRES_DSN="$2"; shift 2 ;;
    --set) EXTRA_ARGS+=(--set "$2"); shift 2 ;;
    --values|-f) EXTRA_ARGS+=(--values "$2"); shift 2 ;;
    --no-wait) WAIT=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required. Install it and rerun this script." >&2
    exit 1
  fi
}

need kubectl
need helm

if ! kubectl version --client >/dev/null 2>&1; then
  echo "kubectl is installed but not working." >&2
  exit 1
fi
if ! kubectl cluster-info >/dev/null 2>&1; then
  echo "kubectl cannot reach a cluster. Check KUBECONFIG/current context." >&2
  exit 1
fi

if [[ ! -d "$CHART" && "$CHART" != oci://* ]]; then
  echo "chart not found: $CHART" >&2
  echo "Run from a Goodman checkout or set GOODMAN_CHART/--chart to a packaged chart." >&2
  exit 1
fi

echo "Installing Goodman into namespace ${NAMESPACE} on context $(kubectl config current-context)"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

HELM_ARGS=(
  upgrade --install "$RELEASE" "$CHART"
  --namespace "$NAMESPACE"
  --create-namespace
  --set "cluster=$CLUSTER"
  --set-string "registries=npm\,pypi"
  --set "collector.image=${IMAGE_REGISTRY}/collector:${TAG}"
  --set "sensor.image=${IMAGE_REGISTRY}/sensor:${TAG}"
)

if [[ -n "$POSTGRES_DSN" ]]; then
  HELM_ARGS+=(--set-string "postgres.dsn=$POSTGRES_DSN")
fi
if [[ "$WAIT" -eq 1 ]]; then
  HELM_ARGS+=(--wait --timeout 5m)
fi

helm "${HELM_ARGS[@]}" "${EXTRA_ARGS[@]}"

cat <<EOF

Goodman is installed.

Open the dashboard:
  kubectl -n ${NAMESPACE} port-forward svc/${RELEASE}-collector 8844:8844
  http://127.0.0.1:8844

Enable Tier-1 Node attribution on selected app workloads:
  scripts/enable-node-attribution.sh --namespace <app-namespace> --selector app=<app-label>

Or patch every Deployment in one namespace:
  scripts/enable-node-attribution.sh --namespace <app-namespace> --all

Goodman detects drift after the first stable baseline is learned. For production,
use Postgres via --postgres-dsn or GOODMAN_POSTGRES_DSN.
EOF
