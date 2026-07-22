# Goodman Demo Build

This directory is the self-contained generation workspace for the Goodman
product demo. The canonical video is a Remotion composition built around a
real, scripted Chromium walkthrough of the Goodman dashboard, deterministic
motion graphics, and an original locally generated score. The cursor moves,
the Mini-Shai-Hulud alert arrives live, the rollback command is copied, and the
walkthrough clicks through Fingerprints, Reachability, and Coverage.

Final output:

- `goodman_demo.mp4` — 1920×1080, 30 fps, 54.7 seconds, H.264/AAC. The
  founder-sales master cut.
- `goodman_demo_x.mp4` — 1920×1080, 30 fps, 45.4 seconds, H.264/AAC. The
  X/Twitter cut: same scenes, tightened beats, its own beat-matched score.
- `recordings/goodman_walkthrough.mp4` — 20-second live dashboard interaction
  used inside the Remotion scenes.

Both cuts render from one component tree: the `GoodmanDemo` and
`GoodmanDemoX` compositions differ only in per-scene durations and the
walkthrough playback rates that keep every recording segment covering its
scene.

The film follows the hook → turn → proof → trust → close arc used by
premium dev-tool launch videos: a real-world Shai-Hulud cold open with no
logo, a dip-to-black brand turn with 400 ms of silence, the live alert
landing mid-recording with a synced screen flash, a kill-chain attribution
cascade, animated reachability counters, one bento-grid trust recap, and a
typed-terminal call to action. Motion uses critically damped springs for
entrances and reserves the single overshoot spring for verdict moments; the
canvas is a dark grid with film grain, vignette, and one radial glow per
scene.

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
- `generate_audio.py` — creates the deterministic Goodman scores (56s master,
  46s X cut) with Python's standard library; impacts land on each cut's scene
  beats, the mix dips to near-silence under the brand turn, and the
  live-alert moment carries a heavier sub-bass hit. No downloaded music or
  remote render dependency.
- `tests/` — protects scene order, timing math, interaction choreography,
  duration, and required proof assets.
Generated scratch data (`node_modules/`, `public/`, and Python bytecode) is
intentionally ignored. The final MP4 stays tracked.

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
npm run render:x  # render the 45s X/Twitter cut (goodman_demo_x.mp4)
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

1. Cold open (8.3s): the real September 2025 Shai-Hulud hook, a typed
   `npm install` that reports zero vulnerabilities, and the red kernel-event
   cascade that followed. No logo yet.
2. Turn (3.2s): dip to black, 400 ms of silence, then the first green frame —
   "Goodman watches what your code actually does."
3. Live alert (10s): the alerts segment of the real recording plays at 0.88×
   so it exactly covers the scene; the Mini-Shai-Hulud alert lands on frame
   150 with a synced screen flash and a border-beam verdict card, then the
   cursor copies the rollback command.
4. Kill chain (8.3s): package update card → four escalating behavior events →
   ATTRIBUTED verdict bar with the film's one overshoot spring.
5. Reachability (8.7s): 1,400 declared counts down to 240 executed with
   tabular numerals while the walkthrough clicks into the live report.
6. Trust (8.2s): a single bento grid — live coverage view plus fingerprint,
   coverage, and attribution counters.
7. Close (8s): typed `goodmanctl demo`, the repository URL, and a 2-second
   hold on the final frame.

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

Remotion is free for eligible small teams; organizations outside its free terms
should review the current license at <https://www.remotion.dev/license>.
