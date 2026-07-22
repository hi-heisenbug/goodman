#!/usr/bin/env bash
# Add the Node flags Goodman needs for Tier-1 package attribution.
set -euo pipefail

NAMESPACE="${GOODMAN_WORKLOAD_NAMESPACE:-default}"
SELECTOR=""
PATCH_ALL=0
DRY_RUN=0
NODE_OPTIONS_VALUE="${GOODMAN_NODE_OPTIONS:---perf-basic-prof-only-functions --interpreted-frames-native-stack}"
SERVICE="${GOODMAN_SERVICE:-openclaw}"
RESOURCES=()
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  cat <<'EOF'
Usage: scripts/enable-node-attribution.sh [options] [deployment/name ...]

Adds Goodman's required NODE_OPTIONS value to selected Kubernetes Deployments.
This is a workload restart, because Kubernetes rolls pods after env changes.

Options:
  --namespace, -n NAME   Workload namespace (default: default)
  --selector, -l LABEL   Patch Deployments matching a label selector
  --all                  Patch every Deployment in the namespace
  --service NAME         Set GOODMAN_SERVICE while patching (default: openclaw)
  --dry-run              Print the planned patches without applying them
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
		--service) SERVICE="$2"; shift 2 ;;
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

if [[ "$DRY_RUN" -eq 1 ]]; then
	echo "Would patch selected Deployments without replacing existing NODE_OPTIONS:"
	if [[ "$PATCH_ALL" -eq 1 ]]; then
		printf '  kubectl -n %q get deployment --all -o name\n' "$NAMESPACE"
	elif [[ -n "$SELECTOR" ]]; then
		printf '  kubectl -n %q get deployment -l %q -o name\n' "$NAMESPACE" "$SELECTOR"
	else
		printf '  %q\n' "${RESOURCES[@]}"
	fi
	printf '  append NODE_OPTIONS=%q\n' "$NODE_OPTIONS_VALUE"
	printf '  set GOODMAN_SERVICE=%q\n' "$SERVICE"
	exit 0
fi

if ! command -v kubectl >/dev/null 2>&1; then
	echo "kubectl is required." >&2
	exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
	echo "python3 is required to preserve existing Deployment environment values." >&2
	exit 1
fi

TARGETS=()
if [[ "$PATCH_ALL" -eq 1 ]]; then
	mapfile -t TARGETS < <(kubectl -n "$NAMESPACE" get deployment -o name)
elif [[ -n "$SELECTOR" ]]; then
	mapfile -t TARGETS < <(kubectl -n "$NAMESPACE" get deployment -l "$SELECTOR" -o name)
else
	TARGETS=("${RESOURCES[@]}")
fi
if [[ "${#TARGETS[@]}" -eq 0 ]]; then
	echo "no Deployments matched the requested OpenClaw target" >&2
	exit 1
fi

for resource in "${TARGETS[@]}"; do
	deployment_json="$(kubectl -n "$NAMESPACE" get "$resource" -o json)"
	patch="$(python3 "$SCRIPT_DIR/merge-k8s-node-env.py" "$NODE_OPTIONS_VALUE" "$SERVICE" <<<"$deployment_json")"
	if [[ "$patch" == "[]" ]]; then
		echo "$resource already has Goodman Node attribution enabled."
		continue
	fi
	kubectl -n "$NAMESPACE" patch "$resource" --type=json -p "$patch"
done

cat <<EOF

Node attribution enabled for the selected Deployment(s).
Kubernetes will roll new pods with:
  NODE_OPTIONS=${NODE_OPTIONS_VALUE}
  GOODMAN_SERVICE=${SERVICE}

Watch rollout:
  kubectl -n ${NAMESPACE} rollout status deployment --selector='<same selector>'
EOF
