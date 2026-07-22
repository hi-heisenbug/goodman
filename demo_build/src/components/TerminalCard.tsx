import { COLORS, FONTS } from "../theme";

export type TerminalLine = {
  readonly text: string;
  readonly at: number;
  readonly kind: "command" | "output" | "alert" | "success";
  readonly typed?: boolean;
};

type TerminalCardProps = {
  readonly lines: readonly TerminalLine[];
  readonly frame: number;
  readonly width?: number;
  readonly title?: string;
  readonly fontSize?: number;
  readonly minHeight?: number;
};

// Typed commands appear character by character (~2.5 chars/frame); output
// lands as a block, the way real terminals behave.
export const TerminalCard: React.FC<TerminalCardProps> = ({
  lines,
  frame,
  width = 980,
  title = "app-server — bash",
  fontSize = 26,
  minHeight = 0,
}) => {
  const cursorOn = Math.floor(frame / 16) % 2 === 0;
  const lastVisible = [...lines]
    .reverse()
    .find((line) => frame >= line.at && line.kind === "command");

  return (
    <div
      style={{
        width,
        borderRadius: 14,
        overflow: "hidden",
        border: `1px solid ${COLORS.line}`,
        backgroundColor: COLORS.surface,
        boxShadow: "0 40px 110px rgba(0,0,0,0.6)",
      }}
    >
      <div
        style={{
          height: 46,
          display: "flex",
          alignItems: "center",
          gap: 9,
          padding: "0 20px",
          backgroundColor: COLORS.surfaceRaised,
          borderBottom: `1px solid ${COLORS.line}`,
        }}
      >
        {["#3a3f47", "#3a3f47", "#3a3f47"].map((color, index) => (
          <div
            key={index}
            style={{
              width: 12,
              height: 12,
              borderRadius: "50%",
              backgroundColor: color,
            }}
          />
        ))}
        <div
          style={{
            marginLeft: 14,
            color: COLORS.muted,
            fontFamily: FONTS.mono,
            fontSize: 17,
          }}
        >
          {title}
        </div>
      </div>
      <div
        style={{
          padding: "26px 30px",
          fontFamily: FONTS.mono,
          fontSize,
          lineHeight: 1.75,
          minHeight,
        }}
      >
        {lines.map((line) => {
          if (frame < line.at) {
            return null;
          }
          const typedChars =
            line.typed === false
              ? line.text.length
              : Math.floor((frame - line.at) * 2.5);
          const visible = line.text.slice(0, typedChars);
          const isTyping =
            line.typed !== false && typedChars < line.text.length;
          const color =
            line.kind === "command"
              ? COLORS.white
              : line.kind === "alert"
                ? COLORS.red
                : line.kind === "success"
                  ? COLORS.lime
                  : COLORS.muted;
          return (
            <div key={`${line.at}-${line.text}`} style={{ color }}>
              {line.kind === "command" ? (
                <span style={{ color: COLORS.lime }}>$ </span>
              ) : null}
              {visible}
              {isTyping && line === lastVisible && cursorOn ? (
                <span
                  style={{
                    display: "inline-block",
                    width: fontSize * 0.55,
                    height: fontSize * 1.05,
                    verticalAlign: "text-bottom",
                    backgroundColor: COLORS.white,
                  }}
                />
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
};
