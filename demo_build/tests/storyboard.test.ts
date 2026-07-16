import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  FPS,
  SCENES,
  TOTAL_FRAMES,
  TOTAL_FRAMES_X,
  X_SCENES,
  sceneStart,
  scenesFor,
  type Cut,
} from "../src/storyboard";

const SCENE_ORDER = [
  "cold-open",
  "turn",
  "live-alert",
  "kill-chain",
  "reachability",
  "trust",
  "close",
];

const CUTS: readonly {
  cut: Cut;
  scenes: typeof SCENES;
  total: number;
  minSeconds: number;
  maxSeconds: number;
}[] = [
  {
    cut: "master",
    scenes: SCENES,
    total: TOTAL_FRAMES,
    minSeconds: 48,
    maxSeconds: 60,
  },
  {
    cut: "x",
    scenes: X_SCENES,
    total: TOTAL_FRAMES_X,
    minSeconds: 40,
    maxSeconds: 48,
  },
];

describe.each(CUTS)(
  "Goodman demo storyboard ($cut cut)",
  ({ cut, scenes, total, minSeconds, maxSeconds }) => {
    it("keeps the narrative in the intended order", () => {
      expect(scenes.map((scene) => scene.id)).toEqual(SCENE_ORDER);
      expect(scenesFor(cut)).toBe(scenes);
    });

    it("keeps every scene duration positive", () => {
      for (const scene of scenes) {
        expect(scene.durationInFrames).toBeGreaterThan(0);
      }
    });

    it("derives the composition duration from hard-cut scenes", () => {
      const sceneFrames = scenes.reduce(
        (sum, scene) => sum + scene.durationInFrames,
        0,
      );

      expect(total).toBe(sceneFrames);
      expect(total / FPS).toBeGreaterThanOrEqual(minSeconds);
      expect(total / FPS).toBeLessThanOrEqual(maxSeconds);
    });

    it("computes scene start offsets that tile the composition", () => {
      let expected = 0;
      for (const scene of scenes) {
        expect(sceneStart(scene.id, cut)).toBe(expected);
        expected += scene.durationInFrames;
      }
    });

    it("uses one real interactive dashboard recording", () => {
      const recordingAssets = scenes.flatMap((scene) =>
        scene.recording ? [scene.recording] : [],
      );

      expect([...new Set(recordingAssets)]).toEqual([
        "goodman_walkthrough.mp4",
      ]);
      for (const asset of recordingAssets) {
        expect(existsSync(resolve(__dirname, "..", "recordings", asset))).toBe(
          true,
        );
      }
    });
  },
);
