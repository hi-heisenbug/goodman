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

PORT=8845
BASE="http://127.0.0.1:$PORT"
DB="$(mktemp -u /tmp/goodman-e2e-XXXX.db)"
WORK="$ROOT/test/workload"
FAKE_SECRETS="/tmp/goodman-fake-secrets"
SINK_PORT=9999
LEARN_OBS="${LEARN_OBS:-20}"

pids=()
cleanup() {
  for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done
  rm -f "$DB" "$DB"-* /tmp/goodman-e2e-*.log
  rm -rf "$FAKE_SECRETS"
}
trap cleanup EXIT

echo "== 0. prep: fake secrets + local sink =="
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

./bin/sensor -collector "$BASE" -proc-root /proc -batch-interval 500ms \
  >/tmp/goodman-e2e-sensor.log 2>&1 &
pids+=("$!")
sleep 1

install_pkg() { # $1 = version dir
  mkdir -p "$WORK/node_modules"
  rm -rf "$WORK/node_modules/good-pkg"
  cp -r "$ROOT/test/fixtures/$1" "$WORK/node_modules/good-pkg"
}

start_workload() {
  ( cd "$WORK" && GOODMAN_SINK_PORT="$SINK_PORT" GOODMAN_FAKE_CRED="$FAKE_SECRETS/credentials" PORT=8080 \
      node --perf-basic-prof --interpreted-frames-native-stack server.js \
      >/tmp/goodman-e2e-workload.log 2>&1 ) &
  WORKLOAD_PID=$!; pids+=("$WORKLOAD_PID")
  for i in $(seq 1 50); do curl -sf "http://127.0.0.1:8080/healthz" >/dev/null 2>&1 && return 0; sleep 0.1; done
  echo "workload failed to start"; cat /tmp/goodman-e2e-workload.log; return 1
}

drive() { # $1 = requests
  for i in $(seq 1 "$1"); do curl -sf "http://127.0.0.1:8080/" >/dev/null 2>&1 || true; done
}

echo "== 2. baseline: good-pkg@1.0.0, drive traffic, wait for promotion =="
install_pkg good-pkg-1.0.0
start_workload
# generate enough observations to cross the learning window
for round in $(seq 1 6); do drive 60; sleep 0.7; done

for i in $(seq 1 30); do
  if ./bin/goodmanctl fingerprints -collector "$BASE" -json 2>/dev/null | \
     python3 -c 'import json,sys; fps=json.load(sys.stdin); sys.exit(0 if any(f["package"]=="good-pkg" and f["version"]=="1.0.0" and f["is_baseline"] for f in fps) else 1)'; then
    echo "   baseline promoted."; break
  fi
  drive 40; sleep 0.7
done

echo "== 3. swap to good-pkg@1.0.1, restart workload, drive traffic =="
kill "$WORKLOAD_PID" 2>/dev/null || true; sleep 1
install_pkg good-pkg-1.0.1
start_workload
for round in $(seq 1 5); do drive 40; sleep 0.7; done

echo "== 4. assert CRITICAL alert =="
ALERTS=""
for i in $(seq 1 20); do
  ALERTS="$(curl -sf "$BASE/v1/alerts?status=open" || echo '[]')"
  if echo "$ALERTS" | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin) else 1)' 2>/dev/null; then break; fi
  sleep 1
done
echo "alerts: $ALERTS"

echo "$ALERTS" | python3 - <<'PY'
import json, sys
alerts = json.load(sys.stdin)
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

echo "== DRIFT E2E PASSED =="
