#!/usr/bin/env bash
# Backend smoke test WITHOUT eBPF/root: drive synthetic attributed events
# through the collector to exercise store -> fingerprint -> diff -> API ->
# dashboard end to end. Proves everything except the kernel capture path
# (that path is covered by `make e2e`, which needs root).
set -euo pipefail
COLLECTOR="${1:-http://127.0.0.1:8844}"

post() { # $1 = json events array
  curl -sf -X POST "$COLLECTOR/v1/events" \
    -H 'content-type: application/json' \
    -d "{\"sensor\":\"synth\",\"events\":$1}" >/dev/null
}

ev() { # service pkg version type behavior ts
  printf '{"service":"%s","package":"%s","version":"%s","type":%s,"behavior":"%s","timestamp":%s}' \
    "$1" "$2" "$3" "$4" "$5" "$6"
}

baseline_batch() { # version ts
  echo "[$(ev web good-pkg "$1" 1 'READ /app/node_modules/good-pkg/**' "$2"),$(ev web good-pkg "$1" 2 'CONNECT 10.0.0.5:5432' "$2")]"
}

echo "→ learning good-pkg@1.0.0 baseline…"
ts=1000000000000
for i in $(seq 1 8); do
  post "$(baseline_batch 1.0.0 $((ts + i)))"
done

echo "→ pushing benign version bump 1.0.0 -> 1.0.0-clean (must NOT alert)…"
post "$(baseline_batch 1.0.0-clean $((ts + 100)))"

echo "→ pushing DRIFTED version 1.0.1 (secret read + metadata connect)…"
drift="[$(ev web good-pkg 1.0.1 1 'READ /app/node_modules/good-pkg/**' $((ts+200))),\
$(ev web good-pkg 1.0.1 2 'CONNECT 10.0.0.5:5432' $((ts+201))),\
$(ev web good-pkg 1.0.1 1 'READ /tmp/goodman-fake-secrets/credentials' $((ts+202))),\
$(ev web good-pkg 1.0.1 2 'CONNECT 169.254.169.254:80' $((ts+203)))]"
post "$drift"

sleep 0.5
echo "→ alerts:"
curl -sf "$COLLECTOR/v1/alerts?status=open"
echo
