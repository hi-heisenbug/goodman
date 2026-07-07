#!/usr/bin/env bash
# FULL end-to-end drift replay with the REAL eBPF sensor. NEEDS ROOT.
#
#   sudo make e2e         (or)      sudo bash test/e2e/drift_test.sh
#
# Steps (plan §10.3):
#   1. start collector + sensor locally
#   2. run the victim workload requiring good-pkg@1.0.0, drive traffic,
#      wait for baseline promotion (dev-shortened learning window)
#   3. swap to good-pkg@1.0.1, restart the workload, drive traffic
#   4. assert a CRITICAL alert appears naming good-pkg 1.0.0 -> 1.0.1 with the
#      new secret read + local-sink connect
#   5. exit non-zero if no alert or a misattributed alert
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT="$PWD"

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: the eBPF sensor needs root. Run: sudo make e2e" >&2
  exit 2
fi

free_port() {
  python3 - <<'PY'
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

PORT="${GOODMAN_E2E_COLLECTOR_PORT:-$(free_port)}"
BASE="http://127.0.0.1:$PORT"
DB="$(mktemp -u /tmp/goodman-e2e-XXXX.db)"
WORK="$ROOT/test/workload"
FAKE_SECRETS="/tmp/goodman-fake-secrets"
SINK_PORT="${GOODMAN_E2E_SINK_PORT:-$(free_port)}"
WORKLOAD_PORT="${GOODMAN_E2E_WORKLOAD_PORT:-$(free_port)}"
LEARN_OBS="${LEARN_OBS:-20}"

pids=()
workload_pids=()
passed=0
cleanup() {
  for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done
  if [[ "$passed" == "1" && "${GOODMAN_E2E_KEEP_LOGS:-0}" != "1" ]]; then
    rm -f "$DB" "$DB"-* /tmp/goodman-e2e-*.log
    rm -f "$WORK"/isolate-*-v8.log
    for p in "${workload_pids[@]:-}"; do rm -f "/tmp/perf-$p.map"; done
    rm -rf "$FAKE_SECRETS"
  else
    echo "e2e logs preserved under /tmp/goodman-e2e-*.log" >&2
  fi
}
trap cleanup EXIT

dump_state() {
  echo "--- collector log ---" >&2
  tail -n 120 /tmp/goodman-e2e-collector.log >&2 2>/dev/null || true
  echo "--- sensor log ---" >&2
  tail -n 160 /tmp/goodman-e2e-sensor.log >&2 2>/dev/null || true
  echo "--- workload log ---" >&2
  tail -n 120 /tmp/goodman-e2e-workload.log >&2 2>/dev/null || true
  echo "--- fingerprints ---" >&2
  ./bin/goodmanctl fingerprints -collector "$BASE" -json >&2 2>/dev/null || true
  echo "--- alerts ---" >&2
  curl -sf "$BASE/v1/alerts" >&2 2>/dev/null || true
  echo >&2
}

fail() {
  echo "ERROR: $*" >&2
  dump_state
  exit 1
}

echo "== 0. prep: fake secrets + local sink =="
echo "   collector=$PORT workload=$WORKLOAD_PORT sink=$SINK_PORT"
mkdir -p "$FAKE_SECRETS"
echo "AKIA_FAKE_DO_NOT_USE=deadbeef" > "$FAKE_SECRETS/credentials"

# Local exfil sink so the workload's outbound POST connects to something.
cat > /tmp/goodman-e2e-sink.py <<'PY'
import http.server, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get('content-length', 0) or 0))
        self.send_response(200); self.end_headers()
    def log_message(self, *a): pass
http.server.HTTPServer(('127.0.0.1', int(sys.argv[1])), H).serve_forever()
PY
python3 /tmp/goodman-e2e-sink.py "$SINK_PORT" >/tmp/goodman-e2e-sink.log 2>&1 &
pids+=("$!")

echo "== 1. build + start collector and sensor =="
make -s bpf >/dev/null
go build -o bin/collector ./cmd/collector
go build -o bin/sensor ./cmd/sensor

GOODMAN_DSN="$DB" GOODMAN_LEARN_OBS="$LEARN_OBS" GOODMAN_LEARN_MIN_AGE=1ns GOODMAN_LISTEN=":$PORT" \
  ./bin/collector >/tmp/goodman-e2e-collector.log 2>&1 &
pids+=("$!")
for i in $(seq 1 50); do curl -sf "$BASE/v1/healthz" >/dev/null 2>&1 && break; sleep 0.1; done

