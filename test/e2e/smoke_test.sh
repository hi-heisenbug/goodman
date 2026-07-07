#!/usr/bin/env bash
# Backend end-to-end WITHOUT eBPF/root. Starts the collector, drives synthetic
# attributed events (baseline -> benign bump -> drift), and asserts exactly one
# CRITICAL alert naming good-pkg 1.0.0 -> 1.0.1 with the two drift behaviors and
# no baseline leakage. Exits non-zero on any failure. Safe to run in CI.
set -euo pipefail
cd "$(dirname "$0")/../.."

DB="$(mktemp -u /tmp/goodman-smoke-XXXX.db)"
PORT=8846
BASE="http://127.0.0.1:$PORT"
LOG="$(mktemp /tmp/goodman-smoke-log-XXXX)"

cleanup() { kill "${COLL_PID:-}" 2>/dev/null || true; rm -f "$DB" "$DB"-* "$LOG"; }
trap cleanup EXIT

echo "== starting collector (learning window: 10 obs / 1ns) =="
GOODMAN_DSN="$DB" GOODMAN_LEARN_OBS=10 GOODMAN_LEARN_MIN_AGE=1ns GOODMAN_LISTEN=":$PORT" \
  ./bin/collector >"$LOG" 2>&1 &
COLL_PID=$!

for i in $(seq 1 50); do
  curl -sf "$BASE/v1/healthz" >/dev/null 2>&1 && break
  sleep 0.1
done

bash test/synth_driver.sh "$BASE" >/dev/null

ALERTS="$(curl -sf "$BASE/v1/alerts?status=open")"
echo "alerts: $ALERTS"

fail() { echo "FAIL: $1"; echo "--- collector log ---"; cat "$LOG"; exit 1; }

python3 - "$ALERTS" <<'PY' || fail "assertions failed"
import json, sys
alerts = json.loads(sys.argv[1])
assert len(alerts) == 1, f"expected 1 alert, got {len(alerts)}"
a = alerts[0]
assert a["severity"] == "CRITICAL", a["severity"]
assert a["package"] == "good-pkg", a["package"]
assert a["old_version"] == "1.0.0" and a["new_version"] == "1.0.1", (a["old_version"], a["new_version"])
nb = set(a["new_behaviors"])
want = {"READ /tmp/goodman-fake-secrets/credentials", "CONNECT 169.254.169.254:80"}
assert nb == want, f"new_behaviors mismatch: {nb}"
# baseline behaviors must NOT leak into the alert
assert "CONNECT 10.0.0.5:5432" not in nb
print("OK: exactly one CRITICAL alert with correct drift and no baseline leakage")
PY

echo "== SMOKE TEST PASSED =="
