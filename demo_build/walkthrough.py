#!/usr/bin/env python3
"""Shared interaction plan and recording validation for the Goodman demo."""

import json
import subprocess
from pathlib import Path
from typing import Any


BUILD = Path(__file__).parent
PLAN = json.loads((BUILD / "interaction_plan.json").read_text())


def validate_recording(path: Path) -> dict[str, Any]:
    result = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-select_streams",
            "v:0",
            "-count_frames",
            "-show_entries",
            "stream=codec_name,width,height,r_frame_rate,avg_frame_rate,nb_read_frames:format=duration,size",
            "-of",
            "json",
            str(path),
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    metadata = json.loads(result.stdout)
    stream = metadata["streams"][0]
    duration = float(metadata["format"]["duration"])
    size = int(metadata["format"]["size"])
    frame_count = int(stream["nb_read_frames"])
    expected_rate = f"{PLAN['fps']}/1"
    expected_frames = int(PLAN["fps"] * PLAN["duration_seconds"])

    if stream["codec_name"] != "h264":
        raise ValueError(f"unexpected walkthrough codec: {stream['codec_name']}")
    if (stream["width"], stream["height"]) != (PLAN["width"], PLAN["height"]):
        raise ValueError(f"unexpected walkthrough size: {stream['width']}x{stream['height']}")
    if stream["r_frame_rate"] != expected_rate or stream["avg_frame_rate"] != expected_rate:
        raise ValueError(
            "walkthrough is not constant-frame-rate "
            f"({stream['r_frame_rate']} nominal, {stream['avg_frame_rate']} average)"
        )
    if frame_count != expected_frames:
        raise ValueError(f"unexpected walkthrough frame count: {frame_count} != {expected_frames}")
    if abs(duration - PLAN["duration_seconds"]) > 1 / PLAN["fps"]:
        raise ValueError(f"unexpected walkthrough duration: {duration:.3f}s")
    if size < 300_000:
        raise ValueError(f"walkthrough is suspiciously small: {size:,} bytes")

    return {
        "duration": duration,
        "frame_count": frame_count,
        "size": size,
    }
