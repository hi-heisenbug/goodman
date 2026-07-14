#!/usr/bin/env python3
"""
Goodman product demo video assembler.

Output: goodman_demo.mp4  1280x720  24fps
"""
import shutil
import subprocess
import sys
import textwrap
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

BUILD = Path(__file__).parent
SCREENS = BUILD / "screenshots"
FRAMES = BUILD / "frames"
OUT = BUILD / "goodman_demo.mp4"
W, H = 1920, 1080
FPS = 24

# Brand palette from HEISENBUG.png.
BG = (251, 251, 250)
SURFACE = (255, 255, 255)
INK = (5, 5, 5)
CHARCOAL = (70, 70, 70)
MUTED = (116, 116, 116)
LINE = (222, 226, 224)
LIME = (147, 203, 82)
TURQ = (28, 151, 112)
MINT = (190, 243, 226)
BLUSH = (242, 238, 238)
RED = (180, 48, 58)
AMBER = (230, 126, 34)


def font(size, bold=False):
    candidates = [
        f"/usr/share/fonts/truetype/dejavu/DejaVuSans{'-Bold' if bold else ''}.ttf",
        f"/usr/share/fonts/truetype/liberation/LiberationSans-{'Bold' if bold else 'Regular'}.ttf",
    ]
    for candidate in candidates:
        if Path(candidate).exists():
            return ImageFont.truetype(candidate, size)
    return ImageFont.load_default()


F_BODY = font(30)
F_BODY_B = font(30, True)
F_SMALL = font(23)
F_SMALL_B = font(23, True)
F_H1 = font(82, True)
F_H2 = font(54, True)
F_H3 = font(34, True)
F_LABEL = font(22, True)


def text_width(d, text, fnt):
    return int(d.textlength(text, font=fnt))


def wrap(text, width):
    return textwrap.wrap(text, width=width, break_long_words=False)


def wrap_pixels(d, text, fnt, max_px):
    lines = []
    current = ""
    for word in text.split():
        candidate = word if not current else f"{current} {word}"
        if text_width(d, candidate, fnt) <= max_px:
            current = candidate
            continue
        if current:
            lines.append(current)
        current = word
    if current:
        lines.append(current)
    return lines


def draw_multiline(d, xy, lines, fnt, fill, spacing=12, anchor="la"):
    x, y = xy
    for line in lines:
        d.text((x, y), line, font=fnt, fill=fill, anchor=anchor)
        y += fnt.size + spacing
    return y


