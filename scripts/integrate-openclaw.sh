#!/usr/bin/env bash
# Prepare an OpenClaw host for Goodman's Tier-1 Node attribution.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

COLLECTOR_URL="${GOODMAN_COLLECTOR_URL:-http://127.0.0.1:8844}"
SERVICE="${GOODMAN_SERVICE:-openclaw}"
CONFIG_PATH="${GOODMAN_OPENCLAW_CONFIG:-${XDG_CONFIG_HOME:-$HOME/.config}/goodman/openclaw.env}"
LAUNCHER_PATH="${GOODMAN_OPENCLAW_LAUNCHER:-$HOME/.local/bin/openclaw-goodman}"
NODE_OPTIONS_VALUE="${GOODMAN_NODE_OPTIONS:---perf-basic-prof-only-functions --interpreted-frames-native-stack}"
OPENCLAW_COMM="openclaw-gatewa"
OPENCLAW_PREFIX="${GOODMAN_OPENCLAW_PREFIX:-${XDG_DATA_HOME:-$HOME/.local/share}/goodman/openclaw}"
OPENCLAW_BIN_OVERRIDE=""
INSTALL_OPENCLAW=0
SYSTEMD_USER=0
RESTART_SYSTEMD=0
SYSTEMD_UNIT="${GOODMAN_OPENCLAW_SYSTEMD_UNIT:-openclaw-gateway.service}"
DRY_RUN=0
K8S=0
NAMESPACE="${GOODMAN_WORKLOAD_NAMESPACE:-default}"
SELECTOR=""
PATCH_ALL=0

usage() {
  cat <<'EOF'
Usage: scripts/integrate-openclaw.sh [options]

Creates a local Goodman/OpenClaw env file and an openclaw-goodman launcher.
The launcher starts OpenClaw with the V8 perf-map flags Goodman needs for
package and ClawHub skill attribution. OpenClaw is left untouched unless
--install-openclaw is supplied.

Options:
  --collector URL       Collector URL (default: http://127.0.0.1:8844)
  --service NAME        Stable Goodman service name (default: openclaw)
  --config PATH         Env snippet path (default: ~/.config/goodman/openclaw.env)
  --launcher PATH       Launcher path (default: ~/.local/bin/openclaw-goodman)
  --openclaw-bin PATH   Use this OpenClaw executable
  --install-openclaw    Install openclaw@latest into the user-local Goodman prefix
  --systemd-user        Persist the environment in the OpenClaw user service
  --restart             Restart the user service after writing the drop-in
  --unit NAME           User service unit (default: openclaw-gateway.service)
  --dry-run             Print detection, files, and commands without writing
  --k8s                 Also patch Kubernetes Deployment pod templates
  --namespace, -n NAME  Kubernetes namespace (default: default)
  --selector, -l LABEL  Patch Deployments matching this selector
  --all                 Patch every Deployment in the namespace
  -h, --help            Show this help

Examples:
  scripts/integrate-openclaw.sh
  scripts/integrate-openclaw.sh --dry-run
  scripts/integrate-openclaw.sh --install-openclaw --systemd-user --restart
  scripts/integrate-openclaw.sh --k8s -n agents -l app=openclaw
EOF
}

need_value() {
  if [[ $# -lt 2 || -z "$2" ]]; then
    echo "$1 requires a value" >&2
    exit 2
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --collector) need_value "$@"; COLLECTOR_URL="$2"; shift 2 ;;
    --service) need_value "$@"; SERVICE="$2"; shift 2 ;;
    --config) need_value "$@"; CONFIG_PATH="$2"; shift 2 ;;
		--launcher) need_value "$@"; LAUNCHER_PATH="$2"; shift 2 ;;
		--openclaw-bin) need_value "$@"; OPENCLAW_BIN_OVERRIDE="$2"; shift 2 ;;
		--install-openclaw) INSTALL_OPENCLAW=1; shift ;;
		--systemd-user) SYSTEMD_USER=1; shift ;;
		--restart) RESTART_SYSTEMD=1; shift ;;
		--unit) need_value "$@"; SYSTEMD_UNIT="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --k8s) K8S=1; shift ;;
    --namespace|-n) need_value "$@"; NAMESPACE="$2"; shift 2 ;;
    --selector|-l) need_value "$@"; SELECTOR="$2"; shift 2 ;;
    --all) PATCH_ALL=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ "$RESTART_SYSTEMD" -eq 1 && "$SYSTEMD_USER" -ne 1 ]]; then
	echo "--restart requires --systemd-user" >&2
	exit 2