./bin/sensor -collector "$BASE" -proc-root /proc -batch-interval 500ms -metrics-addr "" -watch-interval 200ms \
  >/tmp/goodman-e2e-sensor.log 2>&1 &
pids+=("$!")
sleep 1

install_pkg() { # $1 = version dir
  mkdir -p "$WORK/node_modules"
  rm -rf "$WORK/node_modules/good-pkg"
  cp -r "$ROOT/test/fixtures/$1" "$WORK/node_modules/good-pkg"
}

start_workload() {
  ( cd "$WORK" && export GOODMAN_SINK_PORT="$SINK_PORT" GOODMAN_FAKE_CRED="$FAKE_SECRETS/credentials" PORT="$WORKLOAD_PORT" && \
      exec node --perf-basic-prof --interpreted-frames-native-stack server.js \
      >/tmp/goodman-e2e-workload.log 2>&1 ) &
  WORKLOAD_PID=$!; pids+=("$WORKLOAD_PID"); workload_pids+=("$WORKLOAD_PID")
  for i in $(seq 1 50); do
    kill -0 "$WORKLOAD_PID" 2>/dev/null || { echo "workload exited early"; cat /tmp/goodman-e2e-workload.log; return 1; }
    curl -sf "http://127.0.0.1:$WORKLOAD_PORT/healthz" >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  echo "workload failed to start"; cat /tmp/goodman-e2e-workload.log; return 1
}

wait_workload_watched() {
  for i in $(seq 1 75); do
    grep -q "watching pid $WORKLOAD_PID" /tmp/goodman-e2e-sensor.log 2>/dev/null && return 0
    kill -0 "$WORKLOAD_PID" 2>/dev/null || fail "workload pid $WORKLOAD_PID exited before the sensor watched it"
    sleep 0.1
  done
  fail "sensor did not watch workload pid $WORKLOAD_PID"
}

drive() { # $1 = requests
  for i in $(seq 1 "$1"); do curl -sf "http://127.0.0.1:$WORKLOAD_PORT/" >/dev/null; done
}

echo "== 2. baseline: good-pkg@1.0.0, drive traffic, wait for promotion =="
install_pkg good-pkg-1.0.0
start_workload
wait_workload_watched
# generate enough observations to cross the learning window
for round in $(seq 1 6); do drive 60; sleep 0.7; done

promoted=0
for i in $(seq 1 30); do
  if ./bin/goodmanctl fingerprints -collector "$BASE" -json 2>/dev/null | \
     python3 -c 'import json,sys; fps=json.load(sys.stdin); sys.exit(0 if any(f["package"]=="good-pkg" and f["version"]=="1.0.0" and f["is_baseline"] for f in fps) else 1)'; then
    echo "   baseline promoted."
    promoted=1
    break
  fi
  drive 40; sleep 0.7
done
[[ "$promoted" == "1" ]] || fail "good-pkg@1.0.0 baseline was not promoted"

echo "== 3. swap to good-pkg@1.0.1, restart workload, drive traffic =="
kill "$WORKLOAD_PID" 2>/dev/null || true; sleep 1
install_pkg good-pkg-1.0.1
start_workload
wait_workload_watched
for round in $(seq 1 5); do drive 40; sleep 0.7; done

echo "== 4. assert CRITICAL alert =="
ALERTS=""
for i in $(seq 1 20); do
  ALERTS="$(curl -sf "$BASE/v1/alerts?status=open" || echo '[]')"
  if echo "$ALERTS" | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin) else 1)' 2>/dev/null; then break; fi
  sleep 1
done
echo "alerts: $ALERTS"

ALERTS_JSON="$ALERTS" python3 - <<'PY'
import json, sys
import os
alerts = json.loads(os.environ["ALERTS_JSON"])
crit = [a for a in alerts if a["package"] == "good-pkg" and a["severity"] == "CRITICAL"]
assert crit, f"no CRITICAL good-pkg alert; got {alerts}"
a = crit[0]
assert a["new_version"] == "1.0.1", a["new_version"]
assert a["old_version"] == "1.0.0", a["old_version"]
nb = " ".join(a["new_behaviors"])
assert "credential" in nb.lower() or "secret" in nb.lower(), f"missing secret read: {a['new_behaviors']}"
assert "127.0.0.1:9999" in nb or "CONNECT" in nb, f"missing sink connect: {a['new_behaviors']}"
# never misattribute to a different package
assert not any(x["package"] not in ("good-pkg","<app>","<unknown>") for x in alerts), alerts
print("OK: CRITICAL drift alert for good-pkg 1.0.0 -> 1.0.1 with secret read + sink connect")
PY

passed=1
echo "== DRIFT E2E PASSED =="
