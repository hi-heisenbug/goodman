import { AbsoluteFill, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { BorderBeam } from "../components/BorderBeam";
import { BrandMark } from "../components/BrandMark";
import { KineticHeadline } from "../components/KineticHeadline";
import { SceneLabel } from "../components/SceneLabel";
import { progress, springIn, verdictPop } from "../motion";
import { COLORS, FONTS, SAFE_X, TNUM } from "../theme";

const EVENTS = [
  ["SECRET READ", "READ /home/app/.npmrc", COLORS.amber],
  ["CLOUD METADATA", "CONNECT 169.254.169.254:80", COLORS.amber],
  ["NEW OUTBOUND", "CONNECT 203.0.113.42:443", COLORS.red],
  ["NEW EXEC", "EXEC /bin/sh", COLORS.red],
] as const;

const EVENT_START = 52;

type KillChainSceneProps = {
  readonly verdictAt?: number;
};

export const KillChainScene: React.FC<KillChainSceneProps> = ({
  verdictAt = 150,
}) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const packageIn = springIn(frame, fps, 26);
  const verdict = verdictPop(frame, fps, verdictAt);

  return (
    <AbsoluteFill>
      <Backdrop accent={COLORS.green} glowX="30%" glowY="55%" glowOpacity={0.1} />
      <div style={{ position: "absolute", left: SAFE_X, top: 62 }}>
        <BrandMark compact />
      </div>
      <div style={{ position: "absolute", left: SAFE_X, top: 128 }}>
        <SceneLabel>Package attribution, end to end</SceneLabel>
        <div style={{ marginTop: 14 }}>
          <KineticHeadline
            text="From syscall to culprit package."
            frame={frame}
            startAt={6}
            fontSize={66}
            maxWidth={1400}
            accentWords={["culprit", "package"]}
            accentColor={COLORS.lime}
          />
        </div>
      </div>

      <svg
        viewBox="0 0 1920 1080"
        style={{ position: "absolute", inset: 0, width: "100%", height: "100%" }}
      >
        {EVENTS.map((event, index) => {
          const draw = progress(frame, EVENT_START + index * 8, 20);
          const y = 388 + index * 122;
          return (
            <path
              key={event[0]}
              d={`M 760 560 C 900 560, 880 ${y}, 1020 ${y}`}
              fill="none"
              stroke={event[2]}
              strokeWidth="2.5"
              pathLength="1"
              strokeDasharray="1"
              strokeDashoffset={1 - draw}
              opacity={0.55}
            />
          );
        })}
      </svg>

      <div
        style={{
          position: "absolute",
          left: SAFE_X,
          top: 388,
          width: 620,
          padding: 40,
          borderRadius: 16,
          border: "1px solid rgba(251,191,36,0.4)",
          backgroundColor: "rgba(13,17,23,0.97)",
          boxShadow: "0 40px 110px rgba(0,0,0,0.6)",
          opacity: Math.min(1, packageIn * 1.3),
          translate: `${(1 - packageIn) * -50}px 0px`,
        }}
      >
        <div
          style={{
            color: COLORS.amber,
            fontFamily: FONTS.mono,
            fontSize: 18,
            fontWeight: 700,
            letterSpacing: 3,
          }}
        >
          PACKAGE UPDATE
        </div>
        <div
          style={{
            marginTop: 18,
            color: COLORS.white,
            fontFamily: FONTS.heading,
            fontWeight: 700,
            fontSize: 48,
            lineHeight: 1.05,
            overflowWrap: "anywhere",
          }}
        >
          mini-shai-hulud-loader
        </div>
        <div
          style={{
            marginTop: 16,
            display: "flex",
            gap: 14,
            alignItems: "center",
            color: COLORS.muted,
            fontFamily: FONTS.mono,
            fontSize: 26,
            ...TNUM,
          }}
        >
          <span>1.0.0</span>
          <span style={{ color: COLORS.lime }}>→</span>
          <span style={{ color: COLORS.white }}>1.0.1</span>
        </div>
        <div
          style={{
            marginTop: 28,
            paddingTop: 24,
            borderTop: `1px solid ${COLORS.line}`,
            color: COLORS.muted,
            fontFamily: FONTS.body,
            fontSize: 21,
            lineHeight: 1.5,
          }}
        >
          Same service. Same deployment. New behavior — caught by drift, not
          signatures.
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          left: 1020,
          top: 336,
          width: 730,
          display: "grid",
          gap: 15,
        }}
      >
        {EVENTS.map(([label, detail, color], index) => {
          const reveal = springIn(frame, fps, EVENT_START + index * 8);
          return (
            <div
              key={label}
              style={{
                display: "grid",
                gridTemplateColumns: "205px 1fr",
                alignItems: "center",
                gap: 22,
                minHeight: 100,
                padding: "18px 26px",
                borderRadius: 14,
                border: `1px solid ${COLORS.line}`,
                borderLeft: `3px solid ${color}`,
                backgroundColor: "rgba(13,17,23,0.94)",
                opacity: Math.min(1, reveal * 1.3),
                translate: `${(1 - reveal) * 40}px 0px`,
              }}
            >
              <div
                style={{
                  padding: "10px 12px",
                  borderRadius: 7,
                  backgroundColor: `${color}1c`,
                  border: `1px solid ${color}44`,
                  color,
                  fontFamily: FONTS.mono,
                  fontSize: 16,
                  fontWeight: 700,
                  letterSpacing: 1,
                  textAlign: "center",
                }}
              >
                {label}
              </div>
              <div
                style={{
                  color: COLORS.white,
                  fontFamily: FONTS.mono,
                  fontSize: 22,
                }}
              >
                {detail}
              </div>
            </div>
          );
        })}
      </div>

      <div
        style={{
          position: "absolute",
          left: 0,
          right: 0,
          bottom: 62,
          display: "flex",
          justifyContent: "center",
          opacity: Math.min(1, verdict * 1.2),
          scale: 0.92 + verdict * 0.08,
        }}
      >
        <div
          style={{
            position: "relative",
            display: "flex",
            alignItems: "center",
            gap: 28,
            padding: "22px 40px",
            borderRadius: 14,
            backgroundColor: "rgba(13,17,23,0.97)",
            border: "1px solid rgba(147,203,82,0.4)",
            boxShadow: `0 0 70px -18px ${COLORS.lime}88`,
          }}
        >
          <BorderBeam color={COLORS.lime} borderRadius={14} />
          <span
            style={{
              color: COLORS.lime,
              fontFamily: FONTS.mono,
              fontSize: 21,
              fontWeight: 700,
              letterSpacing: 3,
            }}
          >
            ATTRIBUTED
          </span>
          <span
            style={{
              color: COLORS.white,
              fontFamily: FONTS.mono,
              fontSize: 27,
              fontWeight: 700,
            }}
          >
            mini-shai-hulud-loader@1.0.1
          </span>
          <span
            style={{
              padding: "9px 16px",
              borderRadius: 8,
              backgroundColor: COLORS.red,
              color: "#ffffff",
              fontFamily: FONTS.mono,
              fontSize: 18,
              fontWeight: 700,
              letterSpacing: 1.5,
            }}
          >
            CRITICAL
          </span>
        </div>
      </div>
    </AbsoluteFill>
  );
};
