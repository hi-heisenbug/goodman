#!/usr/bin/env bash
# Start a no-root Goodman product demo with realistic seeded data.
set -euo pipefail

cd "$(dirname "$0")/.."

HOST="${GOODMAN_DEMO_HOST:-127.0.0.1}"
PORT="${GOODMAN_DEMO_PORT:-8844}"
DB="${GOODMAN_DEMO_DB:-demo_build/goodman_demo.db}"
URL="http://${HOST}:${PORT}"

if [[ ! -x ./bin/collector ]]; then
  echo "bin/collector not found. Run: make build" >&2
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required for the demo data loader." >&2
  exit 1
fi

python3 - "$HOST" "$PORT" <<'PY'
import socket
import sys

host, port = sys.argv[1], int(sys.argv[2])
sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
try:
    sock.bind((host, port))
except OSError:
    print(f"{host}:{port} is already in use. Set GOODMAN_DEMO_PORT to another port.", file=sys.stderr)
    sys.exit(1)
finally:
    sock.close()
PY

rm -f "$DB" "$DB-shm" "$DB-wal"

cleanup() {
  if [[ -n "${COLLECTOR_PID:-}" ]] && kill -0 "$COLLECTOR_PID" 2>/dev/null; then
    kill "$COLLECTOR_PID" 2>/dev/null || true
    wait "$COLLECTOR_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

echo "Starting Goodman collector on ${URL}"
GOODMAN_DSN="$DB" \
GOODMAN_LEARN_OBS="${GOODMAN_DEMO_LEARN_OBS:-3}" \
GOODMAN_LEARN_MIN_AGE="${GOODMAN_DEMO_LEARN_MIN_AGE:-1s}" \
  ./bin/collector -listen "${HOST}:${PORT}" &
COLLECTOR_PID=$!

READY=0
for _ in {1..50}; do
  if python3 - "$URL" <<'PY' >/dev/null 2>&1
import sys
import urllib.request

with urllib.request.urlopen(sys.argv[1] + "/v1/healthz", timeout=0.3) as resp:
    raise SystemExit(0 if resp.status == 200 else 1)
PY
  then
    READY=1
    break
  fi
  if ! kill -0 "$COLLECTOR_PID" 2>/dev/null; then
    echo "collector exited before it became healthy" >&2
    wait "$COLLECTOR_PID"
    exit 1
  fi
  sleep 0.1
done

if [[ "$READY" -ne 1 ]]; then
  echo "collector did not become healthy at ${URL}/v1/healthz" >&2
  exit 1
fi

python3 demo_build/inject_demo.py "$URL"

cat <<EOF

Goodman demo is ready.

Dashboard:    ${URL}
Alerts:       ${URL}/#alerts
Fingerprints: ${URL}/#fingerprints

The collector is running with SQLite data at ${DB}.
Press Ctrl-C to stop and remove the demo process.
EOF

wait "$COLLECTOR_PID"
