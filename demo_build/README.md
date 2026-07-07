# Goodman Demo Build

This directory contains the Goodman product demo assets.

Final outputs:

- `goodman_demo.mp4` — final 720p product demo, 54 seconds, 24 fps.

Reusable inputs:

- `backdoor_preview.html` — light Goodman-styled malicious-update evidence scene.
- `inject_demo.py` — injects realistic baseline and drift data into a local collector.
- `capture_screens.py` — starts the collector, injects data, captures screenshots, and stops the collector.
- `assemble.py` — assembles proof screenshots and designed story scenes into
  the final video.
- `screenshots/` — sequential live proof captures used by the assembler:
  `01_malicious_update.png`, `02_alerts_open.png`,
  `03_fingerprints_all.png`, `04_alerts_triaged.png`, and
  `05_fingerprints_learning.png`.

Story structure:

1. Product opener: what Goodman does.
2. Blind spot: why process-level security tooling is not enough.
3. Malicious update: compromised dependency evidence.
4. Open alert queue: live package drift review in the product.
5. Fingerprint proof: learned runtime behavior baselines.
6. Attribution pipeline: eBPF capture to package-level alert.
7. Triage proof: acknowledged and resolved alerts from real API updates.
8. Learning filter: packages still gathering runtime observations.
9. Outcome: package update to rollback in seconds.

Regenerate the demo from a clean repo:

```bash
make dashboard
make build
python3 demo_build/capture_screens.py
python3 demo_build/assemble.py
```

To run the interactive product demo locally with seeded data:

```bash
make demo
```

Use `GOODMAN_DEMO_PORT`, `GOODMAN_DEMO_HOST`, `GOODMAN_DEMO_DB`,
`GOODMAN_DEMO_LEARN_OBS`, and `GOODMAN_DEMO_LEARN_MIN_AGE` to override the
default local demo settings.

Do not commit `frames/`, Chromium profiles, local SQLite DB files, `nohup.out`,
temporary collector logs, or extra resized copies unless they are explicitly
requested. Keep one canonical final video by default.
