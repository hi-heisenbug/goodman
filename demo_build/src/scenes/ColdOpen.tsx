import { AbsoluteFill, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { KineticHeadline } from "../components/KineticHeadline";
import { TerminalCard, type TerminalLine } from "../components/TerminalCard";
import { fadeWindow, progress, springIn } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

const KERNEL_EVENTS = [
  ["SECRET READ", "READ /home/app/.npmrc"],
  ["CLOUD METADATA", "CONNECT 169.254.169.254:80"],
  ["NEW OUTBOUND", "CONNECT 203.0.113.42:443"],
  ["SHELL", "EXEC /bin/sh"],
] as const;

const TIMINGS = {
  master: {
    hook: [0, 10, 42, 52],
    middle: [46, 58, 132, 142],
    terminalIn: 50,
    kernelIn: 56,
    command: 56,
    output: 80,
    rows: 72,
    closer: 136,
  },
  compact: {
    hook: [0, 8, 34, 42],
    middle: [38, 48, 106, 116],
    terminalIn: 42,
    kernelIn: 48,
    command: 46,
    output: 66,
    rows: 60,
    closer: 112,
  },
} as const;

type ColdOpenProps = {
  readonly compact?: boolean;
};

export const ColdOpen: React.FC<ColdOpenProps> = ({ compact = false }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const timing = TIMINGS[compact ? "compact" : "master"];
  const hook = fadeWindow(
    frame,
    timing.hook[0],
    timing.hook[1],
    timing.hook[2],
    timing.hook[3],
  );
  const middle = fadeWindow(
    frame,
    timing.middle[0],
    timing.middle[1],
    timing.middle[2],
    timing.middle[3],
  );
  const closer = progress(frame, timing.closer, 14);
  const terminalLines: readonly TerminalLine[] = [
    { text: "npm install", at: timing.command, kind: "command" },
    {
      text: "added 1 package, audited 1401 packages",
      at: timing.output,
      kind: "output",
      typed: false,
    },
    {
      text: "found 0 vulnerabilities",
      at: timing.output + 5,
      kind: "success",
      typed: false,
    },
  ];

  return (
    <AbsoluteFill>
      <Backdrop accent={COLORS.red} glowOpacity={0.11} />
      <div
        style={{
          position: "absolute",
          right: SAFE_X,
          top: 76,
          color: COLORS.faint,
          fontFamily: FONTS.mono,
          fontSize: 17,
          letterSpacing: 2,
        }}
      >
        PROD / DEPENDENCY UPDATE / 02:17:41
      </div>

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
              color: COLORS.lime,
              fontFamily: FONTS.mono,
              fontSize: 22,
              fontWeight: 700,
              letterSpacing: 5,
              marginBottom: 30,
            }}
          >
            A CLEAN INSTALL
          </div>
          <KineticHeadline
            text="found 0 vulnerabilities"
            frame={frame}
            startAt={2}
            fontSize={120}
            align="center"
            maxWidth={1500}
            accentWords={["0"]}
            accentColor={COLORS.lime}
          />
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          left: SAFE_X,
          right: SAFE_X,
          top: 0,
          bottom: 0,
          display: "grid",
          gridTemplateColumns: "0.92fr 1.08fr",
          gap: 58,
          alignItems: "center",
          opacity: middle,
        }}
      >
        <div
          style={{
            translate: `0px ${(1 - springIn(frame, fps, timing.terminalIn)) * 42}px`,
          }}
        >
          <div
            style={{
              color: COLORS.muted,
              fontFamily: FONTS.mono,
              fontSize: 19,
              letterSpacing: 3,
              marginBottom: 22,
            }}
          >
            WHAT THE SCANNER SAW
          </div>
          <TerminalCard
            lines={terminalLines}
            frame={frame}
            width={730}
            minHeight={220}
          />
        </div>

        <div
          style={{
            translate: `0px ${(1 - springIn(frame, fps, timing.kernelIn)) * 42}px`,
          }}
        >
          <div
            style={{
              color: COLORS.red,
              fontFamily: FONTS.mono,
              fontSize: 19,
              letterSpacing: 3,
              marginBottom: 22,
            }}
          >
            WHAT RAN SECONDS LATER
          </div>
          <div
            style={{
              borderRadius: 14,
              border: `1px solid ${COLORS.line}`,
              backgroundColor: COLORS.surface,
              boxShadow: "0 40px 110px rgba(0,0,0,0.6)",
              padding: "8px 28px",
              minHeight: 360,
            }}
          >
            {KERNEL_EVENTS.map(([type, value], index) => {
              const row = springIn(frame, fps, timing.rows + index * 6);
              return (
                <div
                  key={type}
                  style={{
                    display: "grid",
                    gridTemplateColumns: "205px 1fr",
                    gap: 18,
                    padding: "19px 0 19px 26px",
                    borderTop: index === 0 ? "none" : `1px solid ${COLORS.line}`,
                    borderLeft: `3px solid ${index < 2 ? COLORS.amber : COLORS.red}`,
                    opacity: Math.min(1, row * 1.35),
                    translate: `0px ${(1 - row) * 16}px`,
                    fontFamily: FONTS.mono,
                    fontSize: 22,
                  }}
                >
                  <span
                    style={{
                      color: index < 2 ? COLORS.amber : COLORS.red,
                      fontWeight: 700,
                    }}
                  >
                    + {type}
                  </span>
                  <span style={{ color: COLORS.white }}>{value}</span>
                </div>
              );
            })}
          </div>
        </div>
      </div>

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
          text="The lockfile was clean. Runtime wasn't."
          frame={frame}
          startAt={timing.closer + 1}
          fontSize={98}
          align="center"
          maxWidth={1480}
          accentWords={["Runtime"]}
          accentColor={COLORS.red}
        />
      </div>
    </AbsoluteFill>
  );
};
