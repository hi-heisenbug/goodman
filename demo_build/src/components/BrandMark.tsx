import { COLORS, FONTS } from "../theme";

type BrandMarkProps = {
  readonly light?: boolean;
  readonly compact?: boolean;
};

export const BrandMark: React.FC<BrandMarkProps> = ({
  light = false,
  compact = false,
}) => {
  const foreground = light ? COLORS.white : COLORS.ink;

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
      <svg width={compact ? 42 : 54} height={compact ? 42 : 54} viewBox="0 0 54 54">
        <rect
          x="1"
          y="1"
          width="52"
          height="52"
          rx="10"
          fill="none"
          stroke={foreground}
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
            fontSize: compact ? 26 : 34,
            letterSpacing: -0.8,
            lineHeight: 1,
          }}
        >
          GOODMAN
        </div>
        {!compact ? (
          <div
            style={{
              color: light ? "rgba(255,255,255,0.55)" : COLORS.muted,
              fontSize: 16,
              fontStyle: "italic",
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
