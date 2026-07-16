export const FPS = 30;

export type SceneId =
  | "cold-open"
  | "turn"
  | "live-alert"
  | "kill-chain"
  | "reachability"
  | "trust"
  | "close";

export type StoryScene = {
  readonly id: SceneId;
  readonly durationInFrames: number;
  readonly recording?: string;
};

// Hard cuts between scenes: tempo contrast comes from inside each scene, not
// from crossfades. Scene boundaries land on score beats.
export const SCENES: readonly StoryScene[] = [
  { id: "cold-open", durationInFrames: 250 },
  { id: "turn", durationInFrames: 95 },
  {
    id: "live-alert",
    durationInFrames: 300,
    recording: "goodman_walkthrough.mp4",
  },
  { id: "kill-chain", durationInFrames: 250 },
  {
    id: "reachability",
    durationInFrames: 260,
    recording: "goodman_walkthrough.mp4",
  },
  {
    id: "trust",
    durationInFrames: 245,
    recording: "goodman_walkthrough.mp4",
  },
  { id: "close", durationInFrames: 240 },
] as const;

export const TOTAL_FRAMES = SCENES.reduce(
  (total, scene) => total + scene.durationInFrames,
  0,
);

export const sceneStart = (id: SceneId): number => {
  let start = 0;
  for (const scene of SCENES) {
    if (scene.id === id) {
      return start;
    }
    start += scene.durationInFrames;
  }
  throw new Error(`unknown scene: ${id}`);
};
