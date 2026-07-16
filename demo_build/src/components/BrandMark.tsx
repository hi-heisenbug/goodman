import { COLORS, FONTS } from "../theme";

type BrandMarkProps = {
  readonly compact?: boolean;
  readonly glow?: boolean;
};

export const BrandMark: React.FC<BrandMarkProps> = ({
  compact = false,
  glow = false,
}) => {
  const foreground = COLORS.white;

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
      <svg
        width={compact ? 42 : 60}
        height={compact ? 42 : 60}
        viewBox="0 0 54 54"
        style={
          glow
            ? { filter: `drop-shadow(0 0 18px ${COLORS.lime}66)` }
            : undefined
        }
      >
        <rect
          x="1"
          y="1"
          width="52"
          height="52"
          rx="10"
          fill="none"
          stroke={glow ? COLORS.lime : foreground}
          strokeWidth="2"
        />
        <path
          d="M27 13 L16 38 H38 Z"
          fill="none"
          stroke={foreground}
          strokeWidth="3"
          strokeLinejoin="round"
        />
      </svg>
      <div>
        <div
          style={{
            color: foreground,
            fontFamily: FONTS.heading,
            fontWeight: 700,
            fontSize: compact ? 26 : 38,
            letterSpacing: -0.8,
            lineHeight: 1,
          }}
        >
          GOODMAN
        </div>
        {!compact ? (
          <div
            style={{
              color: COLORS.muted,
              fontSize: 17,
              letterSpacing: 3,
              marginTop: 8,
            }}
          >
            by Heisenbug
          </div>
        ) : null}
      </div>
    </div>
  );
};
