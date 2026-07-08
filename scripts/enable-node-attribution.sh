#!/usr/bin/env bash
# Add the Node flags Goodman needs for Tier-1 package attribution.
set -euo pipefail

NAMESPACE="${GOODMAN_WORKLOAD_NAMESPACE:-default}"
SELECTOR=""
PATCH_ALL=0
DRY_RUN=0
NODE_OPTIONS_VALUE="${GOODMAN_NODE_OPTIONS:---perf-basic-prof --interpreted-frames-native-stack}"
RESOURCES=()

usage() {
  cat <<'EOF'
Usage: scripts/enable-node-attribution.sh [options] [deployment/name ...]

Adds Goodman's required NODE_OPTIONS value to selected Kubernetes Deployments.
This is a workload restart, because Kubernetes rolls pods after env changes.

Options:
  --namespace, -n NAME   Workload namespace (default: default)
  --selector, -l LABEL   Patch Deployments matching a label selector
  --all                  Patch every Deployment in the namespace
  --dry-run              Print the kubectl command without applying it
  -h, --help             Show this help

Examples:
  scripts/enable-node-attribution.sh -n checkout -l app=api
  scripts/enable-node-attribution.sh -n checkout deployment/web deployment/worker
  scripts/enable-node-attribution.sh -n staging --all
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace|-n) NAMESPACE="$2"; shift 2 ;;
    --selector|-l) SELECTOR="$2"; shift 2 ;;
    --all) PATCH_ALL=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    *) RESOURCES+=("$1"); shift ;;
  esac
done

TARGET_COUNT=0
[[ "$PATCH_ALL" -eq 1 ]] && TARGET_COUNT=$((TARGET_COUNT + 1))
[[ -n "$SELECTOR" ]] && TARGET_COUNT=$((TARGET_COUNT + 1))
[[ "${#RESOURCES[@]}" -gt 0 ]] && TARGET_COUNT=$((TARGET_COUNT + 1))

if [[ "$TARGET_COUNT" -ne 1 ]]; then
  echo "choose exactly one target: --all, --selector, or explicit deployment/name resources" >&2
  usage >&2
  exit 2
fi

CMD=(kubectl -n "$NAMESPACE" set env)
if [[ "$PATCH_ALL" -eq 1 ]]; then
  CMD+=(deployment --all)
elif [[ -n "$SELECTOR" ]]; then
  CMD+=(deployment -l "$SELECTOR")
else
  CMD+=("${RESOURCES[@]}")
fi
CMD+=("NODE_OPTIONS=$NODE_OPTIONS_VALUE")

if [[ "$DRY_RUN" -eq 1 ]]; then
  printf '%q ' "${CMD[@]}"
  printf '\n'
  exit 0
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required." >&2
  exit 1
fi

"${CMD[@]}"

cat <<EOF

Node attribution enabled for the selected Deployment(s).
Kubernetes will roll new pods with:
  NODE_OPTIONS=${NODE_OPTIONS_VALUE}

Watch rollout:
  kubectl -n ${NAMESPACE} rollout status deployment --selector='<same selector>'
EOF
