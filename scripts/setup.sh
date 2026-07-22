#!/usr/bin/env bash
# Goodman one-shot setup for Debian/Ubuntu hosts. Installs the build and
# runtime prerequisites, then runs the preflight check. Idempotent: skips
# anything already present. Everything that touches the system uses sudo and
# is echoed before running.
#
#   ./scripts/setup.sh            # install missing prerequisites
#   ./scripts/setup.sh --check    # only run the preflight, install nothing
#   ./scripts/setup.sh --with-openclaw  # also install a supported Node runtime
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/runtime-versions.sh

WITH_OPENCLAW=0
CHECK_ONLY=0
while [[ $# -gt 0 ]]; do
	case "$1" in
		--with-openclaw) WITH_OPENCLAW=1; shift ;;
		--check) CHECK_ONLY=1; shift ;;
		-h|--help)
			echo "Usage: scripts/setup.sh [--check] [--with-openclaw]"
			exit 0
			;;
		*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
done

have() { command -v "$1" >/dev/null 2>&1; }
run()  { echo "+ $*"; "$@"; }

if [[ "$CHECK_ONLY" -eq 1 ]]; then
  exec bash scripts/preflight.sh
fi

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "This installer targets Linux. On macOS/Windows, use a Linux VM." >&2
  exit 1
fi
if ! have apt-get; then
  echo "This installer supports Debian/Ubuntu (apt). For other distros install:" >&2
  echo "  clang llvm libelf-dev libbpf-dev linux-headers bpftool go node" >&2
  echo "then run: make doctor" >&2
  exit 1
fi

echo "== Installing build prerequisites (clang/llvm/bpftool/headers) =="
run sudo apt-get update -y
run sudo apt-get install -y --no-install-recommends \
	clang llvm libelf-dev libbpf-dev build-essential pkg-config bpftool \
	ca-certificates curl git python3 xz-utils
KERNEL_HEADERS="linux-headers-$(uname -r)"
if apt-cache show "$KERNEL_HEADERS" >/dev/null 2>&1; then
	run sudo apt-get install -y --no-install-recommends "$KERNEL_HEADERS"
else
	echo "  Kernel headers package $KERNEL_HEADERS is unavailable; CO-RE uses committed vmlinux.h."
fi

if ! goodman_go_is_supported; then
	if have go; then
		echo "== Upgrading unsupported Go runtime ($(go version)) =="
	else
		echo "== Installing Go 1.25 =="
	fi
	GO_VER=1.25.12
	case "$(uname -m)" in
		x86_64|amd64) GO_ARCH=amd64 ;;
		aarch64|arm64) GO_ARCH=arm64 ;;
		*) echo "Unsupported Go installer architecture: $(uname -m)" >&2; exit 1 ;;
	esac
	TMP="$(mktemp -d)"
	run curl -fsSL -o "$TMP/go.tgz" "https://go.dev/dl/go${GO_VER}.linux-${GO_ARCH}.tar.gz"
  run sudo rm -rf /usr/local/go
  run sudo tar -C /usr/local -xzf "$TMP/go.tgz"
  rm -rf "$TMP"
  echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh >/dev/null
  export PATH=$PATH:/usr/local/go/bin
  echo "  Go installed. Open a new shell or: export PATH=\$PATH:/usr/local/go/bin"
else
  echo "== Go already present: $(go version) =="
fi

if [[ "$WITH_OPENCLAW" -eq 1 ]] && ! goodman_node_supports_openclaw; then
	echo "== Installing Node.js 22.22.3 for OpenClaw =="
	NODE_VER=22.22.3
	case "$(uname -m)" in
		x86_64|amd64) NODE_ARCH=x64 ;;
		aarch64|arm64) NODE_ARCH=arm64 ;;
		*) echo "Unsupported Node installer architecture: $(uname -m)" >&2; exit 1 ;;
	esac
	NODE_DIST="node-v${NODE_VER}-linux-${NODE_ARCH}"
	TMP="$(mktemp -d)"
	run curl -fsSL -o "$TMP/$NODE_DIST.tar.xz" "https://nodejs.org/dist/v${NODE_VER}/$NODE_DIST.tar.xz"
	run curl -fsSL -o "$TMP/SHASUMS256.txt" "https://nodejs.org/dist/v${NODE_VER}/SHASUMS256.txt"
	(
		cd "$TMP"
		grep "  $NODE_DIST.tar.xz\$" SHASUMS256.txt | sha256sum -c -
	)
	run sudo mkdir -p /usr/local/lib/nodejs
	run sudo tar -xJf "$TMP/$NODE_DIST.tar.xz" -C /usr/local/lib/nodejs
	for binary in node npm npx corepack; do
		run sudo ln -sfn "/usr/local/lib/nodejs/$NODE_DIST/bin/$binary" "/usr/local/bin/$binary"
	done
	rm -rf "$TMP"
fi

if ! have node; then
  echo "== Installing Node.js 20 (only needed to rebuild the dashboard) =="
  run bash -c 'curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -'
  run sudo apt-get install -y nodejs
else
  echo "== Node already present: $(node --version) =="
fi

echo
echo "== Preflight =="
bash scripts/preflight.sh

echo
echo "Setup complete. Next:"
echo "  make build && make test && make smoke     # no root needed"
echo "  sudo make e2e                             # live eBPF drift demo"
