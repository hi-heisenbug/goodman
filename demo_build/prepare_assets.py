#!/usr/bin/env python3
"""Prepare captured Goodman proof assets for the Remotion renderer."""

import shutil
from pathlib import Path

from generate_audio import render_score


BUILD = Path(__file__).parent
SCREENSHOTS = BUILD / "screenshots"
PUBLIC = BUILD / "public"

REQUIRED_SCREENSHOTS = (
    "01_alerts_seeded.png",
    "02_mini_shai_hulud.png",
    "03_reachability.png",
    "04_coverage.png",
    "05_fingerprints.png",
)


def copy_screenshots() -> None:
    destination = PUBLIC / "screenshots"
    destination.mkdir(parents=True, exist_ok=True)

    for filename in REQUIRED_SCREENSHOTS:
        source = SCREENSHOTS / filename
        if not source.exists():
            raise FileNotFoundError(f"missing captured dashboard screenshot: {source}")
        if source.stat().st_size < 80_000:
            raise ValueError(f"{source} looks blank ({source.stat().st_size:,} bytes)")
        shutil.copy2(source, destination / filename)


def main() -> None:
    copy_screenshots()
    render_score(PUBLIC / "audio" / "goodman-score.wav")
    print(f"Prepared Remotion assets in {PUBLIC}")


if __name__ == "__main__":
    main()
