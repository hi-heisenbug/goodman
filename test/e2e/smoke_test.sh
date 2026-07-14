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

echo "== enforcement pipeline (no kernel) =="
ENF_DB="$(mktemp -u /tmp/goodman-enf-smoke-XXXX.db)"
ENF_PORT=8847
ENF_BASE="http://127.0.0.1:$ENF_PORT"
ENF_RULES="$(mktemp /tmp/goodman-enf-rules-XXXX.json)"
cat >"$ENF_RULES" <<'JSON'
[{"name":"shadow","pattern":"^READ /etc/shadow","always_on":true,"action":"block"}]
JSON

GOODMAN_DSN="$ENF_DB" GOODMAN_LEARN_OBS=2 GOODMAN_LEARN_MIN_AGE=1ns GOODMAN_LISTEN=":$ENF_PORT" \
  GOODMAN_ENFORCE_ENABLED=true ./bin/collector -rules="$ENF_RULES" >"$LOG.enf" 2>&1 &
ENF_PID=$!
trap 'kill "${COLL_PID:-}" "${ENF_PID:-}" 2>/dev/null || true; rm -f "$DB" "$DB"-* "$ENF_DB" "$ENF_DB"-* "$LOG" "$LOG.enf" "$ENF_RULES"' EXIT

for i in $(seq 1 50); do curl -sf "$ENF_BASE/v1/healthz" >/dev/null 2>&1 && break; sleep 0.1; done

curl -sf -X POST "$ENF_BASE/v1/events" -H 'Content-Type: application/json' -d '{
  "sensor":"smoke",
  "events":[{"service":"web","package":"evil","version":"1.0.0","type":1,"behavior":"READ /etc/shadow","timestamp":100}]
}' >/dev/null

STATE="$(curl -sf "$ENF_BASE/v1/enforce/state")"
python3 - "$STATE" <<'PY' || fail "enforce state assertions"
import json, sys
s = json.loads(sys.argv[1])
assert s.get("enabled") is False
open_paths = s.get("verdicts", {}).get("web", {}).get("open", [])
assert "/etc/shadow" in open_paths, open_paths
print("OK: verdict compiled while runtime switch off")
PY

curl -sf -X POST "$ENF_BASE/v1/enforce/on" >/dev/null
STATE="$(curl -sf "$ENF_BASE/v1/enforce/state")"
python3 - "$STATE" <<'PY' || fail "enforce on"
import json, sys
s = json.loads(sys.argv[1])
assert s.get("enabled") is True
PY

curl -sf -X POST "$ENF_BASE/v1/enforce/off" >/dev/null
STATE="$(curl -sf "$ENF_BASE/v1/enforce/state")"
python3 - "$STATE" <<'PY' || fail "enforce off"
import json, sys
assert json.loads(sys.argv[1]).get("enabled") is False
PY

curl -sf -X POST "$ENF_BASE/v1/events" -H 'Content-Type: application/json' -d '{
  "sensor":"smoke",
  "events":[{"service":"web","package":"evil","version":"1.0.0","type":1,"behavior":"READ /etc/shadow","timestamp":200,"denied":true}]
}' >/dev/null

ALERTS="$(curl -sf "$ENF_BASE/v1/alerts")"
python3 - "$ALERTS" <<'PY' || fail "blocked alert"
import json, sys
alerts = json.loads(sys.argv[1])
assert any(a.get("blocked") for a in alerts), alerts
print("OK: denied event upgraded alert to blocked")
PY

echo "== ENFORCEMENT SMOKE PASSED =="
