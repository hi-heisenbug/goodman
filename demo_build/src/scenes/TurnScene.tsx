import { AbsoluteFill, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { BrandMark } from "../components/BrandMark";
import { KineticHeadline } from "../components/KineticHeadline";
import { progress, springIn } from "../motion";
import { COLORS, FONTS } from "../theme";

// 400ms of pure black and silence, then the first green frame of the film.
export const TurnScene: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const reveal = progress(frame, 12, 14);
  const logo = springIn(frame, fps, 14);
  const tagline = progress(frame, 62, 16);

  return (
    <AbsoluteFill style={{ backgroundColor: "#000000" }}>
      <div style={{ position: "absolute", inset: 0, opacity: reveal }}>
        <Backdrop accent={COLORS.green} glowY="50%" glowOpacity={0.18} />
      </div>
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 54,
        }}
      >
        <div
          style={{
            opacity: Math.min(1, logo * 1.3),
            scale: 0.92 + logo * 0.08,
          }}
        >
          <BrandMark glow />
        </div>
        <KineticHeadline
          text="Goodman watches what your code actually does."
          frame={frame}
          startAt={26}
          fontSize={84}
          align="center"
          maxWidth={1360}
          accentWords={["actually", "does"]}
          accentColor={COLORS.lime}
        />
        <div
          style={{
            color: COLORS.muted,
            fontFamily: FONTS.mono,
            fontSize: 21,
            letterSpacing: 3,
            opacity: tagline,
          }}
        >
          eBPF RUNTIME SENSOR · PACKAGE-LEVEL ATTRIBUTION · ZERO CODE CHANGES
        </div>
      </div>
    </AbsoluteFill>
  );
};
