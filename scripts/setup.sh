#!/usr/bin/env bash
# Goodman one-shot setup for Debian/Ubuntu hosts. Installs the build and
# runtime prerequisites, then runs the preflight check. Idempotent: skips
# anything already present. Everything that touches the system uses sudo and
# is echoed before running.
#
#   ./scripts/setup.sh            # install missing prerequisites
#   ./scripts/setup.sh --check    # only run the preflight, install nothing
set -euo pipefail
cd "$(dirname "$0")/.."

CHECK_ONLY=0
[[ "${1:-}" == "--check" ]] && CHECK_ONLY=1

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
  "linux-headers-$(uname -r)" ca-certificates curl git

if ! have go; then
  echo "== Installing Go 1.23 =="
  GO_VER=1.23.4
  TMP="$(mktemp -d)"
  run curl -fsSL -o "$TMP/go.tgz" "https://go.dev/dl/go${GO_VER}.linux-amd64.tar.gz"
  run sudo rm -rf /usr/local/go
  run sudo tar -C /usr/local -xzf "$TMP/go.tgz"
  rm -rf "$TMP"
  echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh >/dev/null
  export PATH=$PATH:/usr/local/go/bin
  echo "  Go installed. Open a new shell or: export PATH=\$PATH:/usr/local/go/bin"
else
  echo "== Go already present: $(go version) =="
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
bash scripts/preflight.sh || true

echo
echo "Setup complete. Next:"
echo "  make build && make test && make smoke     # no root needed"
echo "  sudo make e2e                             # live eBPF drift demo"
