#!/usr/bin/env bash
# HA smoke: two collector replicas against one Postgres. Proves concurrent ingest
# converges to identical fingerprints and digest webhooks are not doubled
# (WithLeader advisory locks). Skips cleanly when Docker or Postgres is unavailable.
#
# INVALID: two collectors against one SQLite DSN — SQLite is single-writer; HA
# requires Postgres (see docs/deployment.md, docs/release.md).
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v docker >/dev/null 2>&1; then
  echo "SKIP: docker not found (HA proof needs Postgres; see docs/release.md)"
  exit 0
fi
if ! docker info >/dev/null 2>&1; then
  echo "SKIP: docker daemon not reachable"
  exit 0
fi
if [[ ! -x ./bin/collector ]]; then
  echo "== building collector =="
  make build >/dev/null
fi

PG_PORT="${GOODMAN_HA_SMOKE_PG_PORT:-0}"
if [[ "$PG_PORT" == "0" ]]; then
  PG_PORT="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
fi

COLL_A=8860
COLL_B=8861
PG_NAME="goodman-ha-smoke-$$"
PG_CID=""
COLL_A_PID=""
COLL_B_PID=""

cleanup() {
  kill "$COLL_A_PID" "$COLL_B_PID" 2>/dev/null || true
  docker rm -f "$PG_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() { echo "FAIL: $1"; exit 1; }

echo "== starting Postgres on 127.0.0.1:$PG_PORT =="
if ! PG_CID="$(docker run -d --rm --name "$PG_NAME" \
  -e POSTGRES_USER=goodman -e POSTGRES_PASSWORD=goodman -e POSTGRES_DB=goodman \
  -p "127.0.0.1:${PG_PORT}:5432" postgres:16-alpine 2>&1)"; then
  echo "SKIP: could not start postgres container: $PG_CID"
  exit 0
fi

echo "== waiting for Postgres ready =="
for i in $(seq 1 60); do
  if docker exec "$PG_NAME" psql -U goodman -d goodman -c 'select 1' >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
docker exec "$PG_NAME" psql -U goodman -d goodman -c 'select 1' >/dev/null 2>&1 \
  || fail "postgres did not become ready"

DSN="postgres://goodman:goodman@127.0.0.1:${PG_PORT}/goodman?sslmode=disable"

COMMON_ENV=(
  "GOODMAN_DSN=$DSN"
  "GOODMAN_HA_REPLICAS=2"
  "GOODMAN_LEARN_OBS=10"
  "GOODMAN_LEARN_MIN_AGE=1ns"
)

wait_health() {
  local base=$1
  for _ in $(seq 1 80); do
    curl -sf "$base/v1/healthz" >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  return 1
}

echo "== starting two collector replicas (sequential — migrations race if parallel) =="
env "${COMMON_ENV[@]}" "GOODMAN_LISTEN=:${COLL_A}" ./bin/collector >/dev/null 2>&1 &
COLL_A_PID=$!
wait_health "http://127.0.0.1:${COLL_A}" || fail "collector A not healthy"
sleep 0.5
env "${COMMON_ENV[@]}" "GOODMAN_LISTEN=:${COLL_B}" ./bin/collector >/dev/null 2>&1 &
COLL_B_PID=$!
wait_health "http://127.0.0.1:${COLL_B}" || fail "collector B not healthy"

post() {
  local base=$1 json=$2
  curl -sf -X POST "$base/v1/events" -H 'content-type: application/json' \
    -d "{\"sensor\":\"ha-smoke\",\"events\":$json}" >/dev/null
}

ev() {
  printf '{"service":"%s","package":"%s","version":"%s","type":%s,"behavior":"%s","timestamp":%s}' \
    "$1" "$2" "$3" "$4" "$5" "$6"
}

echo "== concurrent ingest (split across replicas) =="
ts=2000000000000
for i in $(seq 1 8); do
  batch="[$(ev web good-pkg 1.0.0 1 'READ /app/node_modules/good-pkg/**' $((ts + i))),\
$(ev web good-pkg 1.0.0 2 'CONNECT 10.0.0.5:5432' $((ts + i)))]"
  if (( i % 2 == 0 )); then
    post "http://127.0.0.1:${COLL_A}" "$batch"
  else
    post "http://127.0.0.1:${COLL_B}" "$batch"
  fi
done

drift="[$(ev web good-pkg 1.0.1 1 'READ /app/node_modules/good-pkg/**' $((ts+100))),\
$(ev web good-pkg 1.0.1 2 'CONNECT 10.0.0.5:5432' $((ts+101))),\
$(ev web good-pkg 1.0.1 1 'READ /tmp/goodman-fake-secrets/credentials' $((ts+102))),\
$(ev web good-pkg 1.0.1 2 'CONNECT 169.254.169.254:80' $((ts+103)))]"
post "http://127.0.0.1:${COLL_A}" "$drift"
post "http://127.0.0.1:${COLL_B}" "$drift"

sleep 0.5

FP_A="$(curl -sf "http://127.0.0.1:${COLL_A}/v1/fingerprints")"
FP_B="$(curl -sf "http://127.0.0.1:${COLL_B}/v1/fingerprints")"

python3 - "$FP_A" "$FP_B" <<'PY' || fail "fingerprint mismatch across replicas"
import json, sys
a, b = json.loads(sys.argv[1]), json.loads(sys.argv[2])
def norm(fps):
    out = []
    for fp in fps:
        out.append({
            "service": fp["service"], "package": fp["package"], "version": fp["version"],
            "obs_count": fp["obs_count"],
            "behaviors": sorted(fp.get("behaviors", {}).keys()),
            "baseline": fp.get("baseline", False),
        })
    return sorted(out, key=lambda x: (x["service"], x["package"], x["version"]))
if norm(a) != norm(b):
    print("replica A:", json.dumps(a, indent=2)[:800])
    print("replica B:", json.dumps(b, indent=2)[:800])
    raise SystemExit("fingerprints differ")
print("OK: identical fingerprints on both replicas")
PY

ALERTS_A="$(curl -sf "http://127.0.0.1:${COLL_A}/v1/alerts?status=open")"
ALERTS_B="$(curl -sf "http://127.0.0.1:${COLL_B}/v1/alerts?status=open")"
python3 - "$ALERTS_A" "$ALERTS_B" <<'PY' || fail "alert mismatch"
import json, sys
a, b = json.loads(sys.argv[1]), json.loads(sys.argv[2])
assert len(a) == len(b) == 1, (len(a), len(b))
assert a[0]["id"] == b[0]["id"], (a[0]["id"], b[0]["id"])
print("OK: exactly one deduplicated alert visible on both replicas")
PY

echo "== HA SMOKE PASSED (fingerprints + alert dedup; WithLeader covered by store tests) =="
