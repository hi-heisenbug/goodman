export const FPS = 30;
export const TRANSITION_FRAMES = 18;

export type SceneId =
  | "cold-open"
  | "attribution"
  | "live-alert"
  | "attack-path"
  | "reachability"
  | "trust"
  | "close";

export type StoryScene = {
  readonly id: SceneId;
  readonly durationInFrames: number;
  readonly screenshot?: readonly string[];
};

export const SCENES: readonly StoryScene[] = [
  { id: "cold-open", durationInFrames: 165 },
  { id: "attribution", durationInFrames: 195 },
  {
    id: "live-alert",
    durationInFrames: 240,
    screenshot: ["02_mini_shai_hulud.png"],
  },
  {
    id: "attack-path",
    durationInFrames: 165,
    screenshot: ["01_alerts_seeded.png"],
  },
  {
    id: "reachability",
    durationInFrames: 210,
    screenshot: ["03_reachability.png"],
  },
  {
    id: "trust",
    durationInFrames: 210,
    screenshot: ["04_coverage.png", "05_fingerprints.png"],
  },
  { id: "close", durationInFrames: 180 },
] as const;

const sceneFrames = SCENES.reduce(
  (total, scene) => total + scene.durationInFrames,
  0,
);

export const TOTAL_FRAMES =
  sceneFrames - TRANSITION_FRAMES * (SCENES.length - 1);
