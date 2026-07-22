#!/usr/bin/env bash
# Real eBPF/V8 proof of the current OpenClaw runtime and ClawHub disk contract.
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT="$PWD"

if [[ $EUID -ne 0 ]]; then
	echo "ERROR: the OpenClaw eBPF proof needs root. Run: sudo make e2e-openclaw" >&2
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

if [[ "${GOODMAN_E2E_SKIP_BUILD:-0}" != "1" ]]; then
	make -s build >/dev/null
fi
for binary in bin/collector bin/sensor bin/goodmanctl; do
	[[ -x "$binary" ]] || { echo "missing $binary; run make build" >&2; exit 2; }
done

TMP_ROOT="$(mktemp -d /tmp/goodman-openclaw-e2e-XXXXXX)"
WORKSPACE="$TMP_ROOT/workspace"
SKILL_DIR="$WORKSPACE/skills/calendar-sync"
CREDENTIAL="$TMP_ROOT/credentials"
DB="$TMP_ROOT/goodman.db"
PORT="${GOODMAN_OPENCLAW_E2E_COLLECTOR_PORT:-$(free_port)}"
GATEWAY_PORT="${GOODMAN_OPENCLAW_E2E_GATEWAY_PORT:-$(free_port)}"
SINK_PORT="${GOODMAN_OPENCLAW_E2E_SINK_PORT:-$(free_port)}"
BASE="http://127.0.0.1:$PORT"
BASELINE_ROUNDS="${GOODMAN_OPENCLAW_E2E_BASELINE_ROUNDS:-6}"
PROMOTE_ATTEMPTS="${GOODMAN_OPENCLAW_E2E_PROMOTE_ATTEMPTS:-40}"
PIDS=()
GATEWAY_PIDS=()
GATEWAY_PID=""
PASSED=0

cleanup() {
	for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done
	for pid in "${GATEWAY_PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done
	if [[ "$PASSED" == 1 && "${GOODMAN_E2E_KEEP_LOGS:-0}" != 1 ]]; then
		for pid in "${GATEWAY_PIDS[@]:-}"; do rm -f "/tmp/perf-$pid.map"; done
		rm -rf "$TMP_ROOT"
	else
		echo "OpenClaw e2e artifacts: $TMP_ROOT" >&2
	fi
}
trap cleanup EXIT

fail() {
	echo "ERROR: $*" >&2
	tail -n 120 "$TMP_ROOT/collector.log" >&2 2>/dev/null || true
	tail -n 160 "$TMP_ROOT/sensor.log" >&2 2>/dev/null || true
	tail -n 120 "$TMP_ROOT/gateway.log" >&2 2>/dev/null || true
	echo "--- fingerprints ---" >&2
	./bin/goodmanctl fingerprints -collector "$BASE" -json >&2 2>/dev/null || true
	echo "--- alerts ---" >&2
	curl -sf "$BASE/v1/alerts" >&2 2>/dev/null || true
	echo >&2
	exit 1
}

write_install_metadata() {
	local version="$1" installed_at="$2"
	mkdir -p "$SKILL_DIR/.clawhub" "$WORKSPACE/.clawhub"
	cat > "$SKILL_DIR/.clawhub/origin.json" <<JSON
{"version":1,"registry":"https://clawhub.ai","slug":"calendar-sync","ownerHandle":"goodman-demo","installedVersion":"$version","installedAt":$installed_at}
JSON
	cat > "$WORKSPACE/.clawhub/lock.json" <<JSON
{"version":1,"skills":{"calendar-sync":{"version":"$version","installedAt":$installed_at,"registry":"https://clawhub.ai","ownerHandle":"goodman-demo"}}}
JSON
}

start_gateway() {
	local mode="$1"
	(
		cd "$SKILL_DIR"
		exec env \
			NODE_OPTIONS="--perf-basic-prof-only-functions --interpreted-frames-native-stack" \
			GOODMAN_SERVICE=openclaw \
			GOODMAN_OPENCLAW_MODE="$mode" \
			GOODMAN_OPENCLAW_CREDENTIAL="$CREDENTIAL" \
			GOODMAN_OPENCLAW_SINK_PORT="$SINK_PORT" \
			PORT="$GATEWAY_PORT" \
			node runtime.js >"$TMP_ROOT/gateway.log" 2>&1
	) &
	GATEWAY_PID=$!
	GATEWAY_PIDS+=("$GATEWAY_PID")
	for _ in $(seq 1 80); do
		kill -0 "$GATEWAY_PID" 2>/dev/null || fail "OpenClaw fixture exited before becoming ready"
		curl -sf "http://127.0.0.1:$GATEWAY_PORT/healthz" >/dev/null 2>&1 && break
		sleep 0.1
	done
	[[ "$(tr -d '\n' < "/proc/$GATEWAY_PID/comm")" == "openclaw-gatewa" ]] ||
		fail "Gateway comm is not the current OpenClaw Linux title"
	for _ in $(seq 1 80); do
		grep -q "watching pid $GATEWAY_PID" "$TMP_ROOT/sensor.log" 2>/dev/null && return
		sleep 0.1
	done
	fail "sensor did not watch current OpenClaw comm for pid $GATEWAY_PID"
}

