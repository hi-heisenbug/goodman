import { AbsoluteFill, Sequence, useCurrentFrame } from "remotion";
import { BrandMark } from "../components/BrandMark";
import { SceneBackground } from "../components/SceneBackground";
import { SceneLabel } from "../components/SceneLabel";
import { WalkthroughFrame } from "../components/WalkthroughFrame";
import { fadeWindow, progress } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

type MetricProps = {
  readonly value: string;
  readonly label: string;
  readonly color: string;
};

const Metric: React.FC<MetricProps> = ({ value, label, color }) => (
  <div
    style={{
      minWidth: 180,
      padding: "18px 22px",
      borderRadius: 12,
      border: "1px solid rgba(255,255,255,0.14)",
      backgroundColor: "rgba(6,11,9,0.9)",
    }}
  >
    <div style={{ color, fontFamily: FONTS.heading, fontSize: 43 }}>{value}</div>
    <div style={{ color: "rgba(255,255,255,0.55)", fontSize: 17 }}>{label}</div>
  </div>
);

export const TrustScene: React.FC = () => {
  const frame = useCurrentFrame();
  const fingerprints = fadeWindow(frame, 0, 18, 78, 92);
  const coverage = progress(frame, 94, 22);

  return (
    <AbsoluteFill>
      <SceneBackground accent={COLORS.green} />
      <div style={{ position: "absolute", left: SAFE_X, top: 58 }}>
        <BrandMark light compact />
      </div>

      <div style={{ position: "absolute", inset: 0, opacity: fingerprints }}>
        <div style={{ position: "absolute", left: SAFE_X, top: 145 }}>
          <SceneLabel>Behavior fingerprint library</SceneLabel>
          <div
            style={{
              marginTop: 12,
              color: COLORS.white,
              fontFamily: FONTS.heading,
              fontSize: 66,
              letterSpacing: -2.8,
            }}
          >
            Know exactly what normal looks like.
          </div>
        </div>
        <div style={{ position: "absolute", left: 190, top: 255 }}>
          <WalkthroughFrame
            segment="fingerprints"
            frame={frame}
            width={1350}
            zoomAt={44}
            zoom={1.12}
            focus="55% 30%"
            playbackRate={0.92}
          />
        </div>
        <div
          style={{
            position: "absolute",
            right: 110,
            top: 380,
            display: "grid",
            gap: 14,
          }}
        >
          <Metric value="251" label="packages learned" color={COLORS.white} />
          <Metric value="246" label="baselines promoted" color={COLORS.lime} />
          <Metric value="98%" label="baseline coverage" color={COLORS.mint} />
        </div>
      </div>

      <Sequence from={88}>
        <div style={{ position: "absolute", inset: 0, opacity: coverage }}>
          <div style={{ position: "absolute", left: SAFE_X, top: 145 }}>
            <SceneLabel>Coverage and trust</SceneLabel>
            <div
              style={{
                marginTop: 12,
                color: COLORS.white,
                fontFamily: FONTS.heading,
                fontSize: 66,
                letterSpacing: -2.8,
              }}
            >
              Know when the signal is trustworthy.
            </div>
          </div>
          <div style={{ position: "absolute", left: 190, top: 255 }}>
            <WalkthroughFrame
              segment="coverage"
              frame={Math.max(0, frame - 88)}
              width={1350}
              zoomAt={46}
              zoom={1.12}
              focus="55% 24%"
              playbackRate={1.28}
            />
          </div>
          <div
            style={{
              position: "absolute",
              right: 110,
              top: 380,
              display: "grid",
              gap: 14,
            }}
          >
            <Metric value="100%" label="attribution success" color={COLORS.lime} />
            <Metric value="1" label="sensor reporting" color={COLORS.mint} />
            <Metric value="0" label="unknown packages" color={COLORS.white} />
          </div>
        </div>
      </Sequence>
    </AbsoluteFill>
  );
};
