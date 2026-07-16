import { AbsoluteFill, interpolate, useCurrentFrame } from "remotion";
import { COLORS } from "../theme";

type SceneBackgroundProps = {
  readonly tone?: "dark" | "light";
  readonly accent?: string;
};

export const SceneBackground: React.FC<SceneBackgroundProps> = ({
  tone = "dark",
  accent = COLORS.green,
}) => {
  const frame = useCurrentFrame();
  const dark = tone === "dark";

  return (
    <AbsoluteFill
      style={{
        overflow: "hidden",
        backgroundColor: dark ? COLORS.night : COLORS.paper,
        backgroundImage: dark
          ? `linear-gradient(${COLORS.line} 1px, transparent 1px), linear-gradient(90deg, ${COLORS.line} 1px, transparent 1px)`
          : "linear-gradient(rgba(28,151,112,0.06) 1px, transparent 1px), linear-gradient(90deg, rgba(28,151,112,0.06) 1px, transparent 1px)",
        backgroundSize: "84px 84px",
      }}
    >
      <div
        style={{
          position: "absolute",
          width: 740,
          height: 740,
          borderRadius: "50%",
          left: -260,
          top: -280,
          opacity: dark ? 0.22 : 0.12,
          filter: "blur(90px)",
          backgroundColor: accent,
          translate: `${interpolate(frame, [0, 220], [0, 70], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
          })}px 0px`,
        }}
      />
      <div
        style={{
          position: "absolute",
          width: 620,
          height: 620,
          borderRadius: "50%",
          right: -240,
          bottom: -300,
          opacity: dark ? 0.15 : 0.08,
          filter: "blur(110px)",
          backgroundColor: COLORS.lime,
          translate: `${interpolate(frame, [0, 220], [0, -60], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
          })}px 0px`,
        }}
      />
    </AbsoluteFill>
  );
};