drive() {
	local count="$1"
	for _ in $(seq 1 "$count"); do curl -sf "http://127.0.0.1:$GATEWAY_PORT/" >/dev/null; done
}

echo "== OpenClaw e2e: prepare versioned ClawHub workspace =="
mkdir -p "$SKILL_DIR"
cp test/openclaw/gateway_fixture.js "$SKILL_DIR/runtime.js"
printf '%s\n' '---' 'name: calendar-sync' 'description: Goodman live attribution fixture' '---' > "$SKILL_DIR/SKILL.md"
printf '%s\n' 'OPENCLAW_FAKE_CREDENTIAL=do-not-use' > "$CREDENTIAL"
write_install_metadata "1.2.2" 1784678400000

python3 - "$SINK_PORT" >"$TMP_ROOT/sink.log" 2>&1 <<'PY' &
import socket, sys
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", int(sys.argv[1])))
s.listen()
while True:
    conn, _ = s.accept()
    conn.recv(4096)
    conn.close()
PY
PIDS+=("$!")

GOODMAN_DSN="$DB" GOODMAN_LEARN_OBS=20 GOODMAN_LEARN_MIN_AGE=1ns GOODMAN_LISTEN=":$PORT" \
	./bin/collector >"$TMP_ROOT/collector.log" 2>&1 &
PIDS+=("$!")
for _ in $(seq 1 80); do curl -sf "$BASE/v1/healthz" >/dev/null 2>&1 && break; sleep 0.1; done

SENSOR_ARGS=(-collector "$BASE" -proc-root /proc -batch-interval 300ms -watch-interval 200ms -metrics-addr "")
if [[ "${GOODMAN_OPENCLAW_E2E_STDOUT:-0}" == 1 ]]; then
	SENSOR_ARGS+=(-stdout -raw)
fi
./bin/sensor "${SENSOR_ARGS[@]}" >"$TMP_ROOT/sensor.log" 2>&1 &
PIDS+=("$!")
sleep 1

echo "== OpenClaw e2e: learn @goodman-demo/calendar-sync@1.2.2 =="
start_gateway baseline
for _ in $(seq 1 "$BASELINE_ROUNDS"); do drive 50; sleep 0.4; done
PERF_READY=0
for _ in $(seq 1 40); do
	if grep -q "workspace/skills/calendar-sync/runtime.js" "/tmp/perf-$GATEWAY_PID.map" 2>/dev/null; then
		PERF_READY=1
		break
	fi
	sleep 0.1
done
[[ "$PERF_READY" == 1 ]] || fail "OpenClaw fixture did not emit the expected V8 perf-map symbols"
PROMOTED=0
for _ in $(seq 1 "$PROMOTE_ATTEMPTS"); do
	if ./bin/goodmanctl fingerprints -collector "$BASE" -json 2>/dev/null | python3 -c '
import json,sys
fps=json.load(sys.stdin)
sys.exit(0 if any(f["service"]=="openclaw" and f["package"]=="@goodman-demo/calendar-sync" and f["version"]=="1.2.2" and f["is_baseline"] for f in fps) else 1)
'; then
		PROMOTED=1
		break
	fi
	drive 30
	sleep 0.4
done
[[ "$PROMOTED" == 1 ]] || fail "ClawHub skill baseline was not promoted"

kill "$GATEWAY_PID" 2>/dev/null || true
wait "$GATEWAY_PID" 2>/dev/null || true
sleep 0.5
write_install_metadata "1.2.3" 1784678401000

echo "== OpenClaw e2e: replay credential read + new connection at 1.2.3 =="
start_gateway attack
for _ in $(seq 1 6); do drive 40; sleep 0.4; done

ALERTS='[]'
FOUND=0
for _ in $(seq 1 40); do
	ALERTS="$(curl -sf "$BASE/v1/alerts?status=open" || echo '[]')"
	if ALERTS_JSON="$ALERTS" python3 -c '
import json,os,sys
alerts=json.loads(os.environ["ALERTS_JSON"])
match=next((a for a in alerts if a["service"]=="openclaw" and a["package"]=="@goodman-demo/calendar-sync" and a["old_version"]=="1.2.2" and a["new_version"]=="1.2.3" and a["severity"]=="CRITICAL"),None)
sys.exit(0 if match else 1)
'; then
		FOUND=1
		break
	fi
	drive 10
	sleep 0.5
done
[[ "$FOUND" == 1 ]] || fail "no CRITICAL OpenClaw skill drift alert"

curl -sf "$BASE/v1/export" | python3 -c '
import json,sys
data=json.load(sys.stdin)
assert data["schema"] == "goodman.export/v1", data
assert any(a["package"] == "@goodman-demo/calendar-sync" for a in data["alerts"]), data["alerts"]
assert any(f["package"] == "@goodman-demo/calendar-sync" for f in data["fingerprints"]), data["fingerprints"]
print("OK: complete export contains the OpenClaw alert and fingerprints")
' || fail "complete export omitted OpenClaw data"

echo "OK: current openclaw-gatewa comm stayed watched"
echo "OK: trusted origin + workspace lock resolved @goodman-demo/calendar-sync@1.2.3"
echo "OK: V8 perf-map symbols were emitted and real eBPF attribution raised the expected CRITICAL drift"
PASSED=1
echo "== OPENCLAW E2E PASSED =="
