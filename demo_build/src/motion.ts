import { Easing, interpolate } from "remotion";

export const crisp = Easing.bezier(0.16, 1, 0.3, 1);
export const editorial = Easing.bezier(0.45, 0, 0.55, 1);
export const pop = Easing.bezier(0.34, 1.36, 0.64, 1);

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
