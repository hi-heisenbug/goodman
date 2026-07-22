#!/usr/bin/env bash
# One entrypoint for a portable product demo or the full Linux live proof.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/runtime-versions.sh

COMMAND=demo
if [[ $# -gt 0 && "$1" != -* ]]; then
	COMMAND="$1"
	shift
fi
CHECK=0
INSTALL=0
INSTALL_OPENCLAW=0
SYSTEMD_USER=0
DRY_RUN=0
BACKEND=auto
LIVE_BACKEND=auto
PORT="${GOODMAN_DEMO_PORT:-8844}"
PREREQUISITES_READY=0

usage() {
	cat <<'EOF'
Usage: scripts/setup-everything.sh [demo|live|all] [options]

Commands:
  demo   Portable no-root demo. Uses local Go or Docker automatically.
  live   Linux-only real eBPF proofs, including the OpenClaw runtime contract.
  all    Run the portable verification and both live eBPF proofs.

Options:
  --check              Verify and exit instead of serving the demo
  --install            Install missing Debian/Ubuntu build prerequisites
  --install-openclaw   Install openclaw@latest under ~/.local/share/goodman
  --systemd-user       Configure and restart openclaw-gateway.service
  --backend MODE       auto, local, or docker (demo only)
  --live-backend MODE  auto, host, or docker (live/all only)
  --port PORT          Dashboard port (default: 8844)
  --dry-run            Print every action without changing the machine
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
		--check) CHECK=1; shift ;;
		--install) INSTALL=1; shift ;;
		--install-openclaw) INSTALL_OPENCLAW=1; shift ;;
		--systemd-user) SYSTEMD_USER=1; shift ;;
		--backend) need_value "$@"; BACKEND="$2"; shift 2 ;;
		--live-backend) need_value "$@"; LIVE_BACKEND="$2"; shift 2 ;;
		--port) need_value "$@"; PORT="$2"; shift 2 ;;
		--dry-run) DRY_RUN=1; shift ;;
		-h|--help) usage; exit 0 ;;
		*) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
	esac
done

