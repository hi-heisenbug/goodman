import { useCurrentFrame } from "remotion";

type BorderBeamProps = {
  readonly color: string;
  readonly borderRadius?: number;
  readonly secondsPerRotation?: number;
};

// A light sweep travelling along the parent card's border. Use on the single
// most important element in a scene, never more than one at a time.
export const BorderBeam: React.FC<BorderBeamProps> = ({
  color,
  borderRadius = 16,
  secondsPerRotation = 3.5,
}) => {
  const frame = useCurrentFrame();
  const angle = ((frame / (30 * secondsPerRotation)) * 360) % 360;

  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        borderRadius,
        padding: 2,
        background: `conic-gradient(from ${angle}deg, transparent 0deg, transparent 285deg, ${color} 330deg, transparent 360deg)`,
        WebkitMask:
          "linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0)",
        WebkitMaskComposite: "xor",
        maskComposite: "exclude",
        pointerEvents: "none",
      }}
    />
  );
};