def base():
    img = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(img)

    # Subtle structured background, not decorative blobs.
    for y in range(0, H, 72):
        color = (246, 247, 245) if (y // 72) % 2 else (250, 250, 248)
        d.rectangle([0, y, W, y + 36], fill=color)
    for x in range(-200, W, 240):
        d.line([(x, H), (x + 520, 0)], fill=(236, 240, 237), width=1)

    d.rectangle([0, 0, W, 92], fill=SURFACE)
    d.line([(0, 92), (W, 92)], fill=LINE, width=1)
    draw_brand(d, 92, 46)
    d.text((W - 92, 46), "Runtime dependency security", font=F_SMALL_B, fill=CHARCOAL, anchor="rm")
    return img


def draw_brand(d, x, y):
    tri = [(x, y - 25), (x - 23, y + 23), (x + 23, y + 23)]
    d.line([tri[0], tri[1], tri[2], tri[0]], fill=INK, width=7, joint="curve")
    d.text((x + 44, y - 8), "GOODMAN", font=font(42, True), fill=INK, anchor="lm")
    d.text((x + 47, y + 30), "by Heisenbug", font=font(20), fill=CHARCOAL, anchor="lm")


def card(d, box, fill=SURFACE, outline=LINE, width=1):
    d.rounded_rectangle(box, radius=8, fill=fill, outline=outline, width=width)


def pill(d, x, y, text, fg=TURQ, bg=MINT):
    pad_x = 18
    tw = text_width(d, text, F_LABEL)
    d.rounded_rectangle([x, y, x + tw + pad_x * 2, y + 40], radius=20, fill=bg, outline=fg, width=1)
    d.text((x + pad_x, y + 20), text, font=F_LABEL, fill=fg, anchor="lm")
    return x + tw + pad_x * 2


def title_block(d, x, y, eyebrow, title, body, max_px=900):
    d.text((x, y), eyebrow.upper(), font=F_LABEL, fill=LIME, anchor="la")
    y += 48
    for line in wrap_pixels(d, title, F_H1, max_px):
        d.text((x, y), line, font=F_H1, fill=INK, anchor="la")
        y += 92
    y += 8
    d.rounded_rectangle([x, y, x + 78, y + 7], radius=4, fill=TURQ)
    y += 38
    return draw_multiline(d, (x, y), wrap_pixels(d, body, F_BODY, max_px), F_BODY, CHARCOAL, spacing=14)


def scene_opener():
    img = base()
    d = ImageDraw.Draw(img)
    title_block(
        d,
        140,
        205,
        "Production demo",
        "Find the package behind the syscall",
        "Goodman watches real runtime behavior, learns normal package fingerprints, and flags the dependency update that starts touching secrets or launching shells.",
        max_px=980,
    )

    x0, y0, w, h = 1220, 245, 520, 540
    card(d, [x0, y0, x0 + w, y0 + h])
    d.text((x0 + 40, y0 + 56), "Live signal", font=F_H3, fill=INK, anchor="la")
    rows = [
        ("eBPF", "captures security syscalls", TURQ),
        ("V8 stack", "maps syscall to package", LIME),
        ("Baseline", "learns package behavior", AMBER),
        ("Alert", "shows drift and rollback", RED),
    ]
    y = y0 + 132
    for label, desc, color in rows:
        d.rounded_rectangle([x0 + 40, y, x0 + 96, y + 56], radius=8, fill=color)
        d.text((x0 + 124, y + 14), label, font=F_BODY_B, fill=INK, anchor="la")
        d.text((x0 + 124, y + 48), desc, font=F_SMALL, fill=CHARCOAL, anchor="la")
        y += 92
    return img


def scene_problem():
    img = base()
    d = ImageDraw.Draw(img)
    title_block(
        d,
        110,
        176,
        "The blind spot",
        "Most tools stop at the process name",
        "A production Node.js service can import hundreds of packages. When the kernel sees a suspicious read or exec, the hard question is: which dependency caused it?",
        max_px=760,
    )

    cards = [
        ("What changed?", "mini-shai-hulud-loader 1.0.0 -> 1.0.1", "Package update", AMBER),
        ("What happened?", "Credential read + IMDS + C2 + exec", "Runtime behavior", RED),
        ("Who did it?", "Package attribution, not just PID", "Goodman's answer", TURQ),
    ]
    x = 960
    for i, (q, a, label, color) in enumerate(cards):
        y = 210 + i * 208
        card(d, [x, y, x + 780, y + 158])
        d.rounded_rectangle([x + 28, y + 30, x + 48, y + 128], radius=8, fill=color)
        d.text((x + 78, y + 38), label.upper(), font=F_SMALL_B, fill=color, anchor="la")
        d.text((x + 78, y + 76), q, font=F_H3, fill=INK, anchor="la")
        d.text((x + 78, y + 122), a, font=F_BODY, fill=CHARCOAL, anchor="la")
    return img


def scene_pipeline():
    img = base()
    d = ImageDraw.Draw(img)
    title_block(
        d,
        118,
        164,
        "How it works",
        "Kernel signal becomes package evidence",
        "Goodman keeps the path data-backed end to end: capture, attribute, baseline, diff, and act.",
        max_px=840,
    )

    steps = [
        ("1", "Capture", "eBPF records security syscalls and user stacks.", TURQ),
        ("2", "Attribute", "V8 frames resolve to package@version.", LIME),
        ("3", "Learn", "Stable behavior becomes a baseline.", AMBER),
        ("4", "Detect", "New risky behavior raises an alert.", RED),
    ]
    x0, y0 = 1080, 235
    for i, (num, head, body, color) in enumerate(steps):
        y = y0 + i * 144
        card(d, [x0, y, x0 + 670, y + 104])
        d.rounded_rectangle([x0 + 28, y + 24, x0 + 80, y + 76], radius=8, fill=color)
        d.text((x0 + 54, y + 51), num, font=F_BODY_B, fill=SURFACE, anchor="mm")
        d.text((x0 + 112, y + 28), head, font=F_BODY_B, fill=INK, anchor="la")
        d.text((x0 + 112, y + 66), body, font=F_SMALL, fill=CHARCOAL, anchor="la")
        if i < len(steps) - 1:
            d.line([(x0 + 54, y + 104), (x0 + 54, y + 144)], fill=color, width=3)
    return img


def scene_close():
    img = base()
    d = ImageDraw.Draw(img)
    title_block(
        d,
        128,
        168,
        "The outcome",
        "From package update to rollback in seconds",
        "Goodman turns a vague production incident into a concrete package-level answer operators can trust.",
        max_px=840,
    )

    metrics = [
        ("0", "app code changes", TURQ),
        ("1", "culprit package", LIME),
        ("4", "behavior diffs", AMBER),
        ("1", "rollback command", RED),
    ]
    x, y = 1040, 238
    for i, (num, label, color) in enumerate(metrics):
        bx = x + (i % 2) * 350
        by = y + (i // 2) * 210
        card(d, [bx, by, bx + 300, by + 160])
        d.text((bx + 34, by + 68), num, font=font(70, True), fill=color, anchor="lm")
        d.text((bx + 34, by + 118), label, font=F_SMALL_B, fill=CHARCOAL, anchor="la")

    px = 128
    py = 820
    for item, fg, bg in [
        ("eBPF capture", TURQ, MINT),
        ("V8 attribution", LIME, (229, 244, 205)),
        ("Behavior baselines", AMBER, (255, 236, 214)),
        ("Instant action", RED, BLUSH),
    ]:
        px = pill(d, px, py, item, fg, bg) + 14
    return img


def proof_scene(img, eyebrow, title, body, url, accent=TURQ):
    return img.resize((W, H), Image.Resampling.LANCZOS)


_frame_idx = 0


def save_frame(img):
    global _frame_idx
    img.save(FRAMES / f"f{_frame_idx:06d}.png")
    _frame_idx += 1


def write_still(img, seconds, fade=0.25):
    total = max(1, int(seconds * FPS))
    white = Image.new("RGB", (W, H), BG)
    fade_frames = int(fade * FPS)
    for i in range(total):
        alpha = 1.0
        if fade_frames and i < fade_frames:
            alpha = i / fade_frames
        elif fade_frames and i >= total - fade_frames:
            alpha = (total - i) / fade_frames
        save_frame(Image.blend(white, img, max(0, min(1, alpha))) if alpha < 1 else img.copy())


def write_zoom(img, seconds, z0=1.0, z1=1.035, anchor=(0.5, 0.5)):
    total = max(1, int(seconds * FPS))
    base_img = img.resize((W, H), Image.Resampling.LANCZOS)
    white = Image.new("RGB", (W, H), BG)
    for i in range(total):
        t = i / max(1, total - 1)
        ease = t * t * (3 - 2 * t)
        z = z0 + (z1 - z0) * ease
        cw, ch = int(W / z), int(H / z)
        ox = int((W - cw) * anchor[0])
        oy = int((H - ch) * anchor[1])
        frame = base_img.crop((ox, oy, ox + cw, oy + ch)).resize((W, H), Image.Resampling.LANCZOS)
        fade_frames = int(0.25 * FPS)
        alpha = 1.0
        if i < fade_frames:
            alpha = i / fade_frames
        elif i >= total - fade_frames:
            alpha = (total - i) / fade_frames
        save_frame(Image.blend(white, frame, max(0, min(1, alpha))) if alpha < 1 else frame)


def load_screen(name, min_bytes=80_000):
    p = SCREENS / f"{name}.png"
    if not p.exists():
        raise FileNotFoundError(f"Missing screenshot: {p}")
    size = p.stat().st_size
    if size < min_bytes:
        raise ValueError(f"{p} is only {size:,} bytes; it is probably blank or an error page")
    img = Image.open(p).convert("RGB")
    print(f"  loaded {p.name}: {size:,} bytes, {img.size[0]}x{img.size[1]}")
    return img


def encode():
    if OUT.exists():
        OUT.unlink()
    cmd = [
        "ffmpeg",
        "-y",
        "-framerate",
        str(FPS),
        "-i",
        str(FRAMES / "f%06d.png"),
        "-c:v",
        "libx264",
        "-preset",
        "medium",
        "-crf",
        "18",
        "-vf",
        "scale=1280:720",
        "-pix_fmt",
        "yuv420p",
        "-movflags",
        "+faststart",
        str(OUT),
    ]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(result.stderr[-3000:])
        sys.exit(result.returncode)


def main():
    if FRAMES.exists():
        shutil.rmtree(FRAMES)
    FRAMES.mkdir(parents=True, exist_ok=True)

    print("Loading proof screenshots...")
    alerts_seeded = load_screen("01_alerts_seeded")
    mini_shai = load_screen("02_mini_shai_hulud")
    reachability = load_screen("03_reachability")
    coverage = load_screen("04_coverage")
    fingerprints = load_screen("05_fingerprints")

    scenes = [
        ("opener", lambda: write_still(scene_opener(), 4.5)),
        ("problem", lambda: write_still(scene_problem(), 5.5)),
        (
            "seeded alert queue",
            lambda: write_still(
                proof_scene(
                    alerts_seeded,
                    "Step 1",
                    "Start with real package-attributed drift",
                    "The demo opens on an operational queue with service, package, version, and behavior evidence already populated.",
                    "dashboard: seeded alerts",
                    AMBER,
                ),
                6.0,
            ),
        ),
        (
            "Mini-Shai-Hulud alert",
            lambda: write_still(
                proof_scene(
                    mini_shai,
                    "Step 2",
                    "Mini-Shai-Hulud appears live as one package",
                    "Credential access, cloud metadata, C2, and exec drift land in one attributed CRITICAL alert.",
                    "dashboard: Mini-Shai-Hulud",
                    RED,
                ),
                7.0,
            ),
        ),
        (
            "reachability proof",
            lambda: write_still(
                proof_scene(
                    reachability,
                    "Step 3",
                    "Prioritize what shipped and actually executed",
                    "The same runtime evidence reduces 1,400 declared dependencies to 240 executed packages and highlights reachable advisories.",
                    "dashboard: reachability",
                    TURQ,
                ),
                7.0,
            ),
        ),
        ("attribution pipeline", lambda: write_still(scene_pipeline(), 5.0)),
        (
            "coverage proof",
            lambda: write_still(
                proof_scene(
                    coverage,
                    "Step 4",
                    "Know whether the signal is trustworthy",
                    "Coverage exposes sensor health, attribution quality, injection gaps, and the alert budget before operators enable blocking.",
                    "dashboard: coverage and trust",
                    RED,
                ),
                7.0,
            ),
        ),
        (
            "fingerprint library",
            lambda: write_still(
                proof_scene(
                    fingerprints,
                    "Step 5",
                    "Inspect the learned behavior library",
                    "Promoted package baselines show the reads, connects, and execs Goodman uses to explain future drift.",
                    "dashboard: fingerprint library",
                    LIME,
                ),
                6.5,
            ),
        ),
        ("close", lambda: write_still(scene_close(), 5.0)),
    ]

    for name, render in scenes:
        print(f"Encoding scene: {name}")
        render()

    print(f"Total: {_frame_idx} frames ({_frame_idx / FPS:.1f}s)")
    encode()
    print(f"SUCCESS: {OUT} ({OUT.stat().st_size / 1_000_000:.1f} MB)")


if __name__ == "__main__":
    main()
