#!/usr/bin/env python3
"""Prepare and validate the real Goodman evidence used by Remotion."""

import json
import shutil
from pathlib import Path

from generate_audio import MASTER, XCUT, render_score
from walkthrough import PLAN, validate_recording


BUILD = Path(__file__).parent
RECORDINGS = BUILD / "recordings"
PUBLIC = BUILD / "public"
WALKTHROUGH = PLAN["output"]
OBSERVE_PROOF = BUILD / "evidence" / "observe_proof.json"


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


def validate_observe_proof() -> None:
    proof = json.loads(OBSERVE_PROOF.read_text())
    if proof.get("schema") != "goodman.demo.observe-proof/v1":
        raise ValueError("unexpected observe-proof schema")
    if proof.get("package") != "good-pkg" or proof.get("version") != "1.0.0":
        raise ValueError("observe proof does not name good-pkg@1.0.0")
    if proof.get("events", 0) <= 0 or proof.get("exact_dependency_events") != proof.get("events"):
        raise ValueError("observe proof does not contain exact dependency events")
    if "/home/" in proof.get("behavior", ""):
        raise ValueError("observe proof leaks a local path")


def main() -> None:
    copy_walkthrough()
    validate_observe_proof()
    render_score(PUBLIC / "audio" / "goodman-score.wav", MASTER)
    render_score(PUBLIC / "audio" / "goodman-score-x.wav", XCUT)
    print(f"Prepared Remotion assets in {PUBLIC}")


if __name__ == "__main__":
    main()
