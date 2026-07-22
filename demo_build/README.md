# Goodman Demo Build

This directory is the self-contained generation workspace for the Goodman
product demo. The canonical video is a Remotion composition built around a
real, scripted Chromium walkthrough of the Goodman dashboard, deterministic
motion graphics, and an original score generated on the build host. The walkthrough
starts with a real OpenClaw skill alert, records a second alert arriving live,
copies both rollback commands, and opens the Fingerprints, Reachability, and
Coverage views. A separate host-kernel capture proves attribution against a
real Node workload.

Final output:

- `goodman_demo.mp4`: 1920×1080, 30 fps, 50 seconds, H.264/AAC. The
  judge-facing master cut.
- `goodman_demo_x.mp4`: 1920×1080, 30 fps, 42 seconds, H.264/AAC. The
  social cut with the same proof in tighter beats.
- `recordings/goodman_walkthrough.mp4`: 22-second live dashboard interaction
  used inside the Remotion scenes.

Both cuts render from one component tree: the `GoodmanDemo` and
`GoodmanDemoX` compositions use different per-scene durations and walkthrough
playback rates that keep each recording segment covering its
scene.

The film opens on a clean npm install, then shows the kernel behavior that the
package manager missed. Goodman identifies an OpenClaw skill drift and a live
Mini-Shai-Hulud arrival, traces syscalls to the versioned package, and displays
the real-workload observe proof. Reachability and coverage show the deployment
view before the final command invites judges to repeat the proof on their own
app.

## What is here

- `src/`: Remotion composition, shared motion components, and seven scenes.
- `interaction_plan.json`: recording dimensions, synchronized scene segments,
  and the cursor/click choreography.
- `capture_walkthrough.py`: starts the real `goodmanctl demo` flow and records
  a deterministic Chromium session under Xvfb with FFmpeg and `xdotool`.
- `browser_target.mjs`: finds alert controls from rendered card text so
  capture actions survive layout changes.
- `browser_state.mjs`: reads the live page through Chrome DevTools so each
  coordinate-driven click is verified against its route and rendered text.
- `capture_observe_proof.py`: runs the real host workload through the
  privileged Docker observe path and writes sanitized attribution evidence.
- `evidence/observe_proof.json`: checked-in package, behavior, and event counts
  from that host-kernel capture.
- `walkthrough.py`: shared plan loading and strict FFprobe validation used by
  both capture and asset preparation.
- `recordings/`: canonical live product recording consumed by Remotion.
- `prepare_assets.py`: validates and copies the walkthrough into Remotion's
  generated `public/` directory.
- `generate_audio.py`: creates the deterministic Goodman scores (51s master,
  43s social cut) with Python's standard library; impacts land on each cut's
  scene beats, the mix dips to near-silence under the brand turn, and the
  live-alert moment carries a heavier sub-bass hit. No downloaded music or
  remote render dependency.
- `tests/`: protects scene order, timing math, interaction choreography,
  duration, and required proof assets.
The repo ignores generated scratch data (`node_modules/`, `public/`, and Python
bytecode). The final MP4 stays tracked.

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

Remotion Studio prints a local preview URL and leaves browser launch to you. In
a second terminal, render the canonical video:

```bash
cd demo_build
npm run render
```

Useful commands:

```bash
npm run capture   # regenerate the live Chromium walkthrough
npm run capture:observe # refresh the host-kernel attribution evidence
npm run capture:all     # refresh both proof sources
npm run render:x  # render the 42s social cut (goodman_demo_x.mp4)
npm test          # storyboard and asset contract
npm run lint      # ESLint + strict TypeScript
npm run compositions
npm run poster    # render poster.png at the live-alert scene
npm run upgrade   # upgrade aligned Remotion packages
```

Remotion packages must stay on the same exact version. The checked-in lockfile
pins Remotion 4.0.489.

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
npm run capture:all
npm run check
npm run render
npm run render:x
```

The browser capture uses a fresh port, X display, Chromium profile, and SQLite
database. It synchronizes the recording to the live attack timer, verifies both
alert cards, copied rollback states, routes, and page headings through Chrome
DevTools, then checks the output's codec, resolution, exact frame count, frame
rate, duration, and size. The observe capture starts the repository's Node
workload and runs `setup-everything.sh observe` through privileged Docker. It
stores sanitized counts and the dependency identity in
`evidence/observe_proof.json`. Both scripts stop their child processes on exit.
Adjust `interaction_plan.json` if dashboard navigation or layout changes. Keep
each action inside its intended segment and recapture before rendering.

## Storyboard

1. Cold open (6s): `npm install` reports zero vulnerabilities before kernel
   behavior appears.
2. Turn (3s): Goodman enters after a short black-frame pause.
3. Live alert (11s): the real dashboard shows OpenClaw skill drift, copies its
   rollback command, then receives and handles Mini-Shai-Hulud live.
4. Kill chain (7s): syscall events resolve to the exact package and version.
5. Observe proof (8s): a host-kernel capture attributes 99 real events to
   `good-pkg@1.0.0`.
6. Reachability (9s): the live report moves from 1,400 declared packages to 240
   observed at runtime, then opens coverage.
7. Close (6s): `goodmanctl demo`, the repository URL, and the invitation to
   prove the result on another workload.

## Interactive five-minute product demo

The recorded walkthrough uses the same live Goodman demo flow that you can run
by hand for a longer presentation:

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
