import { AbsoluteFill, useCurrentFrame } from "remotion";
import { COLORS } from "../theme";

type BackdropProps = {
  readonly accent?: string;
  readonly glowX?: string;
  readonly glowY?: string;
  readonly glowOpacity?: number;
};

// Dark canvas with a faint grid, one radial glow behind the focal point,
// a vignette, and animated film grain. Grain reseeds every other frame so
// it reads as texture, not a static overlay.
export const Backdrop: React.FC<BackdropProps> = ({
  accent = COLORS.green,
  glowX = "50%",
  glowY = "38%",
  glowOpacity = 0.14,
}) => {
  const frame = useCurrentFrame();
  const grainSeed = Math.floor(frame / 2) % 9;

  return (
    <AbsoluteFill style={{ overflow: "hidden", backgroundColor: COLORS.ink }}>
      <AbsoluteFill
        style={{
          backgroundImage: `linear-gradient(rgba(255,255,255,0.028) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.028) 1px, transparent 1px)`,
          backgroundSize: "96px 96px",
        }}
      />
      <AbsoluteFill
        style={{
          background: `radial-gradient(900px 620px at ${glowX} ${glowY}, ${accent}, transparent 70%)`,
          opacity: glowOpacity,
        }}
      />
      <AbsoluteFill
        style={{
          background:
            "radial-gradient(ellipse 130% 100% at 50% 45%, transparent 55%, rgba(0,0,0,0.55) 100%)",
        }}
      />
      <svg
        width="100%"
        height="100%"
        style={{ position: "absolute", inset: 0, opacity: 0.05 }}
      >
        <filter id={`grain-${grainSeed}`}>
          <feTurbulence
            type="fractalNoise"
            baseFrequency="0.9"
            numOctaves="2"
            seed={grainSeed}
            stitchTiles="stitch"
          />
          <feColorMatrix type="saturate" values="0" />
        </filter>
        <rect
          width="100%"
          height="100%"
          filter={`url(#grain-${grainSeed})`}
        />
      </svg>
    </AbsoluteFill>
  );
};
