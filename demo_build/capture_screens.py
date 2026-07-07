#!/usr/bin/env python3
import subprocess
import time
import os
import sys
import urllib.request
import json
import tempfile
import atexit
from pathlib import Path

BUILD = Path(__file__).parent
SCREENS = BUILD / "screenshots"
SCREENS.mkdir(exist_ok=True)
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

# 1. Backdoor Code Preview
backdoor_html = f"file://{BUILD.resolve()}/backdoor_preview.html"
capture("02_trigger", backdoor_html)

# 2. Fingerprints Explorer
capture("03_evidence", f"http://127.0.0.1:{PORT}/?static=true#fingerprints")

# 3. Alerts Dashboard
capture("04_output", f"http://127.0.0.1:{PORT}/?static=true#alerts")

print("=== Stopping Goodman Collector ===")
collector_proc.terminate()
try:
    collector_proc.wait(timeout=5)
except subprocess.TimeoutExpired:
    collector_proc.kill()

print("=== Screenshot Capture Done ===")
