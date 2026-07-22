#!/usr/bin/env python3
"""Generate deterministic original scores for the Goodman demo cuts.

Each profile is timed to its storyboard: a near-silent notch under the
dip-to-black "turn", a heavier sub-bass hit when the live alert lands, and
softer impacts on the other scene beats.
"""

import math
import struct
import wave
from pathlib import Path


SAMPLE_RATE = 44_100
BPM = 100
BEAT_SECONDS = 60 / BPM


class ScoreProfile:
    """Beat map for one cut: (impact_time_seconds, weight) pairs plus the
    silence notch under the brand turn."""

    def __init__(
        self,
        duration_seconds: float,
        impacts: tuple[tuple[float, float], ...],
        silence: tuple[float, float],
    ) -> None:
        self.duration_seconds = duration_seconds
        self.impacts = impacts
        self.silence = silence


# 50s master: clean-install hook, kernel turn, OpenClaw proof, live attack,
# package verdict, real-workload proof, reachability/coverage, close.
MASTER = ScoreProfile(
    duration_seconds=51.0,
    impacts=(
        (1.7, 0.9),
        (4.2, 1.2),
        (6.2, 1.0),
        (10.5, 1.2),
        (15.05, 1.9),
        (24.0, 1.4),
        (29.0, 1.0),
        (33.4, 1.6),
        (38.0, 1.0),
        (42.2, 1.1),
        (45.0, 1.2),
    ),
    silence=(5.85, 6.35),
)

# 42s X cut: the same proof beats on the tightened storyboard.
XCUT = ScoreProfile(
    duration_seconds=43.0,
    impacts=(
        (1.3, 0.9),
        (3.5, 1.2),
        (5.2, 1.0),
        (8.8, 1.2),
        (12.4, 1.9),
        (20.0, 1.4),
        (24.0, 1.0),
        (28.0, 1.6),
        (32.0, 1.0),
        (36.2, 1.1),
        (37.5, 1.2),
    ),
    silence=(4.85, 5.25),
)


def envelope(time_seconds: float, profile: ScoreProfile) -> float:
    fade_in = min(1.0, time_seconds / 1.5)
    fade_out = min(1.0, (profile.duration_seconds - time_seconds) / 2.0)
    level = max(0.0, min(fade_in, fade_out))
    silence_start, silence_end = profile.silence
    if silence_start - 0.2 <= time_seconds <= silence_end + 0.25:
        if time_seconds < silence_start:
            dip = (silence_start - time_seconds) / 0.2
        elif time_seconds > silence_end:
            dip = (time_seconds - silence_end) / 0.25
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


def impact(time_seconds: float, profile: ScoreProfile) -> float:
    total = 0.0
    for start, weight in profile.impacts:
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


def sample(sample_index: int, profile: ScoreProfile) -> tuple[int, int]:
    time_seconds = sample_index / SAMPLE_RATE
    pad = (
        math.sin(2 * math.pi * 55.0 * time_seconds) * 0.11
        + math.sin(2 * math.pi * 82.41 * time_seconds) * 0.065
        + math.sin(2 * math.pi * 110.0 * time_seconds) * 0.035
    )
    movement = math.sin(2 * math.pi * 0.22 * time_seconds) * 0.025
    transient = kick(time_seconds) * 0.24 + impact(time_seconds, profile) * 0.23
    high = shimmer(sample_index, time_seconds) * 0.055
    mixed = (pad + movement + transient + high) * envelope(time_seconds, profile)
    left = max(-1.0, min(1.0, mixed + high * 0.20))
    right = max(-1.0, min(1.0, mixed - high * 0.20))
    return int(left * 32_767), int(right * 32_767)


def render_score(output: Path, profile: ScoreProfile = MASTER) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    frame_count = int(SAMPLE_RATE * profile.duration_seconds)

    with wave.open(str(output), "wb") as audio:
        audio.setnchannels(2)
        audio.setsampwidth(2)
        audio.setframerate(SAMPLE_RATE)

        chunk = bytearray()
        for sample_index in range(frame_count):
            chunk.extend(struct.pack("<hh", *sample(sample_index, profile)))
            if len(chunk) >= 256 * 1024:
                audio.writeframesraw(chunk)
                chunk.clear()
        if chunk:
            audio.writeframesraw(chunk)


if __name__ == "__main__":
    audio_dir = Path(__file__).parent / "public" / "audio"
    render_score(audio_dir / "goodman-score.wav", MASTER)
    render_score(audio_dir / "goodman-score-x.wav", XCUT)
