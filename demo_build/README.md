# Goodman Demo Build

This directory is the self-contained generation workspace for the Goodman
product demo. The canonical video is a Remotion composition built around a
real, scripted Chromium walkthrough of the Goodman dashboard, deterministic
motion graphics, and an original locally generated score. The cursor moves,
the Mini-Shai-Hulud alert arrives live, the rollback command is copied, and the
walkthrough clicks through Fingerprints, Reachability, and Coverage.

Final output:

- `goodman_demo.mp4` — 1920×1080, 30 fps, 41.9 seconds, H.264/AAC.
- `recordings/goodman_walkthrough.mp4` — 20-second live dashboard interaction
  used inside the Remotion scenes.

## What is here

- `src/` — Remotion composition, shared motion components, and seven scenes.
- `interaction_plan.json` — recording dimensions, synchronized scene segments,
  and the cursor/click choreography.
- `capture_walkthrough.py` — starts the real `goodmanctl demo` flow and records
  a deterministic Chromium session under Xvfb with FFmpeg and `xdotool`.
- `browser_state.mjs` — reads the live page through Chrome DevTools so every
  coordinate-driven click is verified against its route and rendered text.
- `walkthrough.py` — shared plan loading and strict FFprobe validation used by
  both capture and asset preparation.
- `recordings/` — canonical live product recording consumed by Remotion.
- `prepare_assets.py` — validates and copies the walkthrough into Remotion's
  generated `public/` directory.
- `generate_audio.py` — creates the deterministic 44-second Goodman score with
  Python's standard library; no downloaded music or remote render dependency.
- `tests/` — protects scene order, timing math, interaction choreography,
  duration, and required proof assets.
- `screenshots/` and `capture_screens.py` — preserved static-capture workflow
  for the legacy renderer and release comparison; they are no longer used by
  the canonical Remotion composition.
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
npm ci
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
npm run capture   # regenerate the live Chromium walkthrough
npm test          # storyboard and asset contract
npm run lint      # ESLint + strict TypeScript
npm run compositions
npm run poster    # render poster.png at the live-alert scene
npm run upgrade   # upgrade aligned Remotion packages
```

Remotion packages must stay on the same exact version. The checked-in lockfile
currently pins Remotion 4.0.489.

## Refresh the interactive product proof

The walkthrough is served by the actual Goodman collector and dashboard, not a
mock UI. Regenerate it before a release when the dashboard, navigation, or demo
seed changes. The capture host needs `Xvfb`, Chromium, FFmpeg/FFprobe, and
`xdotool` on `PATH`. On Debian, Ubuntu, or Kali:

```bash
sudo apt-get install -y chromium xvfb ffmpeg xdotool
```

```bash
make dashboard
make build
cd demo_build
npm run capture
npm run check
npm run render
```

The capture script uses a fresh port, X display, Chromium profile, and SQLite
database. It synchronizes the recording to the live attack timer, verifies the
alert arrival, copied rollback state, routes, and page headings through Chrome
DevTools, then checks the output's codec, resolution, exact frame count, frame
rate, duration, and size. Every process is stopped on exit. Adjust
`interaction_plan.json` if dashboard navigation or layout changes; keep every
action inside its intended segment and recapture before rendering.

## Storyboard

1. Cold open: a routine dependency update becomes suspicious syscall drift.
2. Attribution: kernel event → user stack → package@version → drift alert.
3. Live Mini-Shai-Hulud alert: the alert lands during a real browser session,
   then the cursor copies the package-specific rollback command.
4. Attack path: four new behaviors grouped under the single package update.
5. Reachability: the walkthrough clicks into the report as 1,400 declared
   dependencies collapse to 240 executed packages.
6. Trust: the cursor moves through promoted behavior fingerprints and live
   coverage health to establish confidence in the signal.
7. Close: package update to rollback, followed by the Goodman call to action.

## Interactive five-minute product demo

The recorded walkthrough uses the same live Goodman demo flow that can also be
run manually for a longer presentation:

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
