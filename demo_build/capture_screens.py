#!/usr/bin/env python3
"""Capture the real Mini-Shai-Hulud demo dashboard for the tracked video."""

import atexit
import json
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path


BUILD = Path(__file__).parent
ROOT = BUILD.parent
SCREENS = BUILD / "screenshots"
SCREENS.mkdir(exist_ok=True)
for old_shot in SCREENS.glob("*.png"):
    old_shot.unlink()

PROFILE_DIR = tempfile.TemporaryDirectory(prefix="goodman-demo-chrome-")
atexit.register(PROFILE_DIR.cleanup)
PROFILE = Path(PROFILE_DIR.name)
DB_PATH = Path(tempfile.gettempdir()) / "goodman-video-capture.db"
for suffix in ("", "-wal", "-shm"):
    Path(str(DB_PATH) + suffix).unlink(missing_ok=True)


def free_port():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


PORT = free_port()
BASE_URL = f"http://127.0.0.1:{PORT}"


def wait_for_json(path, predicate, timeout=20):
    deadline = time.time() + timeout
    last_error = None
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(BASE_URL + path, timeout=2) as response:
                payload = json.loads(response.read().decode())
            if predicate(payload):
                return payload
        except Exception as exc:  # bounded retry; diagnostics reported on failure
            last_error = exc
        time.sleep(0.25)
    raise RuntimeError(f"timed out waiting for {path}: {last_error}")


def capture(name, tab):
    out_path = SCREENS / f"{name}.png"
    url = f"{BASE_URL}/?static=true#{tab}"
    print(f"  Capturing {name} from {url}")
    cmd = [
        "chromium",
        "--headless=new",
        "--no-sandbox",
        "--disable-gpu",
        "--disable-dev-shm-usage",
        "--run-all-compositor-stages-before-draw",
        "--virtual-time-budget=10000",
        f"--user-data-dir={PROFILE}",
        "--window-size=1920,1080",
        "--force-device-scale-factor=1",
        "--hide-scrollbars",
        f"--screenshot={out_path}",
        url,
    ]
    subprocess.run(cmd, cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=30)
    size = out_path.stat().st_size
    if size < 80_000:
        raise RuntimeError(f"{out_path} is only {size} bytes; capture is probably blank")
    print(f"    Saved {out_path.name} ({size:,} bytes)")


def stop_process(process):
    if process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def main():
    if not shutil.which("chromium"):
        raise RuntimeError("chromium is required")
    if not (ROOT / "bin/goodmanctl").exists() or not (ROOT / "bin/collector").exists():
        raise RuntimeError("run `make build` before capture")

    print(f"Starting real Goodman demo on {BASE_URL}")
    process = subprocess.Popen(
        [
            str(ROOT / "bin/goodmanctl"),
            "demo",
            "-host",
            "127.0.0.1",
            "-port",
            str(PORT),
            "-db",
            str(DB_PATH),
            "-attack-delay",
            "12s",
        ],
        cwd=ROOT,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
    )
    atexit.register(stop_process, process)
    try:
        wait_for_json("/v1/healthz", lambda payload: payload.get("status") == "ok")
        wait_for_json("/v1/report", lambda payload: payload.get("report", {}).get("declared_count") == 1400)

        capture("01_alerts_seeded", "alerts")

        wait_for_json(
            "/v1/alerts?limit=500",
            lambda alerts: any(alert.get("package") == "mini-shai-hulud-loader" and alert.get("new_version") == "1.0.1" for alert in alerts),
            timeout=25,
        )
        capture("02_mini_shai_hulud", "alerts")
        capture("03_reachability", "reachability")
        capture("04_coverage", "coverage")
        capture("05_fingerprints", "fingerprints")
    finally:
        stop_process(process)
        stderr = process.stderr.read() if process.stderr else ""
        if process.returncode not in (0, -15) and stderr:
            print(stderr[-3000:], file=sys.stderr)
        for suffix in ("", "-wal", "-shm"):
            Path(str(DB_PATH) + suffix).unlink(missing_ok=True)

    print("Screenshot capture complete")


if __name__ == "__main__":
    main()