fi

if [[ -z "$SERVICE" || "$SERVICE" == *$'\n'* ]]; then
  echo "service must be a non-empty single-line value" >&2
  exit 2
fi

if [[ "$K8S" -eq 1 ]]; then
  TARGET_COUNT=0
  [[ "$PATCH_ALL" -eq 1 ]] && TARGET_COUNT=$((TARGET_COUNT + 1))
  [[ -n "$SELECTOR" ]] && TARGET_COUNT=$((TARGET_COUNT + 1))
  if [[ "$TARGET_COUNT" -ne 1 ]]; then
    echo "--k8s requires exactly one target: --selector or --all" >&2
    exit 2
  fi
fi

if [[ "$INSTALL_OPENCLAW" -eq 1 && -n "$OPENCLAW_BIN_OVERRIDE" ]]; then
	echo "--install-openclaw and --openclaw-bin are mutually exclusive" >&2
	exit 2
fi

OPENCLAW_BIN="$OPENCLAW_BIN_OVERRIDE"
if [[ "$INSTALL_OPENCLAW" -eq 1 ]]; then
	OPENCLAW_BIN="$OPENCLAW_PREFIX/node_modules/.bin/openclaw"
	if [[ "$DRY_RUN" -eq 1 ]]; then
		printf 'Would install OpenClaw: npm install --prefix %q --engine-strict openclaw@latest\n' "$OPENCLAW_PREFIX"
	else
		command -v npm >/dev/null 2>&1 || { echo "npm is required for --install-openclaw" >&2; exit 1; }
		mkdir -p "$OPENCLAW_PREFIX"
		npm install --prefix "$OPENCLAW_PREFIX" --no-audit --no-fund --engine-strict openclaw@latest
	fi
elif [[ -z "$OPENCLAW_BIN" ]]; then
	OPENCLAW_BIN="$(command -v openclaw || true)"
fi
if [[ -n "$OPENCLAW_BIN" ]]; then
  echo "OpenClaw CLI: $OPENCLAW_BIN"
else
  echo "OpenClaw CLI: not found"
  echo "The integration files will still be usable after OpenClaw is installed."
  OPENCLAW_BIN="openclaw"
fi

RUNNING=0
while read -r pid comm args; do
  [[ -n "${pid:-}" && -n "${comm:-}" ]] || continue
  if [[ "$args" != *openclaw* ]]; then
    continue
  fi
  case "$comm" in
		node|nodejs|MainThread|openclaw|openclaw-gatewa) ;;
    *) continue ;;
  esac
  RUNNING=1
  echo "OpenClaw-shaped process: pid=$pid comm=$comm"
  case "$comm" in
    node|nodejs|MainThread|openclaw-gatewa)
      if [[ -r "/proc/$pid/environ" ]]; then
        proc_env="$(tr '\0' '\n' < "/proc/$pid/environ" 2>/dev/null || true)"
        if [[ "$proc_env" == *"--perf-basic-prof-only-functions"* && "$proc_env" == *"--interpreted-frames-native-stack"* ]]; then
          echo "  Tier-1 NODE_OPTIONS: ready"
        else
          echo "  Tier-1 NODE_OPTIONS: missing; restart with $LAUNCHER_PATH"
        fi
      else
        echo "  Tier-1 NODE_OPTIONS: cannot inspect /proc/$pid/environ"
      fi
      ;;
    *)
      echo "  Legacy comm detected; verify the sensor includes it through GOODMAN_EXTRA_COMMS."
      ;;
  esac
