#!/usr/bin/env python3
"""Generate a deterministic original score for the Goodman demo.

The mix is timed to the storyboard: a near-silent notch under the dip-to-black
"turn" at ~8.3s, a heavier sub-bass hit when the live alert lands at ~16.8s,
and softer impacts on each scene beat after that.
"""

import math
import struct
import wave
from pathlib import Path


SAMPLE_RATE = 44_100
DURATION_SECONDS = 56.0
BPM = 100
BEAT_SECONDS = 60 / BPM

# Scene beats (seconds): kernel events, logo reveal, alert lands (weighted),
# kill-chain verdict, reachability counter, trust bento, closing headline.
IMPACT_TIMES = (
    (4.7, 1.0),
    (8.85, 1.0),
    (16.8, 1.9),
    (26.5, 1.2),
    (31.0, 1.0),
    (39.5, 1.0),
    (47.1, 1.1),
)

# The 400ms of stillness before the brand turn.
SILENCE_START = 8.15
SILENCE_END = 8.72


def envelope(time_seconds: float) -> float:
    fade_in = min(1.0, time_seconds / 1.5)
    fade_out = min(1.0, (DURATION_SECONDS - time_seconds) / 2.0)
    level = max(0.0, min(fade_in, fade_out))
    if SILENCE_START - 0.2 <= time_seconds <= SILENCE_END + 0.25:
        if time_seconds < SILENCE_START:
            dip = (SILENCE_START - time_seconds) / 0.2
        elif time_seconds > SILENCE_END:
            dip = (time_seconds - SILENCE_END) / 0.25
        else:
            dip = 0.0
        level *= 0.04 + 0.96 * max(0.0, min(1.0, dip))
    return level


def kick(time_seconds: float) -> float:
    phase = time_seconds % BEAT_SECONDS
    if phase > 0.20:
        return 0.0
    frequency = 78 - 38 * (phase / 0.20)
    return math.sin(2 * math.pi * frequency * phase) * math.exp(-phase * 19)


def impact(time_seconds: float) -> float:
    total = 0.0
    for start, weight in IMPACT_TIMES:
        phase = time_seconds - start
        if 0 <= phase <= 0.8:
            total += (
                math.sin(2 * math.pi * 49 * phase)
                * math.exp(-phase * 6)
                * weight
            )
    return total


def shimmer(sample_index: int, time_seconds: float) -> float:
    half_beat = BEAT_SECONDS / 2
    phase = time_seconds % half_beat
    if phase > 0.055:
        return 0.0
    noise = math.sin(sample_index * 12.9898) * 43_758.5453
    noise -= math.floor(noise)
    return (noise * 2 - 1) * math.exp(-phase * 58)


def sample(sample_index: int) -> tuple[int, int]:
    time_seconds = sample_index / SAMPLE_RATE
    pad = (
        math.sin(2 * math.pi * 55.0 * time_seconds) * 0.11
        + math.sin(2 * math.pi * 82.41 * time_seconds) * 0.065
        + math.sin(2 * math.pi * 110.0 * time_seconds) * 0.035
    )
    movement = math.sin(2 * math.pi * 0.22 * time_seconds) * 0.025
    transient = kick(time_seconds) * 0.24 + impact(time_seconds) * 0.23
    high = shimmer(sample_index, time_seconds) * 0.055
    mixed = (pad + movement + transient + high) * envelope(time_seconds)
    left = max(-1.0, min(1.0, mixed + high * 0.20))
    right = max(-1.0, min(1.0, mixed - high * 0.20))
    return int(left * 32_767), int(right * 32_767)


def render_score(output: Path) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    frame_count = int(SAMPLE_RATE * DURATION_SECONDS)

    with wave.open(str(output), "wb") as audio:
        audio.setnchannels(2)
        audio.setsampwidth(2)
        audio.setframerate(SAMPLE_RATE)

        chunk = bytearray()
        for sample_index in range(frame_count):
            chunk.extend(struct.pack("<hh", *sample(sample_index)))
            if len(chunk) >= 256 * 1024:
                audio.writeframesraw(chunk)
                chunk.clear()
        if chunk:
            audio.writeframesraw(chunk)


if __name__ == "__main__":
    render_score(Path(__file__).parent / "public" / "audio" / "goodman-score.wav")
