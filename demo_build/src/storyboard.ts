export const FPS = 30;

export type SceneId =
  | "cold-open"
  | "turn"
  | "live-alert"
  | "kill-chain"
  | "observe-proof"
  | "reachability"
  | "close";

export type Cut = "master" | "x";

export type StoryScene = {
  readonly id: SceneId;
  readonly durationInFrames: number;
  readonly recording?: string;
};

// Hard cuts between scenes: tempo contrast comes from inside each scene, not
// from crossfades. Scene boundaries land on score beats.
export const SCENES: readonly StoryScene[] = [
  { id: "cold-open", durationInFrames: 180 },
  { id: "turn", durationInFrames: 90 },
  {
    id: "live-alert",
    durationInFrames: 330,
    recording: "goodman_walkthrough.mp4",
  },
  { id: "kill-chain", durationInFrames: 210 },
  { id: "observe-proof", durationInFrames: 240 },
  {
    id: "reachability",
    durationInFrames: 270,
    recording: "goodman_walkthrough.mp4",
  },
  { id: "close", durationInFrames: 180 },
] as const;

// The 42s social cut: same scenes, tightened beats. Walkthrough playback
// rates in GoodmanDemo.tsx are chosen so each recording segment still covers
// its scene.
export const X_SCENES: readonly StoryScene[] = [
  { id: "cold-open", durationInFrames: 150 },
  { id: "turn", durationInFrames: 75 },
  {
    id: "live-alert",
    durationInFrames: 270,
    recording: "goodman_walkthrough.mp4",
  },
  { id: "kill-chain", durationInFrames: 180 },
  { id: "observe-proof", durationInFrames: 210 },
  {
    id: "reachability",
    durationInFrames: 225,
    recording: "goodman_walkthrough.mp4",
  },
  { id: "close", durationInFrames: 150 },
] as const;

export const scenesFor = (cut: Cut) => (cut === "x" ? X_SCENES : SCENES);

const framesOf = (scenes: readonly StoryScene[]) =>
  scenes.reduce((total, scene) => total + scene.durationInFrames, 0);

export const TOTAL_FRAMES = framesOf(SCENES);
export const TOTAL_FRAMES_X = framesOf(X_SCENES);

export const sceneStart = (id: SceneId, cut: Cut = "master"): number => {
  let start = 0;
  for (const scene of scenesFor(cut)) {
    if (scene.id === id) {
      return start;
    }
    start += scene.durationInFrames;
  }
  throw new Error(`unknown scene: ${id}`);
};
