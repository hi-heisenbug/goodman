import { Audio } from "@remotion/media";
import { AbsoluteFill, Series, interpolate, staticFile } from "remotion";
import { ClosingScene } from "./scenes/ClosingScene";
import { ColdOpen } from "./scenes/ColdOpen";
import { KillChainScene } from "./scenes/KillChainScene";
import { LiveAlertScene } from "./scenes/LiveAlertScene";
import { ReachabilityScene } from "./scenes/ReachabilityScene";
import { TrustScene } from "./scenes/TrustScene";
import { TurnScene } from "./scenes/TurnScene";
import {
  TOTAL_FRAMES,
  TOTAL_FRAMES_X,
  scenesFor,
  type Cut,
  type SceneId,
} from "./storyboard";

// Per-cut pacing. Walkthrough playback rates must satisfy
// segment_duration * fps / rate >= scene duration, or the video runs out and
// the frame goes black before the hard cut.
const CUT_CONFIG = {
  master: {
    total: TOTAL_FRAMES,
    audio: "audio/goodman-score.wav",
    compactColdOpen: false,
    alertRate: 0.88,
    verdictAt: 150,
    reachRate: 0.4,
    trustRate: 0.54,
  },
  x: {
    total: TOTAL_FRAMES_X,
    audio: "audio/goodman-score-x.wav",
    compactColdOpen: true,
    alertRate: 0.98,
    verdictAt: 130,
    reachRate: 0.49,
    trustRate: 0.66,
  },
} as const;

export type GoodmanDemoProps = {
  readonly cut?: Cut;
};

export const GoodmanDemo: React.FC<GoodmanDemoProps> = ({
  cut = "master",
}) => {
  const config = CUT_CONFIG[cut];
  const scenes = scenesFor(cut);
  const durationOf = (id: SceneId) => {
    const scene = scenes.find((candidate) => candidate.id === id);
    if (!scene) {
      throw new Error(`unknown scene: ${id}`);
    }
    return scene.durationInFrames;
  };

  return (
    <AbsoluteFill style={{ backgroundColor: "#0a0a0c" }}>
      <Audio
        src={staticFile(config.audio)}
        volume={(frame) =>
          interpolate(
            frame,
            [0, 32, config.total - 65, config.total - 1],
            [0, 0.5, 0.5, 0],
            { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
          )
        }
      />
      <Series>
        <Series.Sequence durationInFrames={durationOf("cold-open")}>
          <ColdOpen compact={config.compactColdOpen} />
        </Series.Sequence>
        <Series.Sequence durationInFrames={durationOf("turn")}>
          <TurnScene />
        </Series.Sequence>
        <Series.Sequence durationInFrames={durationOf("live-alert")}>
          <LiveAlertScene playbackRate={config.alertRate} />
        </Series.Sequence>
        <Series.Sequence durationInFrames={durationOf("kill-chain")}>
          <KillChainScene verdictAt={config.verdictAt} />
        </Series.Sequence>
        <Series.Sequence durationInFrames={durationOf("reachability")}>
          <ReachabilityScene playbackRate={config.reachRate} />
        </Series.Sequence>
        <Series.Sequence durationInFrames={durationOf("trust")}>
          <TrustScene playbackRate={config.trustRate} />
        </Series.Sequence>
        <Series.Sequence durationInFrames={durationOf("close")}>
          <ClosingScene />
        </Series.Sequence>
      </Series>
    </AbsoluteFill>
  );
};
