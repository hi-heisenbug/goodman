import { AbsoluteFill, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { BrandMark } from "../components/BrandMark";
import { CounterStat } from "../components/CounterStat";
import { KineticHeadline } from "../components/KineticHeadline";
import { SceneLabel } from "../components/SceneLabel";
import { WalkthroughFrame } from "../components/WalkthroughFrame";
import { progress, springIn } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

type ReachabilitySceneProps = {
  readonly playbackRate?: number;
};

export const ReachabilityScene: React.FC<ReachabilitySceneProps> = ({
  playbackRate = 0.4,
}) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const body = progress(frame, 26, 20);
  const arrow = progress(frame, 66, 14);
  const chip = springIn(frame, fps, 128);

  return (
    <AbsoluteFill>
      <Backdrop accent={COLORS.green} glowX="26%" glowY="45%" />
      <div style={{ position: "absolute", left: SAFE_X, top: 62 }}>
        <BrandMark compact />
      </div>

      <div style={{ position: "absolute", left: SAFE_X, top: 208, width: 600 }}>
        <SceneLabel>Runtime reachability</SceneLabel>
        <div style={{ marginTop: 18 }}>
          <KineticHeadline
            text="Stop chasing what never executed."
            frame={frame}
            startAt={6}
            fontSize={72}
            maxWidth={600}
            accentWords={["never", "executed"]}
            accentColor={COLORS.lime}
          />
        </div>
        <div
          style={{
            marginTop: 28,
            color: COLORS.muted,
            fontFamily: FONTS.body,
            fontSize: 24,
            lineHeight: 1.55,
            opacity: body,
          }}
        >
          Goodman joins the lockfile with observed runtime behavior, so the
          vulnerable packages that actually ran rise to the top.
        </div>

        <div
          style={{
            marginTop: 52,
            display: "flex",
            alignItems: "flex-start",
            gap: 34,
          }}
        >
          <CounterStat
            frame={frame}
            startAt={34}
            durationInFrames={34}
            target={1400}
            label="declared"
            fontSize={92}
          />
          <div
            style={{
              color: COLORS.green,
              fontSize: 52,
              marginTop: 16,
              opacity: arrow,
            }}
          >
            →
          </div>
          <CounterStat
            frame={frame}
            startAt={68}
            durationInFrames={38}
            target={240}
            label="actually executed"
            color={COLORS.lime}
            fontSize={92}
          />
        </div>

        <div
          style={{
            marginTop: 40,
            display: "inline-flex",
            alignItems: "center",
            gap: 14,
            padding: "16px 24px",
            borderRadius: 10,
            backgroundColor: "rgba(147,203,82,0.12)",
            border: "1px solid rgba(147,203,82,0.35)",
            color: COLORS.white,
            fontFamily: FONTS.body,
            fontWeight: 700,
            fontSize: 23,
            opacity: Math.min(1, chip * 1.3),
            translate: `0px ${(1 - chip) * 16}px`,
          }}
        >
          <span style={{ color: COLORS.lime, fontSize: 32 }}>83%</span>
          less dependency noise
        </div>
      </div>

      <div style={{ position: "absolute", left: 790, top: 240 }}>
        <WalkthroughFrame
          segment="reachability"
          frame={frame}
          width={1060}
          enterAt={12}
          zoomAt={84}
          zoom={1.14}
          focus="55% 25%"
          shiftX={-24}
          playbackRate={playbackRate}
        />
      </div>
    </AbsoluteFill>
  );
};
