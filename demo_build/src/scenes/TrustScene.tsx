import { AbsoluteFill, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { BrandMark } from "../components/BrandMark";
import { CounterStat } from "../components/CounterStat";
import { KineticHeadline } from "../components/KineticHeadline";
import { SceneLabel } from "../components/SceneLabel";
import { WalkthroughFrame } from "../components/WalkthroughFrame";
import { springIn } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

type BentoCellProps = {
  readonly delay: number;
  readonly children: React.ReactNode;
};

const BentoCell: React.FC<BentoCellProps> = ({ delay, children }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const reveal = springIn(frame, fps, delay);
  return (
    <div
      style={{
        padding: "30px 34px",
        borderRadius: 16,
        border: `1px solid ${COLORS.line}`,
        backgroundColor: "rgba(255,255,255,0.03)",
        opacity: Math.min(1, reveal * 1.3),
        translate: `0px ${(1 - reveal) * 26}px`,
      }}
    >
      {children}
    </div>
  );
};

// The single bento-grid recap of the film: live coverage evidence plus the
// trust numbers, all counted up in place.
export const TrustScene: React.FC = () => {
  const frame = useCurrentFrame();

  return (
    <AbsoluteFill>
      <Backdrop accent={COLORS.green} glowX="72%" glowY="60%" />
      <div style={{ position: "absolute", left: SAFE_X, top: 62 }}>
        <BrandMark compact />
      </div>
      <div style={{ position: "absolute", left: SAFE_X, top: 128 }}>
        <SceneLabel>Coverage and trust</SceneLabel>
        <div style={{ marginTop: 14 }}>
          <KineticHeadline
            text="Signal you can defend to your board."
            frame={frame}
            startAt={6}
            fontSize={62}
            maxWidth={1500}
          />
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          left: SAFE_X,
          right: SAFE_X,
          top: 300,
          display: "grid",
          gridTemplateColumns: "860px 1fr 1fr",
          gap: 22,
        }}
      >
        <div style={{ gridRow: "span 2" }}>
          <WalkthroughFrame
            segment="coverage"
            frame={frame}
            width={860}
            enterAt={16}
            zoomAt={60}
            zoom={1.12}
            focus="55% 24%"
            playbackRate={0.54}
          />
        </div>
        <BentoCell delay={26}>
          <CounterStat
            frame={frame}
            startAt={34}
            target={251}
            label="packages fingerprinted"
            fontSize={72}
          />
        </BentoCell>
        <BentoCell delay={32}>
          <CounterStat
            frame={frame}
            startAt={40}
            target={98}
            suffix="%"
            label="baseline coverage"
            color={COLORS.lime}
            fontSize={72}
          />
        </BentoCell>
        <BentoCell delay={38}>
          <CounterStat
            frame={frame}
            startAt={46}
            target={100}
            suffix="%"
            label="attribution success"
            color={COLORS.lime}
            fontSize={72}
          />
        </BentoCell>
        <BentoCell delay={44}>
          <div
            style={{
              color: COLORS.white,
              fontFamily: FONTS.heading,
              fontWeight: 700,
              fontSize: 40,
              lineHeight: 1.1,
            }}
          >
            Zero code changes
          </div>
          <div
            style={{
              marginTop: 14,
              color: COLORS.muted,
              fontFamily: FONTS.mono,
              fontSize: 19,
              letterSpacing: 1,
            }}
          >
            eBPF · kernel-level · fail-open
          </div>
        </BentoCell>
      </div>

      <div
        style={{
          position: "absolute",
          right: SAFE_X,
          bottom: 44,
          color: COLORS.faint,
          fontFamily: FONTS.mono,
          fontSize: 16,
          letterSpacing: 1,
        }}
      >
        live coverage view · real sensor data
      </div>
    </AbsoluteFill>
  );
};
