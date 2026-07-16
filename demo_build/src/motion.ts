import { Easing, interpolate, spring } from "remotion";

// Camera moves and reveals: snappy exponential settle.
export const crisp = Easing.bezier(0.16, 1, 0.3, 1);
// Continuous drifts and crossfades.
export const editorial = Easing.bezier(0.65, 0, 0.35, 1);
// Counters.
export const expoOut = Easing.out(Easing.exp);

export const progress = (
  frame: number,
  start: number,
  duration: number,
  easing = crisp,
) =>
  interpolate(frame, [start, start + duration], [0, 1], {
    easing,
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

export const fadeWindow = (
  frame: number,
  enterStart: number,
  enterEnd: number,
  exitStart: number,
  exitEnd: number,
) =>
  interpolate(
    frame,
    [enterStart, enterEnd, exitStart, exitEnd],
    [0, 1, 1, 0],
    {
      easing: editorial,
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    },
  );

// Critically damped entry: no bounce. The default for UI elements.
export const springIn = (frame: number, fps: number, delay = 0) =>
  spring({
    frame: frame - delay,
    fps,
    config: { damping: 200, stiffness: 100, mass: 0.8 },
  });

// One subtle overshoot. Reserved for the verdict moments only.
export const verdictPop = (frame: number, fps: number, delay = 0) =>
  spring({
    frame: frame - delay,
    fps,
    config: { damping: 14, stiffness: 160 },
  });

export const countUp = (
  frame: number,
  start: number,
  duration: number,
  target: number,
) => {
  // Easing.exp never quite reaches 1, so snap once the window has elapsed.
  if (frame >= start + duration) {
    return target;
  }
  return Math.round(
    interpolate(frame, [start, start + duration], [0, target], {
      easing: expoOut,
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    }),
  );
};
