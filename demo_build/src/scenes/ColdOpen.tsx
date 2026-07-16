import { AbsoluteFill, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { KineticHeadline } from "../components/KineticHeadline";
import { TerminalCard, type TerminalLine } from "../components/TerminalCard";
import { fadeWindow, progress, springIn } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

const TERMINAL_LINES: readonly TerminalLine[] = [
  { text: "npm install", at: 88, kind: "command" },
  { text: "added 1 package, audited 1401 packages in 2.1s", at: 116, kind: "output", typed: false },
  { text: "found 0 vulnerabilities", at: 122, kind: "output", typed: false },
];

const KERNEL_EVENTS = [
  ["READ", "/home/app/.npmrc"],
  ["CONNECT", "169.254.169.254:80"],
  ["EXEC", "/bin/sh"],
] as const;

export const ColdOpen: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const hook = fadeWindow(frame, 2, 12, 66, 80);
  const middle = fadeWindow(frame, 80, 92, 184, 196);
  const closer = progress(frame, 198, 18);

  return (
    <AbsoluteFill>
      <Backdrop accent={COLORS.red} glowOpacity={0.1} />
      <div
        style={{
          position: "absolute",
          right: SAFE_X,
          top: 82,
          color: COLORS.faint,
          fontFamily: FONTS.mono,
          fontSize: 18,
          letterSpacing: 1.5,
        }}
      >
        PROD / RUNTIME / 02:17:41
      </div>

      {/* Beat 1: the real-world hook, no logo. */}
      <div
        style={{
          position: "absolute",
          inset: "0 140px",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          opacity: hook,
        }}
      >
        <div style={{ textAlign: "center" }}>
          <div
            style={{
              color: COLORS.red,
              fontFamily: FONTS.mono,
              fontSize: 22,
              fontWeight: 700,
              letterSpacing: 5,
              marginBottom: 34,
            }}
          >
            SEPTEMBER 2025 · THE SHAI-HULUD WORM
          </div>
          <KineticHeadline
            text="One npm package compromised 500+ more."
            frame={frame}
            startAt={4}
            fontSize={104}
            align="center"
            maxWidth={1450}
            accentWords={["500+"]}
            accentColor={COLORS.red}
          />
        </div>
      </div>

      {/* Beat 2: the trojaned install, and what the kernel saw. */}
      <div
        style={{
          position: "absolute",
          left: SAFE_X,
          right: SAFE_X,
          top: 0,
          bottom: 0,
          display: "grid",
          gridTemplateColumns: "1fr 1fr",
          gap: 60,
          alignItems: "center",
          opacity: middle,
        }}
      >
        <div style={{ translate: `0px ${(1 - springIn(frame, fps, 84)) * 40}px` }}>
          <div
            style={{
              color: COLORS.muted,
              fontFamily: FONTS.mono,
              fontSize: 20,
              letterSpacing: 3,
              marginBottom: 24,
            }}
          >
            A ROUTINE DEPENDENCY UPDATE
          </div>
          <TerminalCard
            lines={TERMINAL_LINES}
            frame={frame}
            width={800}
            minHeight={230}
          />
        </div>
        <div style={{ translate: `0px ${(1 - springIn(frame, fps, 96)) * 40}px` }}>
          <div
            style={{
              color: COLORS.red,
              fontFamily: FONTS.mono,
              fontSize: 20,
              letterSpacing: 3,
              marginBottom: 24,
            }}
          >
            SECONDS LATER, IN THE KERNEL
          </div>
          <div
            style={{
              borderRadius: 14,
              border: `1px solid ${COLORS.line}`,
              backgroundColor: COLORS.surface,
              boxShadow: "0 40px 110px rgba(0,0,0,0.6)",
              padding: "10px 30px",
              minHeight: 300,
            }}
          >
            {KERNEL_EVENTS.map(([type, value], index) => {
              const row = springIn(frame, fps, 138 + index * 7);
              return (
                <div
                  key={type}
                  style={{
                    display: "grid",
                    gridTemplateColumns: "170px 1fr",
                    gap: 22,
                    padding: "22px 0 22px 18px",
                    borderTop: index === 0 ? "none" : `1px solid ${COLORS.line}`,
                    borderLeft: `3px solid ${COLORS.red}`,
                    marginLeft: -18,
                    paddingLeft: 32,
                    opacity: Math.min(1, row * 1.4),
                    translate: `0px ${(1 - row) * 16}px`,
                    fontFamily: FONTS.mono,
                    fontSize: 25,
                  }}
                >
                  <span style={{ color: COLORS.red, fontWeight: 700 }}>
                    + {type}
                  </span>
                  <span style={{ color: COLORS.white }}>{value}</span>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      {/* Beat 3: the wedge. */}
      <div
        style={{
          position: "absolute",
          inset: "0 140px",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          opacity: closer,
        }}
      >
        <KineticHeadline
          text="Your scanner said it was clean. Then it ran."
          frame={frame}
          startAt={200}
          fontSize={98}
          align="center"
          maxWidth={1420}
          accentWords={["ran"]}
          accentColor={COLORS.red}
        />
      </div>
    </AbsoluteFill>
  );
};
