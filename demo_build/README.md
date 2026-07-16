# Goodman Demo Build

This directory is the self-contained generation workspace for the Goodman
product demo. The canonical video is now a Remotion composition built from real
Goodman dashboard captures plus deterministic motion graphics and an original,
locally generated score.

Final output:

- `goodman_demo.mp4` — 1920×1080, 30 fps, 41.9 seconds, H.264/AAC.

## What is here

- `src/` — Remotion composition, shared motion components, and seven scenes.
- `screenshots/` — live dashboard proof captures used by the composition.
- `capture_screens.py` — starts the real `goodmanctl demo` flow, captures
  bounded headless Chromium screenshots, and stops every local process.
- `prepare_assets.py` — validates and copies captures into Remotion's generated
  `public/` directory.
- `generate_audio.py` — creates the deterministic 44-second Goodman score with
  Python's standard library; no downloaded music or remote render dependency.
- `tests/storyboard.test.ts` — protects scene order, timing math, duration, and
  required proof assets.
- `assemble.py` — preserved legacy Pillow/FFmpeg slideshow assembler. It is not
  the canonical renderer, but remains available for comparison and fallback.

Generated scratch data (`node_modules/`, `public/`, Python bytecode, and legacy
`frames/`) is intentionally ignored. The final MP4 stays tracked.

The tiny nested `go.mod` is intentional: it prevents the repository root's
`go test ./...` from walking into npm packages that happen to ship Go sources.

## Fast path: preview and render

From the repository root:

```bash
cd demo_build
npm install
npm run check
npm run dev
```

Remotion Studio prints a local preview URL and does not open a browser
automatically. In a second terminal, render the canonical video:

```bash
cd demo_build
npm run render
```

Useful commands:

```bash
npm test          # storyboard and asset contract
npm run lint      # ESLint + strict TypeScript
npm run compositions
npm run poster    # render poster.png at the live-alert scene
npm run upgrade   # upgrade aligned Remotion packages
```

Remotion packages must stay on the same exact version. The checked-in lockfile
currently pins Remotion 4.0.489.

## Refresh the live product proof

The screenshots are real-shaped product data served by the actual collector and
dashboard, not mock UI. Regenerate them before a release when the dashboard or
demo seed changes:

```bash
make dashboard
make build
python3 demo_build/capture_screens.py
cd demo_build
npm run check
npm run render
```

The capture script uses a fresh port, temporary Chromium profile, and temporary
SQLite database. It verifies each screenshot is large enough to be a real page
and cleans up the demo process and database on exit.

## Storyboard

1. Cold open: a routine dependency update becomes suspicious syscall drift.
2. Attribution: kernel event → user stack → package@version → drift alert.
3. Live Mini-Shai-Hulud alert: guided zoom into the real package-level evidence.
4. Attack path: four new behaviors grouped under the single package update.
5. Reachability: 1,400 declared dependencies collapse to 240 executed packages.
6. Trust: coverage health and promoted behavior baselines establish confidence.
7. Close: package update to rollback, followed by the Goodman call to action.

## Interactive five-minute product demo

The video build is separate from Goodman's live interactive demo:

```bash
make demo
# or: goodmanctl demo [-port 8844] [-attack-delay 12s]
```

Non-interactive definition-of-done check:

```bash
make demo-check
```

The live demo starts a local collector and dashboard, seeds package fingerprints
and CRITICAL alerts, persists the 1,400/240 reachability snapshot, and replays the
Mini-Shai-Hulud behavior profile after the configured delay.

## Legacy renderer

The original static assembler is preserved unchanged:

```bash
python3 demo_build/assemble.py
```

It writes the same `goodman_demo.mp4` path, so running it replaces the Remotion
render. Run `npm run render` afterward to restore the canonical video.

Remotion is free for eligible small teams; organizations outside its free terms
should review the current license at <https://www.remotion.dev/license>.
