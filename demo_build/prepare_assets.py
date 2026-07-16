#!/usr/bin/env python3
"""Prepare the live Goodman walkthrough and audio for Remotion."""

import shutil
from pathlib import Path

from generate_audio import MASTER, XCUT, render_score
from walkthrough import PLAN, validate_recording


BUILD = Path(__file__).parent
RECORDINGS = BUILD / "recordings"
PUBLIC = BUILD / "public"
WALKTHROUGH = PLAN["output"]


def copy_walkthrough() -> None:
    source = RECORDINGS / WALKTHROUGH
    if not source.exists():
        raise FileNotFoundError(
            f"missing interactive recording: {source} (run `python3 capture_walkthrough.py`)"
        )
    validate_recording(source)
    destination = PUBLIC / "recordings"
    destination.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, destination / WALKTHROUGH)


def main() -> None:
    copy_walkthrough()
    render_score(PUBLIC / "audio" / "goodman-score.wav", MASTER)
    render_score(PUBLIC / "audio" / "goodman-score-x.wav", XCUT)
    print(f"Prepared Remotion assets in {PUBLIC}")


if __name__ == "__main__":
    main()
