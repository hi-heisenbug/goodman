# Goodman Demo Build

This directory contains the Goodman product demo assets.

Final outputs:

- `goodman_demo.mp4` — final 1080p product demo, 50 seconds, 24 fps.

Reusable inputs:

- `backdoor_preview.html` — light Goodman-styled malicious-update evidence scene.
- `inject_demo.py` — injects realistic baseline and drift data into a local collector.
- `capture_screens.py` — starts the collector, injects data, captures screenshots, and stops the collector.
- `assemble.py` — assembles proof screenshots and designed story scenes into
  the final video.
- `screenshots/` — selected live proof captures used by the assembler.

Story structure:

1. Product opener: what Goodman does.
2. Blind spot: why process-level security tooling is not enough.
3. Malicious update: compromised dependency evidence.
4. Attribution pipeline: eBPF capture to package-level alert.
5. Fingerprint proof: learned runtime behavior baselines.
6. Alert proof: drift, severity, and rollback action.
7. Outcome: package update to rollback in seconds.

Regenerate the demo from a clean repo:

```bash
make dashboard
make build
python3 demo_build/capture_screens.py
python3 demo_build/assemble.py
```

Do not commit `frames/`, Chromium profiles, local SQLite DB files, `nohup.out`,
temporary collector logs, or extra resized copies unless they are explicitly
requested. Keep one canonical final video by default.
