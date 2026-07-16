import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  FPS,
  SCENES,
  TOTAL_FRAMES,
  TRANSITION_FRAMES,
} from "../src/storyboard";

describe("Goodman demo storyboard", () => {
  it("keeps the narrative in the intended order", () => {
    expect(SCENES.map((scene) => scene.id)).toEqual([
      "cold-open",
      "attribution",
      "live-alert",
      "attack-path",
      "reachability",
      "trust",
      "close",
    ]);
  });

  it("derives the composition duration from scenes and transitions", () => {
    const sceneFrames = SCENES.reduce(
      (total, scene) => total + scene.durationInFrames,
      0,
    );
    const transitionFrames = TRANSITION_FRAMES * (SCENES.length - 1);

    expect(TOTAL_FRAMES).toBe(sceneFrames - transitionFrames);
    expect(TOTAL_FRAMES / FPS).toBeGreaterThanOrEqual(38);
    expect(TOTAL_FRAMES / FPS).toBeLessThanOrEqual(45);
  });

  it("uses one real interactive dashboard recording", () => {
    const recordingAssets = SCENES.flatMap((scene) =>
      scene.recording ? [scene.recording] : [],
    );

    expect([...new Set(recordingAssets)]).toEqual(["goodman_walkthrough.mp4"]);
    for (const asset of recordingAssets) {
      expect(existsSync(resolve(__dirname, "..", "recordings", asset))).toBe(true);
    }
  });
});