case "$COMMAND" in demo|live|all) ;; *) echo "unknown command: $COMMAND" >&2; usage >&2; exit 2 ;; esac
case "$BACKEND" in auto|local|docker) ;; *) echo "backend must be auto, local, or docker" >&2; exit 2 ;; esac
case "$LIVE_BACKEND" in auto|host|docker) ;; *) echo "live backend must be auto, host, or docker" >&2; exit 2 ;; esac
if [[ ! "$PORT" =~ ^[0-9]{1,5}$ ]] || ((10#$PORT < 1 || 10#$PORT > 65535)); then
	echo "port must be an integer between 1 and 65535" >&2
	exit 2
fi

run() {
	printf '+ '
	printf '%q ' "$@"
	printf '\n'
	[[ "$DRY_RUN" -eq 1 ]] || "$@"
}

run_root() {
	if [[ "$(id -u)" -eq 0 ]]; then
		run "$@"
		return
	fi
	run sudo "$@"
}

choose_demo_backend() {
	if [[ "$BACKEND" != auto ]]; then
		printf '%s' "$BACKEND"
		return
	fi
	if goodman_go_is_supported; then
		printf 'local'
		return
	fi
	if command -v docker >/dev/null 2>&1; then
		printf 'docker'
		return
	fi
	printf 'local'
}

choose_live_backend() {
	if [[ "$LIVE_BACKEND" != auto ]]; then
		printf '%s' "$LIVE_BACKEND"
		return
	fi
	if [[ "$INSTALL" -eq 1 || "$INSTALL_OPENCLAW" -eq 1 || "$SYSTEMD_USER" -eq 1 ]]; then
		printf 'host'
		return
	fi
	if [[ "$(id -u)" -eq 0 ]] || sudo -n true >/dev/null 2>&1; then
		printf 'host'
		return
	fi
	if [[ -t 0 && -t 1 ]] && command -v sudo >/dev/null 2>&1; then
		printf 'host'
		return
	fi
	if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
		printf 'docker'
		return
	fi
	printf 'host'
}

install_prerequisites() {
	[[ "$INSTALL" -eq 1 ]] || return 0
	[[ "$PREREQUISITES_READY" -eq 0 ]] || return 0
	local setup=(bash scripts/setup.sh)
	[[ "$INSTALL_OPENCLAW" -eq 1 ]] && setup+=(--with-openclaw)
	run "${setup[@]}"
	if [[ "$DRY_RUN" -eq 0 && -d /usr/local/go/bin ]]; then
		export PATH="/usr/local/go/bin:$PATH"
		hash -r
	fi
	PREREQUISITES_READY=1
}

portable_demo() {
	echo "== portable demo =="
	install_prerequisites
	local backend
	backend="$(choose_demo_backend)"
	if [[ "$backend" == local ]]; then
		if [[ "$DRY_RUN" -eq 0 ]] && ! goodman_go_is_supported; then
			echo "Go 1.25+ is missing. Re-run with --install on Debian/Ubuntu or use --backend docker." >&2
			exit 2
		fi
		run mkdir -p bin
		run go build -o bin/collector ./cmd/collector
		run go build -o bin/goodman-demo ./cmd/goodman-demo
		if [[ "$CHECK" -eq 1 || "$COMMAND" == all ]]; then
			run ./bin/goodman-demo -check -port "$PORT"
			return
		fi
		if [[ "$DRY_RUN" -eq 1 ]]; then
			echo "+ ./bin/goodman-demo -host 0.0.0.0 -port $PORT"
			return
		fi
		exec ./bin/goodman-demo -host 0.0.0.0 -port "$PORT"
	fi

	if [[ "$DRY_RUN" -eq 0 ]]; then
		command -v docker >/dev/null 2>&1 || { echo "Docker is required for --backend docker" >&2; exit 2; }
		docker info >/dev/null
	fi
	run docker build -f deploy/docker/demo.Dockerfile -t goodman/demo:local .
	if [[ "$CHECK" -eq 1 || "$COMMAND" == all ]]; then
		run docker run --rm goodman/demo:local -check -port "$PORT"
		return
	fi
	run docker run --rm -p "$PORT:8844" goodman/demo:local -port 8844
}

live_demo() {
	echo "== live Linux eBPF + OpenClaw contract e2e =="
	if [[ "$(uname -s)" != Linux ]]; then
		echo "Live eBPF requires Linux. Run the portable demo here and use a Linux VM/host for 'live'." >&2
		exit 2
	fi
	local backend
	backend="$(choose_live_backend)"
	if [[ "$backend" == docker ]]; then
		if [[ "$INSTALL" -eq 1 || "$INSTALL_OPENCLAW" -eq 1 || "$SYSTEMD_USER" -eq 1 ]]; then
			echo "--live-backend docker cannot install or modify host OpenClaw; use --live-backend host" >&2
			exit 2
		fi
		if [[ "$DRY_RUN" -eq 0 ]]; then
			command -v docker >/dev/null 2>&1 || { echo "Docker is required for --live-backend docker" >&2; exit 2; }
			docker info >/dev/null
		fi
		run docker build -f deploy/docker/e2e.Dockerfile -t goodman/e2e:local .
		run docker run --rm --privileged --pid=host --cgroupns=host \
			--security-opt seccomp=unconfined --ulimit memlock=-1:-1 \
			-v /sys/fs/cgroup:/sys/fs/cgroup:rw \
			-v /sys/kernel/tracing:/sys/kernel/tracing:rw \
			-v /sys/kernel/debug:/sys/kernel/debug:rw \
			-v /sys/kernel/security:/sys/kernel/security:rw \
			goodman/e2e:local all
		return
	fi
	install_prerequisites
	run make build workload
	local integrate=(bash scripts/integrate-openclaw.sh)
	[[ "$INSTALL_OPENCLAW" -eq 1 ]] && integrate+=(--install-openclaw)
	[[ "$SYSTEMD_USER" -eq 1 ]] && integrate+=(--systemd-user --restart)
	run "${integrate[@]}"
	if [[ "$DRY_RUN" -eq 1 ]]; then
		echo "+ sudo env GOODMAN_E2E_SKIP_BUILD=1 bash test/e2e/openclaw_test.sh"
		echo "+ sudo env GOODMAN_E2E_SKIP_BUILD=1 bash test/e2e/drift_test.sh"
		return
	fi
	run_root env GOODMAN_E2E_SKIP_BUILD=1 bash test/e2e/openclaw_test.sh
	run_root env GOODMAN_E2E_SKIP_BUILD=1 bash test/e2e/drift_test.sh
}

case "$COMMAND" in
	demo) portable_demo ;;
	live) live_demo ;;
	all)
		CHECK=1
		portable_demo
		live_demo
		;;
esac
