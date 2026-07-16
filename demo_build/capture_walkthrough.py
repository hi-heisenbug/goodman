#!/usr/bin/env python3
"""Record a deterministic live Goodman dashboard walkthrough under Xvfb."""

import json
import os
import shutil
import signal
import socket
import subprocess
import tempfile
import threading
import time
import urllib.request
from pathlib import Path
from typing import Any

from walkthrough import PLAN, validate_recording

BUILD = Path(__file__).parent
ROOT = BUILD.parent
RECORDINGS = BUILD / "recordings"
OUTPUT = RECORDINGS / PLAN["output"]
PARTIAL = OUTPUT.with_name(f"{OUTPUT.stem}.partial{OUTPUT.suffix}")


def require_tools() -> None:
    required = ("Xvfb", "chromium", "ffmpeg", "ffprobe", "node", "xdotool")
    missing = [tool for tool in required if not shutil.which(tool)]
    if missing:
        raise RuntimeError(f"missing walkthrough tools: {', '.join(missing)}")
    for binary in (ROOT / "bin/goodmanctl", ROOT / "bin/collector"):
        if not binary.exists():
            raise RuntimeError(f"missing {binary}; run `make build` first")


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def free_display() -> str:
    for number in range(90, 150):
        if not Path(f"/tmp/.X11-unix/X{number}").exists():
            return f":{number}"
    raise RuntimeError("no free X display between :90 and :149")


def wait_for_json(base_url: str, path: str, predicate, timeout: float = 20) -> Any:
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(base_url + path, timeout=2) as response:
                payload = json.loads(response.read().decode())
            if predicate(payload):
                return payload
        except Exception as error:  # bounded retry with final diagnostics
            last_error = error
        time.sleep(0.2)
    raise RuntimeError(f"timed out waiting for {path}: {last_error}")


def stop_process(process: subprocess.Popen | None, interrupt: bool = False) -> None:
    if process is None or process.poll() is not None:
        return
    process.send_signal(signal.SIGINT if interrupt else signal.SIGTERM)
    try:
        process.wait(timeout=8)
        return
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def start_demo(port: int, database: Path) -> tuple[subprocess.Popen, threading.Event, dict[str, float]]:
    timer_ready = threading.Event()
    timing: dict[str, float] = {}
    command = [
        str(ROOT / "bin/goodmanctl"),
        "demo",
        "-host",
        "127.0.0.1",
        "-port",
        str(port),
        "-db",
        str(database),
        "-attack-delay",
        f"{PLAN['attack_delay_seconds']}s",
    ]
    process = subprocess.Popen(
        command,
        cwd=ROOT,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        bufsize=1,
    )

    def read_output() -> None:
        if process.stdout is None:
            return
        for line in process.stdout:
            print(f"[demo] {line.rstrip()}")
            if "Replaying Mini-Shai-Hulud behavior in" in line:
                timing["attack_timer_started"] = time.monotonic()
                timer_ready.set()

    threading.Thread(target=read_output, daemon=True).start()
    return process, timer_ready, timing


