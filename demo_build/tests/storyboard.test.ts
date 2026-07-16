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

  it("references real captured dashboard screenshots", () => {
    const screenshotAssets = SCENES.flatMap((scene) => scene.screenshot ?? []);

    expect(screenshotAssets.length).toBeGreaterThanOrEqual(4);
    for (const asset of screenshotAssets) {
      expect(existsSync(resolve(__dirname, "..", "screenshots", asset))).toBe(
        true,
      );
    }
  });
});
