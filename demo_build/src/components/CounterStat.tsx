import { countUp } from "../motion";
import { COLORS, FONTS, TNUM } from "../theme";

type CounterStatProps = {
  readonly frame: number;
  readonly startAt: number;
  readonly target: number;
  readonly label: string;
  readonly color?: string;
  readonly suffix?: string;
  readonly fontSize?: number;
  readonly durationInFrames?: number;
};

// Animated counter with tabular numerals so digits never jitter, and a label
// that fades in just after the number settles.
export const CounterStat: React.FC<CounterStatProps> = ({
  frame,
  startAt,
  target,
  label,
  color = COLORS.white,
  suffix = "",
  fontSize = 96,
  durationInFrames = 24,
}) => {
  const value = countUp(frame, startAt, durationInFrames, target);
  const labelOpacity = countUp(
    frame,
    startAt + durationInFrames + 3,
    10,
    100,
  );

  return (
    <div>
      <div
        style={{
          color,
          fontFamily: FONTS.heading,
          fontWeight: 700,
          fontSize,
          letterSpacing: fontSize * -0.035,
          lineHeight: 1,
          ...TNUM,
        }}
      >
        {value.toLocaleString("en-US")}
        {suffix}
      </div>
      <div
        style={{
          marginTop: 12,
          color: COLORS.muted,
          fontFamily: FONTS.body,
          fontWeight: 600,
          fontSize: Math.max(20, fontSize * 0.23),
          opacity: labelOpacity / 100,
        }}
      >
        {label}
      </div>
    </div>
  );
};
