import { useVideoConfig } from "remotion";
import { springIn } from "../motion";
import { COLORS, FONTS } from "../theme";

type KineticHeadlineProps = {
  readonly text: string;
  readonly frame: number;
  readonly startAt?: number;
  readonly fontSize?: number;
  readonly color?: string;
  readonly accentWords?: readonly string[];
  readonly accentColor?: string;
  readonly align?: "left" | "center";
  readonly maxWidth?: number;
};

// Word-by-word mask reveal: each word wipes up from a clipped box with a
// critically damped spring, staggered ~2 frames apart.
export const KineticHeadline: React.FC<KineticHeadlineProps> = ({
  text,
  frame,
  startAt = 0,
  fontSize = 96,
  color = COLORS.white,
  accentWords = [],
  accentColor = COLORS.lime,
  align = "left",
  maxWidth,
}) => {
  const { fps } = useVideoConfig();
  const words = text.split(" ");

  return (
    <div
      style={{
        display: "flex",
        flexWrap: "wrap",
        justifyContent: align === "center" ? "center" : "flex-start",
        columnGap: fontSize * 0.26,
        rowGap: fontSize * 0.08,
        maxWidth,
        fontFamily: FONTS.heading,
        fontWeight: 700,
        fontSize,
        letterSpacing: fontSize * -0.032,
        lineHeight: 1.04,
      }}
    >
      {words.map((word, index) => {
        const reveal = springIn(frame, fps, startAt + index * 2);
        const accented = accentWords.includes(word.replace(/[.,]/g, ""));
        return (
          <span
            key={`${word}-${index}`}
            style={{
              display: "inline-block",
              overflow: "hidden",
              verticalAlign: "bottom",
            }}
          >
            <span
              style={{
                display: "inline-block",
                color: accented ? accentColor : color,
                opacity: Math.min(1, reveal * 1.4),
                transform: `translateY(${(1 - reveal) * fontSize * 0.55}px)`,
              }}
            >
              {word}
            </span>
          </span>
        );
      })}
    </div>
  );
};