def start_xvfb(display: str) -> subprocess.Popen:
    process = subprocess.Popen(
        [
            "Xvfb",
            display,
            "-screen",
            "0",
            f"{PLAN['width']}x{PLAN['height']}x24",
            "-nolisten",
            "tcp",
            "-ac",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    socket_path = Path(f"/tmp/.X11-unix/X{display.removeprefix(':')}")
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        if socket_path.exists():
            return process
        if process.poll() is not None:
            break
        time.sleep(0.05)
    stop_process(process)
    raise RuntimeError(f"Xvfb did not start on {display}")


def display_environment(display: str) -> dict[str, str]:
    environment = os.environ.copy()
    environment.update(
        {
            "DISPLAY": display,
            "XCURSOR_SIZE": "34",
            "XCURSOR_THEME": "Adwaita",
        }
    )
    return environment


def start_chromium(
    base_url: str,
    profile: Path,
    environment: dict[str, str],
    debug_port: int,
) -> subprocess.Popen:
    command = [
        "chromium",
        "--disable-gpu",
        "--disable-dev-shm-usage",
        "--disable-background-timer-throttling",
        "--disable-renderer-backgrounding",
        "--disable-session-crashed-bubble",
        "--disable-infobars",
        "--no-first-run",
        "--hide-scrollbars",
        "--remote-allow-origins=*",
        f"--remote-debugging-port={debug_port}",
        f"--user-data-dir={profile}",
        f"--window-size={PLAN['width']},{PLAN['height']}",
        "--window-position=0,0",
        "--force-device-scale-factor=1",
        f"--app={base_url}/#alerts",
    ]
    process = subprocess.Popen(
        command,
        cwd=ROOT,
        env=environment,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    wait_for_window(environment)
    wait_for_browser_state(
        debug_port,
        expected_hash="#alerts",
        expected_text=PLAN["ready_text"],
        timeout=12,
    )
    return process


def run_xdotool(environment: dict[str, str], *arguments: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["xdotool", *arguments],
        env=environment,
        check=check,
        capture_output=True,
        text=True,
    )


def wait_for_window(environment: dict[str, str]) -> None:
    deadline = time.monotonic() + 12
    window_id = ""
    while time.monotonic() < deadline:
        result = run_xdotool(environment, "search", "--onlyvisible", "--class", "chromium", check=False)
        if result.returncode == 0 and result.stdout.strip():
            window_id = result.stdout.strip().splitlines()[-1]
            break
        time.sleep(0.2)
    if not window_id:
        raise RuntimeError("Chromium window did not appear under Xvfb")
    run_xdotool(environment, "windowmove", window_id, "0", "0")
    run_xdotool(
        environment,
        "windowsize",
        window_id,
        str(PLAN["width"]),
        str(PLAN["height"]),
    )
    run_xdotool(environment, "windowfocus", window_id)


def browser_state(debug_port: int) -> dict[str, str]:
    result = subprocess.run(
        ["node", str(BUILD / "browser_state.mjs"), str(debug_port)],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or "browser state helper failed")
    return json.loads(result.stdout)


def wait_for_browser_state(
    debug_port: int,
    *,
    expected_hash: str | None = None,
    expected_text: str | None = None,
    timeout: float = 4,
) -> dict[str, str]:
    deadline = time.monotonic() + timeout
    last_state: dict[str, str] | None = None
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            last_state = browser_state(debug_port)
            hash_matches = expected_hash is None or last_state.get("hash") == expected_hash
            text_matches = expected_text is None or expected_text in last_state.get("text", "")
            if last_state.get("readyState") == "complete" and hash_matches and text_matches:
                return last_state
        except Exception as error:  # Chrome may still be opening its debug endpoint
            last_error = error
        time.sleep(0.1)
    raise RuntimeError(
        "timed out waiting for browser state "
        f"hash={expected_hash!r} text={expected_text!r}; "
        f"last_state={last_state!r} last_error={last_error!r}"
    )


def start_recorder(display: str) -> subprocess.Popen:
    RECORDINGS.mkdir(exist_ok=True)
    PARTIAL.unlink(missing_ok=True)
    return subprocess.Popen(
        [
            "ffmpeg",
            "-y",
            "-f",
            "x11grab",
            "-draw_mouse",
            "1",
            "-framerate",
            str(PLAN["fps"]),
            "-video_size",
            f"{PLAN['width']}x{PLAN['height']}",
            "-i",
            f"{display}.0",
            "-an",
            "-c:v",
            "libx264",
            "-preset",
            "veryfast",
            "-crf",
            "16",
            "-pix_fmt",
            "yuv420p",
            "-r",
            str(PLAN["fps"]),
            "-fps_mode",
            "cfr",
            "-frames:v",
            str(int(PLAN["fps"] * PLAN["duration_seconds"])),
            "-movflags",
            "+faststart",
            "-loglevel",
            "error",
            str(PARTIAL),
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
    )


def sleep_until(target: float) -> None:
    remaining = target - time.monotonic()
    if remaining > 0:
        time.sleep(remaining)


def move_pointer(
    environment: dict[str, str],
    start: tuple[int, int],
    target: tuple[int, int],
    duration: float,
) -> tuple[int, int]:
    steps = max(1, round(duration * PLAN["fps"]))
    step_duration = duration / steps
    for step in range(1, steps + 1):
        ratio = step / steps
        eased = ratio * ratio * (3 - 2 * ratio)
        x = round(start[0] + (target[0] - start[0]) * eased)
        y = round(start[1] + (target[1] - start[1]) * eased)
        run_xdotool(environment, "mousemove", str(x), str(y))
        time.sleep(step_duration)
    return target


def run_actions(
    environment: dict[str, str],
    started_at: float,
    base_url: str,
    debug_port: int,
) -> None:
    pointer = (1450, 880)
    run_xdotool(environment, "mousemove", str(pointer[0]), str(pointer[1]))
    for action in PLAN["actions"]:
        sleep_until(started_at + float(action["at"]))
        if action["label"] in {"new-alert", "copy-rollback"}:
            wait_for_json(
                base_url,
                "/v1/alerts?limit=500",
                lambda alerts: any(
                    alert.get("package") == "mini-shai-hulud-loader"
                    and alert.get("new_version") == "1.0.1"
                    for alert in alerts
                ),
                timeout=4,
            )
        pointer = move_pointer(
            environment,
            pointer,
            (int(action["x"]), int(action["y"])),
            float(action["duration"]),
        )
        if action["click"]:
            run_xdotool(environment, "click", "1")
            time.sleep(0.12)
        if action.get("expect_hash") or action.get("expect_text"):
            wait_for_browser_state(
                debug_port,
                expected_hash=action.get("expect_hash"),
                expected_text=action.get("expect_text"),
            )


def verify_recording() -> None:
    metadata = validate_recording(PARTIAL)
    os.replace(PARTIAL, OUTPUT)
    print(
        f"Recorded {OUTPUT} ({metadata['duration']:.2f}s, "
        f"{metadata['frame_count']} frames, {metadata['size'] / 1_000_000:.1f} MB)"
    )


def main() -> None:
    require_tools()
    port = free_port()
    debug_port = free_port()
    while debug_port == port:
        debug_port = free_port()
    display = free_display()
    base_url = f"http://127.0.0.1:{port}"
    demo_process = xvfb_process = chromium_process = recorder_process = None

    with tempfile.TemporaryDirectory(prefix="goodman-walkthrough-") as temporary:
        temporary_path = Path(temporary)
        database = temporary_path / "goodman.db"
        profile = temporary_path / "chromium"
        environment = display_environment(display)
        try:
            demo_process, timer_ready, timing = start_demo(port, database)
            wait_for_json(base_url, "/v1/healthz", lambda payload: payload.get("status") == "ok")
            wait_for_json(
                base_url,
                "/v1/report",
                lambda payload: payload.get("report", {}).get("declared_count") == 1400,
            )
            if not timer_ready.wait(timeout=10):
                raise RuntimeError("demo attack timer was not announced")

            xvfb_process = start_xvfb(display)
            chromium_process = start_chromium(base_url, profile, environment, debug_port)

            target_start = (
                timing["attack_timer_started"]
                + float(PLAN["attack_delay_seconds"])
                - float(PLAN["attack_visible_at"])
            )
            if target_start < time.monotonic() - 0.5:
                raise RuntimeError("browser setup missed the synchronized recording start")
            sleep_until(target_start)

            recorder_process = start_recorder(display)
            recording_started = time.monotonic()
            run_actions(environment, recording_started, base_url, debug_port)
            sleep_until(recording_started + float(PLAN["duration_seconds"]))
            try:
                recorder_process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                stop_process(recorder_process, interrupt=True)
            recorder_errors = recorder_process.stderr.read() if recorder_process.stderr else ""
            if recorder_process.returncode != 0:
                raise RuntimeError(
                    f"ffmpeg recorder failed ({recorder_process.returncode}): {recorder_errors.strip()}"
                )
            recorder_process = None
            verify_recording()
        finally:
            stop_process(recorder_process, interrupt=True)
            stop_process(chromium_process)
            stop_process(xvfb_process)
            stop_process(demo_process)
            PARTIAL.unlink(missing_ok=True)


if __name__ == "__main__":
    main()
