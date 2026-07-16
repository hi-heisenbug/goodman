import { Audio } from "@remotion/media";
import { AbsoluteFill, Series, interpolate, staticFile } from "remotion";
import { ClosingScene } from "./scenes/ClosingScene";
import { ColdOpen } from "./scenes/ColdOpen";
import { KillChainScene } from "./scenes/KillChainScene";
import { LiveAlertScene } from "./scenes/LiveAlertScene";
import { ReachabilityScene } from "./scenes/ReachabilityScene";
import { TrustScene } from "./scenes/TrustScene";
import { TurnScene } from "./scenes/TurnScene";
import { SCENES, TOTAL_FRAMES, type SceneId } from "./storyboard";

const durationOf = (id: SceneId) => {
  const scene = SCENES.find((candidate) => candidate.id === id);
  if (!scene) {
    throw new Error(`unknown scene: ${id}`);
  }
  return scene.durationInFrames;
};

export const GoodmanDemo: React.FC = () => (
  <AbsoluteFill style={{ backgroundColor: "#0a0a0c" }}>
    <Audio
      src={staticFile("audio/goodman-score.wav")}
      volume={(frame) =>
        interpolate(
          frame,
          [0, 32, TOTAL_FRAMES - 65, TOTAL_FRAMES - 1],
          [0, 0.5, 0.5, 0],
          { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
        )
      }
    />
    <Series>
      <Series.Sequence durationInFrames={durationOf("cold-open")}>
        <ColdOpen />
      </Series.Sequence>
      <Series.Sequence durationInFrames={durationOf("turn")}>
        <TurnScene />
      </Series.Sequence>
      <Series.Sequence durationInFrames={durationOf("live-alert")}>
        <LiveAlertScene />
      </Series.Sequence>
      <Series.Sequence durationInFrames={durationOf("kill-chain")}>
        <KillChainScene />
      </Series.Sequence>
      <Series.Sequence durationInFrames={durationOf("reachability")}>
        <ReachabilityScene />
      </Series.Sequence>
      <Series.Sequence durationInFrames={durationOf("trust")}>
        <TrustScene />
      </Series.Sequence>
      <Series.Sequence durationInFrames={durationOf("close")}>
        <ClosingScene />
      </Series.Sequence>
    </Series>
  </AbsoluteFill>
);
