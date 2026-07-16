import { AbsoluteFill, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { BrandMark } from "../components/BrandMark";
import { KineticHeadline } from "../components/KineticHeadline";
import { TerminalCard, type TerminalLine } from "../components/TerminalCard";
import { progress, springIn } from "../motion";
import { COLORS, FONTS } from "../theme";

const CTA_LINES: readonly TerminalLine[] = [
  { text: "goodmanctl demo", at: 74, kind: "command" },
  { text: "Goodman demo is ready → http://127.0.0.1:8844", at: 104, kind: "output", typed: false },
];

export const ClosingScene: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const logo = springIn(frame, fps, 4);
  const terminal = springIn(frame, fps, 66);
  const pill = springIn(frame, fps, 128);
  const tagline = progress(frame, 148, 16);

  return (
    <AbsoluteFill>
      <Backdrop accent={COLORS.green} glowY="42%" glowOpacity={0.16} />
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 44,
        }}
      >
        <div style={{ opacity: Math.min(1, logo * 1.3), scale: 0.94 + logo * 0.06 }}>
          <BrandMark glow />
        </div>
        <KineticHeadline
          text="See which dependency actually did it."
          frame={frame}
          startAt={14}
          fontSize={92}
          align="center"
          maxWidth={1380}
          accentWords={["actually"]}
          accentColor={COLORS.lime}
        />
        <div
          style={{
            opacity: Math.min(1, terminal * 1.3),
            translate: `0px ${(1 - terminal) * 30}px`,
          }}
        >
          <TerminalCard
            lines={CTA_LINES}
            frame={frame}
            width={880}
            title="one afternoon to a running pilot"
            fontSize={25}
            minHeight={130}
          />
        </div>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: 20,
          }}
        >
          <div
            style={{
              padding: "16px 28px",
              borderRadius: 10,
              backgroundColor: COLORS.mint,
              color: "#0a0a0c",
              fontFamily: FONTS.mono,
              fontSize: 24,
              fontWeight: 700,
              opacity: Math.min(1, pill * 1.3),
              scale: 0.95 + pill * 0.05,
              boxShadow: `0 0 70px -16px ${COLORS.mint}aa`,
            }}
          >
            github.com/hi-heisenbug/goodman
          </div>
          <div
            style={{
              color: COLORS.muted,
              fontFamily: FONTS.mono,
              fontSize: 19,
              letterSpacing: 2.5,
              opacity: tagline,
            }}
          >
            RUNTIME DEPENDENCY SECURITY AT PACKAGE PRECISION
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};