done < <(ps -eo pid=,comm=,args= 2>/dev/null || true)
if [[ "$RUNNING" -eq 0 ]]; then
  echo "Running OpenClaw process: not found"
fi

emit_config() {
  printf '# Generated by Goodman. Source this file before collector, sensor, or API commands.\n'
  printf 'export GOODMAN_COLLECTOR_URL=%q\n' "$COLLECTOR_URL"
  printf 'export GOODMAN_SERVICE=%q\n' "$SERVICE"
  printf 'export GOODMAN_API_TOKEN="${GOODMAN_API_TOKEN:-}"\n'
	printf 'export GOODMAN_INGEST_TOKEN="${GOODMAN_INGEST_TOKEN:-}"\n'
	printf 'export GOODMAN_NODE_OPTIONS=%q\n' "$NODE_OPTIONS_VALUE"
	printf 'case ",${GOODMAN_EXTRA_COMMS:-}," in\n'
	printf '  *,%s,*) ;;\n' "$OPENCLAW_COMM"
	printf '  *) export GOODMAN_EXTRA_COMMS="${GOODMAN_EXTRA_COMMS:+$GOODMAN_EXTRA_COMMS,}%s" ;;\n' "$OPENCLAW_COMM"
	printf 'esac\n'
}

emit_launcher() {
  printf '#!/usr/bin/env bash\n'
  printf 'set -euo pipefail\n'
  printf 'source %q\n' "$CONFIG_PATH"
  cat <<'EOF'
for goodman_flag in $GOODMAN_NODE_OPTIONS; do
  case " ${NODE_OPTIONS:-} " in
    *" $goodman_flag "*) ;;
    *) export NODE_OPTIONS="${NODE_OPTIONS:+$NODE_OPTIONS }$goodman_flag" ;;
  esac
done
export GOODMAN_SERVICE
EOF
  printf 'exec %q "$@"\n' "$OPENCLAW_BIN"
}

merge_node_options() {
	local merged="$1" flag
	for flag in $NODE_OPTIONS_VALUE; do
		case " $merged " in
			*" $flag "*) ;;
			*) merged="${merged:+$merged }$flag" ;;
		esac
	done
	printf '%s' "$merged"
}

systemd_node_options() {
	local environment="$1"
	python3 -c 'import shlex,sys
for item in shlex.split(sys.argv[1]):
    if item.startswith("NODE_OPTIONS="):
        print(item.split("=", 1)[1])
        break
' "$environment"
}

emit_systemd_dropin() {
	local node_options="$1"
	printf '[Service]\n'
	printf 'Environment="GOODMAN_SERVICE=%s"\n' "${SERVICE//\"/\\\"}"
	printf 'Environment="NODE_OPTIONS=%s"\n' "${node_options//\"/\\\"}"
}

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo
  echo "Would write $CONFIG_PATH:"
  emit_config
  echo
  echo "Would write $LAUNCHER_PATH:"
  emit_launcher
else
  mkdir -p "$(dirname -- "$CONFIG_PATH")" "$(dirname -- "$LAUNCHER_PATH")"
  emit_config > "$CONFIG_PATH"
  chmod 600 "$CONFIG_PATH"
  emit_launcher > "$LAUNCHER_PATH"
  chmod 755 "$LAUNCHER_PATH"
  echo "Wrote config:   $CONFIG_PATH"
	echo "Wrote launcher: $LAUNCHER_PATH"
fi

