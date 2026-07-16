import { AbsoluteFill, interpolate, useCurrentFrame } from "remotion";
import { BrandMark } from "../components/BrandMark";
import { SceneBackground } from "../components/SceneBackground";
import { SceneLabel } from "../components/SceneLabel";
import { WalkthroughFrame } from "../components/WalkthroughFrame";
import { progress } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

export const ReachabilityScene: React.FC = () => {
  const frame = useCurrentFrame();
  const enter = progress(frame, 5, 26);
  const declared = Math.round(
    interpolate(frame, [18, 74], [0, 1400], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    }),
  );
  const executed = Math.round(
    interpolate(frame, [58, 116], [0, 240], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    }),
  );
  const reduction = progress(frame, 112, 24);

  return (
    <AbsoluteFill>
      <SceneBackground tone="light" accent={COLORS.green} />
      <div style={{ position: "absolute", left: SAFE_X, top: 58 }}>
        <BrandMark compact />
      </div>
      <div
        style={{
          position: "absolute",
          left: SAFE_X,
          top: 190,
          width: 590,
          opacity: enter,
        }}
      >
        <SceneLabel light={false}>Runtime reachability</SceneLabel>
        <div
          style={{
            marginTop: 18,
            color: COLORS.ink,
            fontFamily: FONTS.heading,
            fontSize: 74,
            lineHeight: 1.02,
            letterSpacing: -3,
          }}
        >
          Stop chasing what never executed.
        </div>
        <div
          style={{
            marginTop: 30,
            color: COLORS.charcoal,
            fontSize: 25,
            lineHeight: 1.5,
          }}
        >
          Goodman combines the lockfile with observed runtime behavior so the
          vulnerable packages that actually ran rise to the top.
        </div>

        <div
          style={{
            marginTop: 48,
            display: "grid",
            gridTemplateColumns: "1fr 70px 1fr",
            alignItems: "center",
            gap: 20,
          }}
        >
          <div>
            <div
              style={{
                color: COLORS.ink,
                fontFamily: FONTS.heading,
                fontSize: 88,
                letterSpacing: -4,
              }}
            >
              {declared.toLocaleString("en-US")}
            </div>
            <div style={{ color: COLORS.muted, fontSize: 22, fontWeight: 700 }}>
              declared
            </div>
          </div>
          <div
            style={{ color: COLORS.green, fontSize: 46, textAlign: "center" }}
          >
            →
          </div>
          <div>
            <div
              style={{
                color: COLORS.green,
                fontFamily: FONTS.heading,
                fontSize: 88,
                letterSpacing: -4,
              }}
            >
              {executed.toLocaleString("en-US")}
            </div>
            <div style={{ color: COLORS.muted, fontSize: 22, fontWeight: 700 }}>
              executed
            </div>
          </div>
        </div>

        <div
          style={{
            marginTop: 34,
            display: "inline-flex",
            alignItems: "center",
            gap: 14,
            padding: "15px 20px",
            borderRadius: 10,
            backgroundColor: COLORS.mint,
            color: COLORS.ink,
            fontSize: 22,
            fontWeight: 700,
            opacity: reduction,
            translate: `${(1 - reduction) * -30}px 0px`,
          }}
        >
          <span style={{ color: COLORS.green, fontSize: 30 }}>83%</span>
          less dependency noise
        </div>
      </div>

      <div style={{ position: "absolute", left: 720, top: 210 }}>
        <WalkthroughFrame
          segment="reachability"
          frame={frame}
          width={1120}
          zoomAt={82}
          zoom={1.14}
          focus="55% 25%"
          shiftX={-28}
          playbackRate={0.5}
        />
      </div>
    </AbsoluteFill>
  );
};
