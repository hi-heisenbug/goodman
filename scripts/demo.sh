#!/usr/bin/env bash
# Start the five-minute Goodman product wow (no root).
# Prefer: make demo  →  ./bin/goodmanctl demo
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ ! -x ./bin/goodmanctl ]]; then
  echo "bin/goodmanctl not found. Run: make build" >&2
  exit 1
fi
if [[ ! -x ./bin/collector ]]; then
  echo "bin/collector not found. Run: make build" >&2
  exit 1
fi

exec ./bin/goodmanctl demo "$@"