if [[ "$SYSTEMD_USER" -eq 1 ]]; then
	DROPIN_PATH="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/$SYSTEMD_UNIT.d/goodman.conf"
	if [[ "$DRY_RUN" -eq 1 ]]; then
		echo
		echo "Would preserve the service's existing NODE_OPTIONS and write $DROPIN_PATH:"
		emit_systemd_dropin "$NODE_OPTIONS_VALUE"
		echo "Would run: systemctl --user daemon-reload"
		[[ "$RESTART_SYSTEMD" -eq 1 ]] && echo "Would run: systemctl --user restart $SYSTEMD_UNIT"
	else
		command -v systemctl >/dev/null 2>&1 || { echo "systemctl is required for --systemd-user" >&2; exit 1; }
		command -v python3 >/dev/null 2>&1 || { echo "python3 is required for --systemd-user" >&2; exit 1; }
		if ! systemctl --user cat "$SYSTEMD_UNIT" >/dev/null 2>&1; then
			echo "$SYSTEMD_UNIT is not installed; asking OpenClaw to create it."
			"$OPENCLAW_BIN" gateway install
		fi
		CURRENT_ENV="$(systemctl --user show "$SYSTEMD_UNIT" --property=Environment --value 2>/dev/null || true)"
		CURRENT_NODE_OPTIONS="$(systemd_node_options "$CURRENT_ENV")"
		MERGED_NODE_OPTIONS="$(merge_node_options "$CURRENT_NODE_OPTIONS")"
		mkdir -p "$(dirname -- "$DROPIN_PATH")"
		emit_systemd_dropin "$MERGED_NODE_OPTIONS" > "$DROPIN_PATH"
		systemctl --user daemon-reload
		echo "Wrote user-service drop-in: $DROPIN_PATH"
		if [[ "$RESTART_SYSTEMD" -eq 1 ]]; then
			systemctl --user restart "$SYSTEMD_UNIT"
			echo "Restarted $SYSTEMD_UNIT"
		fi
	fi
fi

if [[ "$K8S" -eq 1 ]]; then
	NODE_PATCH=("$SCRIPT_DIR/enable-node-attribution.sh" --namespace "$NAMESPACE" --service "$SERVICE")
	if [[ "$PATCH_ALL" -eq 1 ]]; then
		NODE_PATCH+=(--all)
	else
		NODE_PATCH+=(--selector "$SELECTOR")
	fi

	if [[ "$DRY_RUN" -eq 1 ]]; then
		echo
		"${NODE_PATCH[@]}" --dry-run
	else
		"${NODE_PATCH[@]}"
	fi
fi

printf '\nOpenClaw integration commands\n\n'
printf '  # terminal 1: collector (skip when using a remote collector)\n'
printf '  cd %q\n' "$REPO_ROOT"
printf '  source %q\n' "$CONFIG_PATH"
printf '  GOODMAN_DSN=goodman.db ./bin/collector -listen :8844\n\n'
printf '  # terminal 2: sensor (root/CAP_BPF required)\n'
printf '  cd %q\n' "$REPO_ROOT"
printf '  source %q\n' "$CONFIG_PATH"
printf '  sudo --preserve-env=GOODMAN_INGEST_TOKEN ./bin/sensor -collector %q\n\n' "$COLLECTOR_URL"
printf '  # terminal 3: foreground OpenClaw gateway with Tier-1 attribution\n'
printf '  %q gateway --port 18789 --verbose\n\n' "$LAUNCHER_PATH"
printf '  # dashboard-independent JSON for OpenClaw or a SIEM\n'
printf '  cd %q\n' "$REPO_ROOT"
printf '  source %q\n' "$CONFIG_PATH"
printf '  ./bin/goodmanctl export -collector %q\n\n' "$COLLECTOR_URL"
printf 'The launcher adds:\n'
printf '  NODE_OPTIONS=%s\n' "$NODE_OPTIONS_VALUE"
printf '  GOODMAN_SERVICE=%s\n\n' "$SERVICE"
printf 'The sensor watches OpenClaw Linux comm: %s\n\n' "$OPENCLAW_COMM"
if [[ "$SYSTEMD_USER" -eq 1 ]]; then
	printf 'The user systemd drop-in is configured. Existing processes receive the new\n'
	printf 'environment only after a restart; --restart performs that step.\n'
else
	printf 'For a user systemd Gateway, rerun with --systemd-user --restart. Otherwise\n'
	printf 'start the foreground launcher above; existing environments cannot change in place.\n'
fi
