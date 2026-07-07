#!/usr/bin/env python3
import subprocess
import time
import os
import sys
import urllib.request
import json
import urllib.parse
import tempfile
import atexit
from pathlib import Path

BUILD = Path(__file__).parent
SCREENS = BUILD / "screenshots"
SCREENS.mkdir(exist_ok=True)
for old_shot in SCREENS.glob("*.png"):
    old_shot.unlink()
PROFILE_DIR = tempfile.TemporaryDirectory(prefix="goodman-demo-chrome-")
atexit.register(PROFILE_DIR.cleanup)
PROF = Path(PROFILE_DIR.name)

# Find a free port
PORT = 8890
DB_PATH = str(BUILD / "goodman_demo.db")

# Remove old db
if os.path.exists(DB_PATH):
    os.remove(DB_PATH)
if os.path.exists(DB_PATH + "-wal"):
    os.remove(DB_PATH + "-wal")
if os.path.exists(DB_PATH + "-shm"):
    os.remove(DB_PATH + "-shm")

print(f"=== Starting Goodman Collector on port {PORT} ===")
collector_proc = subprocess.Popen([
    "./bin/collector",
    "-listen", f":{PORT}",
    "-dsn", DB_PATH,
    "-learn-obs", "3",
    "-learn-min-age", "1s"
], stdout=subprocess.PIPE, stderr=subprocess.PIPE)

# Wait for it to be ready
time.sleep(3)
if collector_proc.poll() is not None:
    print("ERROR: Collector failed to start!")
    stdout, stderr = collector_proc.communicate()
    print("STDOUT:", stdout.decode())
    print("STDERR:", stderr.decode())
    sys.exit(1)

print("=== Injecting Demo Workload & Malicious Drifts ===")
# Run our data injection script
try:
    subprocess.run(["python3", str(BUILD / "inject_demo.py"), f"http://127.0.0.1:{PORT}"], check=True)
except Exception as e:
    print(f"Injection failed: {e}")
    collector_proc.terminate()
    sys.exit(1)

print("=== Verifying Alerts state ===")
try:
    with urllib.request.urlopen(f"http://127.0.0.1:{PORT}/v1/alerts") as r:
        alerts = json.loads(r.read().decode())
        print(f"  Collector alerts loaded: {len(alerts)} alerts in DB")
except Exception as e:
    print(f"Failed to check alerts: {e}")

print("=== Capturing screenshots with Headless Chromium ===")

def capture(name, url):
    out_path = SCREENS / f"{name}.png"
    print(f"  Capturing '{name}' from {url}...")

    # We use virtual-time-budget to let the React app load and fetch from API
    cmd = [
        "chromium",
        "--headless=new",
        "--no-sandbox",
        "--disable-gpu",
        "--disable-dev-shm-usage",
        "--run-all-compositor-stages-before-draw",
        "--virtual-time-budget=10000",
        f"--user-data-dir={PROF}",
        "--window-size=1920,1080",
        "--force-device-scale-factor=1",
        "--hide-scrollbars",
        f"--screenshot={out_path}",
        url
    ]

    try:
        subprocess.run(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
        sz = out_path.stat().st_size
        print(f"    -> Saved {out_path.name} ({sz} bytes)")
    except Exception as e:
        print(f"    WARNING: Failed to capture {name}: {e}")

def post(path):
    req = urllib.request.Request(
        f"http://127.0.0.1:{PORT}{path}",
        data=b"",
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=5) as r:
        return json.loads(r.read().decode())

def set_triage_state():
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{PORT}/v1/alerts") as r:
            alerts = json.loads(r.read().decode())
        if len(alerts) >= 1:
            post(f"/v1/alerts/{urllib.parse.quote(alerts[0]['id'])}/ack")
            print(f"  Acknowledged {alerts[0]['package']} for triage screenshot")
        if len(alerts) >= 2:
            post(f"/v1/alerts/{urllib.parse.quote(alerts[1]['id'])}/resolve")
            print(f"  Resolved {alerts[1]['package']} for triage screenshot")
    except Exception as e:
        print(f"  WARNING: Could not update alert state for triage screenshot: {e}")

# 1. Malicious package update evidence.
backdoor_html = f"file://{BUILD.resolve()}/backdoor_preview.html"
capture("01_malicious_update", backdoor_html)

# 2. Live alert review.
capture("02_alerts_open", f"http://127.0.0.1:{PORT}/?static=true#alerts")

# 3. Fingerprint explorer.
capture("03_fingerprints_all", f"http://127.0.0.1:{PORT}/?static=true#fingerprints")

# 4. Triage state after real API status changes.
set_triage_state()
capture("04_alerts_triaged", f"http://127.0.0.1:{PORT}/?static=true&status=all#alerts")

# 5. Learning fingerprints filter.
capture("05_fingerprints_learning", f"http://127.0.0.1:{PORT}/?static=true&state=learning#fingerprints")

print("=== Stopping Goodman Collector ===")
collector_proc.terminate()
try:
    collector_proc.wait(timeout=5)
except subprocess.TimeoutExpired:
    collector_proc.kill()

print("=== Screenshot Capture Done ===")
