# Goodman Demo Build

This directory contains the Goodman product demo assets.

Final outputs:

- `goodman_demo.mp4` — final 720p Mini-Shai-Hulud product demo, 53.5 seconds,
  24 fps.

Reusable inputs:

- `capture_screens.py` — starts the real `goodmanctl demo` Mini-Shai-Hulud
  flow, captures bounded headless Chromium screenshots, and stops all local
  processes.
- `assemble.py` — assembles proof screenshots and designed story scenes into
  the final video.
- `screenshots/` — sequential live proof captures used by the assembler:
  seeded alerts, the live Mini-Shai-Hulud alert, Reachability, Coverage, and
  Fingerprints.

## Interactive five-minute wow

```bash
make demo
# or: goodmanctl demo [-port 8844] [-attack-delay 12s]
```

What it does (no root, no cluster):

1. Starts a local collector + embedded dashboard on `http://127.0.0.1:8844`
2. Seeds multi-service fingerprints and CRITICAL drift alerts
3. Persists a reachability snapshot: **1,400 declared / 240 executed**
4. Prints a 60-second guided script
5. After `-attack-delay` (default 12s), replays the 2026 Mini-Shai-Hulud
   behavior profile so a new CRITICAL alert appears live with rule chips

Non-interactive DoD check:

```bash
make demo-check
```

Override host/port/db with `GOODMAN_DEMO_HOST`, `GOODMAN_DEMO_PORT`,
`GOODMAN_DEMO_DB`, or the matching `goodmanctl demo` flags.

## Video regenerate

```bash
make dashboard
make build
python3 demo_build/capture_screens.py
python3 demo_build/assemble.py
```

Do not commit `frames/`, Chromium profiles, local SQLite DB files, `nohup.out`,
temporary collector logs, or extra resized copies unless they are explicitly
requested. Keep one canonical final video by default.
