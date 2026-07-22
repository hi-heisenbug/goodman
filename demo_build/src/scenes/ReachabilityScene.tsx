import { AbsoluteFill, Sequence, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { BrandMark } from "../components/BrandMark";
import { CounterStat } from "../components/CounterStat";
import { KineticHeadline } from "../components/KineticHeadline";
import { SceneLabel } from "../components/SceneLabel";
import { WalkthroughFrame } from "../components/WalkthroughFrame";
import { fadeWindow, progress, springIn } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

type ReachabilitySceneProps = {
  readonly reachPlaybackRate?: number;
  readonly coveragePlaybackRate?: number;
};

const COVERAGE_START = 105;

export const ReachabilityScene: React.FC<ReachabilitySceneProps> = ({
  reachPlaybackRate = 0.84,
  coveragePlaybackRate = 0.73,
}) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const reachVisible = fadeWindow(frame, 8, 20, 98, 116);
  const coverageVisible = progress(frame, COVERAGE_START, 18);
  const coverageFrame = Math.max(0, frame - COVERAGE_START);
  const reduction = springIn(frame, fps, 70);
  const trust = springIn(frame, fps, 142);

  return (
    <AbsoluteFill>
      <Backdrop accent={COLORS.green} glowX="28%" glowY="50%" />
      <div style={{ position: "absolute", left: SAFE_X, top: 62 }}>
        <BrandMark compact />
      </div>

      <div style={{ position: "absolute", left: SAFE_X, top: 170, width: 620 }}>
        <SceneLabel>Prioritization + coverage</SceneLabel>
        <div style={{ marginTop: 18 }}>
          <KineticHeadline
            text="Know what ran—and what you missed."
            frame={frame}
            startAt={4}
            fontSize={72}
            maxWidth={640}
            accentWords={["ran", "missed"]}
            accentColor={COLORS.lime}
          />
        </div>

        <div
          style={{
            marginTop: 40,
            display: "flex",
            alignItems: "flex-start",
            gap: 28,
            opacity: reachVisible,
          }}
        >
          <CounterStat
            frame={frame}
            startAt={30}
            durationInFrames={34}
            target={1400}
            label="declared"
            fontSize={82}
          />
          <div style={{ color: COLORS.green, fontSize: 48, marginTop: 15 }}>→</div>
          <CounterStat
            frame={frame}
            startAt={62}
            durationInFrames={34}
            target={240}
            label="actually executed"
            color={COLORS.lime}
            fontSize={82}
          />
        </div>

        <div
          style={{
            marginTop: 28,
            display: "inline-flex",
            alignItems: "center",
            gap: 12,
            padding: "14px 20px",
            borderRadius: 9,
            backgroundColor: "rgba(147,203,82,0.12)",
            border: "1px solid rgba(147,203,82,0.34)",
            color: COLORS.white,
            fontFamily: FONTS.body,
            fontWeight: 700,
            fontSize: 21,
            opacity: Math.min(reachVisible, reduction * 1.3),
          }}
        >
          <span style={{ color: COLORS.lime, fontSize: 29 }}>83%</span>
          less dependency noise
        </div>

        <div
          style={{
            position: "absolute",
            left: 0,
            top: 360,
            display: "grid",
            gridTemplateColumns: "1fr 1fr",
            gap: 14,
            width: 610,
            opacity: Math.min(1, trust * 1.25),
            translate: `0px ${(1 - trust) * 22}px`,
          }}
        >
          {[
            ["100%", "attribution success", COLORS.lime],
            ["1", "injection gap found", COLORS.amber],
          ].map(([value, label, color]) => (
            <div
              key={label}
              style={{
                padding: "22px 24px",
                borderRadius: 12,
                border: `1px solid ${COLORS.line}`,
                backgroundColor: "rgba(255,255,255,0.035)",
              }}
            >
              <div
                style={{
                  color,
                  fontFamily: FONTS.heading,
                  fontSize: 48,
                  fontWeight: 700,
                }}
              >
                {value}
              </div>
              <div
                style={{
                  marginTop: 8,
                  color: COLORS.muted,
                  fontFamily: FONTS.mono,
                  fontSize: 16,
                }}
              >
                {label}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          left: 760,
          top: 220,
          opacity: reachVisible,
        }}
      >
        <WalkthroughFrame
          segment="reachability"
          frame={frame}
          width={1080}
          enterAt={8}
          zoomAt={58}
          zoom={1.11}
          focus="55% 24%"
          shiftX={-20}
          playbackRate={reachPlaybackRate}
        />
      </div>

      <Sequence from={COVERAGE_START} layout="none" name="Coverage walkthrough">
        <div
          style={{
            position: "absolute",
            left: 760,
            top: 220,
            opacity: coverageVisible,
          }}
        >
          <WalkthroughFrame
            segment="coverage"
            frame={coverageFrame}
            width={1080}
            enterAt={0}
            zoomAt={36}
            zoom={1.12}
            focus="55% 24%"
            shiftX={-20}
            playbackRate={coveragePlaybackRate}
          />
        </div>
      </Sequence>

      <div
        style={{
          position: "absolute",
          right: SAFE_X,
          bottom: 42,
          color: COLORS.faint,
          fontFamily: FONTS.mono,
          fontSize: 15,
          letterSpacing: 1,
        }}
      >
        live reachability + coverage views · API-backed state
      </div>
    </AbsoluteFill>
  );
};
