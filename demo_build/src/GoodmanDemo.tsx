import { Audio } from "@remotion/media";
import { TransitionSeries, linearTiming } from "@remotion/transitions";
import { fade } from "@remotion/transitions/fade";
import { slide } from "@remotion/transitions/slide";
import { AbsoluteFill, interpolate, staticFile } from "remotion";
import { AttackPathScene } from "./scenes/AttackPathScene";
import { AttributionScene } from "./scenes/AttributionScene";
import { ClosingScene } from "./scenes/ClosingScene";
import { ColdOpen } from "./scenes/ColdOpen";
import { LiveAlertScene } from "./scenes/LiveAlertScene";
import { ReachabilityScene } from "./scenes/ReachabilityScene";
import { TrustScene } from "./scenes/TrustScene";
import {
  SCENES,
  TOTAL_FRAMES,
  TRANSITION_FRAMES,
  type SceneId,
} from "./storyboard";

const durationOf = (id: SceneId) => {
  const scene = SCENES.find((candidate) => candidate.id === id);
  if (!scene) {
    throw new Error(`unknown scene: ${id}`);
  }
  return scene.durationInFrames;
};

const timing = linearTiming({ durationInFrames: TRANSITION_FRAMES });

export const GoodmanDemo: React.FC = () => (
  <AbsoluteFill>
    <Audio
      src={staticFile("audio/goodman-score.wav")}
      volume={(frame) =>
        interpolate(
          frame,
          [0, 32, TOTAL_FRAMES - 65, TOTAL_FRAMES - 1],
          [0, 0.46, 0.46, 0],
          { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
        )
      }
    />
    <TransitionSeries>
      <TransitionSeries.Sequence durationInFrames={durationOf("cold-open")}>
        <ColdOpen />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition presentation={fade()} timing={timing} />

      <TransitionSeries.Sequence durationInFrames={durationOf("attribution")}>
        <AttributionScene />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={slide({ direction: "from-bottom" })}
        timing={timing}
      />

      <TransitionSeries.Sequence durationInFrames={durationOf("live-alert")}>
        <LiveAlertScene />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition presentation={fade()} timing={timing} />

      <TransitionSeries.Sequence durationInFrames={durationOf("attack-path")}>
        <AttackPathScene />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={slide({ direction: "from-right" })}
        timing={timing}
      />

      <TransitionSeries.Sequence durationInFrames={durationOf("reachability")}>
        <ReachabilityScene />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition presentation={fade()} timing={timing} />

      <TransitionSeries.Sequence durationInFrames={durationOf("trust")}>
        <TrustScene />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={slide({ direction: "from-bottom" })}
        timing={timing}
      />

      <TransitionSeries.Sequence durationInFrames={durationOf("close")}>
        <ClosingScene />
      </TransitionSeries.Sequence>
    </TransitionSeries>
  </AbsoluteFill>
);
