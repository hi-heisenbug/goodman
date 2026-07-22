import { AbsoluteFill, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { BrandMark } from "../components/BrandMark";
import { KineticHeadline } from "../components/KineticHeadline";
import { TerminalCard, type TerminalLine } from "../components/TerminalCard";
import { progress, springIn } from "../motion";
import { COLORS, FONTS } from "../theme";

const CTA_LINES: readonly TerminalLine[] = [
  { text: "bash scripts/setup-everything.sh demo", at: 52, kind: "command" },
  { text: "dashboard + live replay ready → http://127.0.0.1:8844", at: 78, kind: "output", typed: false },
  { text: "bash scripts/setup-everything.sh observe --pid <PID>", at: 88, kind: "command" },
  { text: "PASS · exact package@version on your workload", at: 118, kind: "success", typed: false },
];

export const ClosingScene: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const logo = springIn(frame, fps, 2);
  const terminal = springIn(frame, fps, 46);
  const pill = springIn(frame, fps, 112);
  const tagline = progress(frame, 124, 14);

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
          gap: 36,
        }}
      >
        <div style={{ opacity: Math.min(1, logo * 1.3), scale: 0.94 + logo * 0.06 }}>
          <BrandMark glow />
        </div>
        <KineticHeadline
          text="Demo anywhere. Prove it on yours."
          frame={frame}
          startAt={8}
          fontSize={96}
          align="center"
          maxWidth={1380}
          accentWords={["Prove", "yours"]}
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
            width={1120}
            title="two commands · zero fake UI"
            fontSize={22}
            minHeight={180}
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
              padding: "14px 26px",
              borderRadius: 10,
              backgroundColor: COLORS.mint,
              color: "#0a0a0c",
              fontFamily: FONTS.mono,
              fontSize: 22,
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
              fontSize: 17,
              letterSpacing: 2.5,
              opacity: tagline,
            }}
          >
            GOODMAN BY HEISENBUG · PACKAGE-PRECISE RUNTIME SECURITY
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};
