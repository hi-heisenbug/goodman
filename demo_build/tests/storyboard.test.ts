import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { FPS, SCENES, TOTAL_FRAMES, sceneStart } from "../src/storyboard";

describe("Goodman demo storyboard", () => {
  it("keeps the narrative in the intended order", () => {
    expect(SCENES.map((scene) => scene.id)).toEqual([
      "cold-open",
      "turn",
      "live-alert",
      "kill-chain",
      "reachability",
      "trust",
      "close",
    ]);
  });

  it("keeps every scene duration positive", () => {
    for (const scene of SCENES) {
      expect(scene.durationInFrames).toBeGreaterThan(0);
    }
  });

  it("derives the composition duration from hard-cut scenes", () => {
    const sceneFrames = SCENES.reduce(
      (total, scene) => total + scene.durationInFrames,
      0,
    );

    expect(TOTAL_FRAMES).toBe(sceneFrames);
    expect(TOTAL_FRAMES / FPS).toBeGreaterThanOrEqual(48);
    expect(TOTAL_FRAMES / FPS).toBeLessThanOrEqual(60);
  });

  it("computes scene start offsets that tile the composition", () => {
    let expected = 0;
    for (const scene of SCENES) {
      expect(sceneStart(scene.id)).toBe(expected);
      expected += scene.durationInFrames;
    }
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
