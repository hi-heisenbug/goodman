export const FPS = 30;

export type SceneId =
  | "cold-open"
  | "turn"
  | "live-alert"
  | "kill-chain"
  | "reachability"
  | "trust"
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

// The ~45s X/Twitter cut: same scenes, tightened beats. Walkthrough playback
// rates in GoodmanDemo.tsx are chosen so each recording segment still covers
// its scene.
export const X_SCENES: readonly StoryScene[] = [
  { id: "cold-open", durationInFrames: 190 },
  { id: "turn", durationInFrames: 88 },
  {
    id: "live-alert",
    durationInFrames: 264,
    recording: "goodman_walkthrough.mp4",
  },
  { id: "kill-chain", durationInFrames: 210 },
  {
    id: "reachability",
    durationInFrames: 210,
    recording: "goodman_walkthrough.mp4",
  },
  {
    id: "trust",
    durationInFrames: 200,
    recording: "goodman_walkthrough.mp4",
  },
  { id: "close", durationInFrames: 200 },
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
