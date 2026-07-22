#!/usr/bin/env python3
"""Capture a sanitized real-host attribution proof for the product film."""

import json
import os
import re
import shutil
import socket
import subprocess
import threading
import time
import urllib.request
from datetime import datetime, timezone
from pathlib import Path


BUILD = Path(__file__).parent
ROOT = BUILD.parent
WORKLOAD = ROOT / "test" / "workload"
EVIDENCE = BUILD / "evidence"
OUTPUT = EVIDENCE / "observe_proof.json"
PARTIAL = OUTPUT.with_suffix(".partial.json")
SUMMARY = re.compile(
    r"proof summary: (?P<events>\d+) events, (?P<behaviors>\d+) unique behaviors, "
    r"(?P<exact>\d+) exact dependency events"
)
PASS = "PASS: Goodman attributed real syscalls to 1 dependency identity."


def require_tools() -> None:
    missing = [tool for tool in ("bash", "docker", "make", "node") if not shutil.which(tool)]
    if missing:
        raise RuntimeError(f"missing observe-proof tools: {', '.join(missing)}")


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def wait_for_workload(url: str, timeout: float = 10) -> None:
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=1) as response:
                if response.status == 200:
                    return
        except Exception as error:
            last_error = error
        time.sleep(0.1)
    raise RuntimeError(f"workload did not become ready: {last_error}")


def drive_traffic(url: str, stop: threading.Event) -> None:
    while not stop.is_set():
        try:
            with urllib.request.urlopen(url, timeout=1):
                pass
        except Exception:
            if not stop.is_set():
                time.sleep(0.1)
        time.sleep(0.08)


def stop_process(process: subprocess.Popen | None) -> None:
    if process is None or process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=8)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def observe_command(pid: int) -> list[str]:
    return [
        "bash",
        "scripts/setup-everything.sh",
        "observe",
        "--pid",
        str(pid),
        "--duration",
        "8s",
        "--live-backend",
        "docker",
    ]


def parse_output(output: str) -> dict[str, object]:
    summary = SUMMARY.search(output)
    if summary is None:
        raise RuntimeError(f"observe output has no proof summary:\n{output[-4000:]}")
    evidence_line = next(
        (line for line in output.splitlines() if " | good-pkg@" in line),
        None,
    )
    if evidence_line is None:
        raise RuntimeError(f"observe output has no good-pkg evidence:\n{output[-4000:]}")
    _, identity, behavior = [part.strip() for part in evidence_line.split(" | ", 2)]
    package, version = identity.rsplit("@", 1)
    behavior = sanitize_behavior(behavior)
    if PASS not in output:
        raise RuntimeError(f"observe verification did not pass:\n{output[-4000:]}")
    return {
        "schema": "goodman.demo.observe-proof/v1",
        "captured_at": datetime.now(timezone.utc).isoformat(),
        "source": "live host process via privileged Docker and host kernel",
        "command": (
            "bash scripts/setup-everything.sh observe --pid <PID> "
            "--duration 8s --live-backend docker"
        ),
        "package": package,
        "version": version,
        "behavior": behavior,
        "events": int(summary.group("events")),
        "unique_behaviors": int(summary.group("behaviors")),
        "exact_dependency_events": int(summary.group("exact")),
        "pass": PASS,
    }


def sanitize_behavior(behavior: str) -> str:
    marker = "/node_modules/"
    if marker not in behavior:
        raise RuntimeError(f"unexpected observe behavior: {behavior}")
    suffix = behavior.split(marker, 1)[1]
    return f"READ …/node_modules/{suffix}"


def write_evidence(payload: dict[str, object]) -> None:
    EVIDENCE.mkdir(exist_ok=True)
    PARTIAL.write_text(json.dumps(payload, indent=2) + "\n")
    os.replace(PARTIAL, OUTPUT)
    print(
        f"Captured {OUTPUT}: {payload['events']} events, "
        f"{payload['package']}@{payload['version']}"
    )


def main() -> None:
    require_tools()
    subprocess.run(["make", "workload"], cwd=ROOT, check=True)
    port = free_port()
    url = f"http://127.0.0.1:{port}/"
    environment = os.environ.copy()
    environment.update(
        {
            "PORT": str(port),
            "NODE_OPTIONS": (
                "--perf-basic-prof-only-functions "
                "--interpreted-frames-native-stack"
            ),
        }
    )
    workload = None
    traffic_stop = threading.Event()
    traffic = None
    try:
        workload = subprocess.Popen(
            ["node", "server.js"],
            cwd=WORKLOAD,
            env=environment,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.STDOUT,
        )
        wait_for_workload(url)
        traffic = threading.Thread(target=drive_traffic, args=(url, traffic_stop), daemon=True)
        traffic.start()
        result = subprocess.run(
            observe_command(workload.pid),
            cwd=ROOT,
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=240,
        )
        if result.returncode != 0:
            raise RuntimeError(f"observe command failed ({result.returncode}):\n{result.stdout[-4000:]}")
        write_evidence(parse_output(result.stdout))
    finally:
        traffic_stop.set()
        if traffic is not None:
            traffic.join(timeout=2)
        pid = workload.pid if workload is not None else None
        stop_process(workload)
        if pid is not None:
            Path(f"/tmp/perf-{pid}.map").unlink(missing_ok=True)
        PARTIAL.unlink(missing_ok=True)


if __name__ == "__main__":
    main()
