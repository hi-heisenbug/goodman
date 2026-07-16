import { AbsoluteFill, useCurrentFrame } from "remotion";
import { BrandMark } from "../components/BrandMark";
import { SceneBackground } from "../components/SceneBackground";
import { SceneLabel } from "../components/SceneLabel";
import { progress } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

const BEHAVIORS = [
  ["SECRET READ", "READ /home/app/.npmrc", COLORS.red],
  ["CLOUD METADATA", "CONNECT 169.254.169.254:80", COLORS.amber],
  ["NEW OUTBOUND", "CONNECT 203.0.113.42:443", COLORS.red],
  ["NEW EXEC", "EXEC /bin/sh", COLORS.red],
] as const;

export const AttackPathScene: React.FC = () => {
  const frame = useCurrentFrame();
  const packageReveal = progress(frame, 20, 24);
  const summary = progress(frame, 116, 24);

  return (
    <AbsoluteFill>
      <SceneBackground accent={COLORS.red} />
      <div style={{ position: "absolute", left: SAFE_X, top: 58 }}>
        <BrandMark light compact />
      </div>
      <div style={{ position: "absolute", left: SAFE_X, top: 145 }}>
        <SceneLabel>Explainable evidence, grouped correctly</SceneLabel>
        <div
          style={{
            marginTop: 14,
            color: COLORS.white,
            fontFamily: FONTS.heading,
            fontSize: 72,
            letterSpacing: -3,
          }}
        >
          Four behaviors. One package update.
        </div>
      </div>

      <svg
        viewBox="0 0 1920 1080"
        style={{ position: "absolute", inset: 0, width: "100%", height: "100%" }}
      >
        {BEHAVIORS.map((_, index) => {
          const line = progress(frame, 54 + index * 14, 24);
          const y = 404 + index * 126;
          return (
            <path
              key={y}
              d={`M 790 570 C 930 570, 900 ${y}, 1040 ${y}`}
              fill="none"
              stroke={index === 1 ? COLORS.amber : COLORS.red}
              strokeWidth="3"
              pathLength="1"
              strokeDashoffset={1 - line}
              strokeDasharray="1"
              opacity={0.7}
            />
          );
        })}
      </svg>

      <div
        style={{
          position: "absolute",
          left: 160,
          top: 395,
          width: 630,
          padding: 38,
          borderRadius: 18,
          border: "1px solid rgba(242,163,58,0.55)",
          backgroundColor: "rgba(10,15,13,0.94)",
          boxShadow: "0 35px 100px rgba(0,0,0,0.42)",
          opacity: packageReveal,
          translate: `${(1 - packageReveal) * -70}px 0px`,
        }}
      >
        <div style={{ color: COLORS.amber, fontSize: 19, fontWeight: 700 }}>
          PACKAGE UPDATE
        </div>
        <div
          style={{
            marginTop: 18,
            color: COLORS.white,
            fontFamily: FONTS.heading,
            fontSize: 50,
            lineHeight: 1.04,
            overflowWrap: "anywhere",
          }}
        >
          mini-shai-hulud-loader
        </div>
        <div
          style={{
            marginTop: 18,
            display: "flex",
            gap: 14,
            alignItems: "center",
            color: "rgba(255,255,255,0.6)",
            fontFamily: FONTS.mono,
            fontSize: 26,
          }}
        >
          <span>1.0.0</span>
          <span style={{ color: COLORS.lime }}>→</span>
          <span style={{ color: COLORS.white }}>1.0.1</span>
        </div>
        <div
          style={{
            marginTop: 30,
            paddingTop: 24,
            borderTop: "1px solid rgba(255,255,255,0.1)",
            color: "rgba(255,255,255,0.58)",
            fontSize: 21,
            lineHeight: 1.45,
          }}
        >
          Same service. Same deployment. New package behavior.
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          left: 1040,
          top: 342,
          width: 720,
          display: "grid",
          gap: 16,
        }}
      >
        {BEHAVIORS.map(([label, detail, color], index) => {
          const reveal = progress(frame, 48 + index * 15, 18);
          return (
            <div
              key={label}
              style={{
                display: "grid",
                gridTemplateColumns: "190px 1fr",
                alignItems: "center",
                gap: 22,
                minHeight: 104,
                padding: "18px 24px",
                borderRadius: 14,
                border: `1px solid ${color}55`,
                backgroundColor: "rgba(8,13,11,0.92)",
                opacity: reveal,
                translate: `${(1 - reveal) * 60}px 0px`,
              }}
            >
              <div
                style={{
                  padding: "10px 12px",
                  borderRadius: 7,
                  backgroundColor: `${color}22`,
                  color,
                  fontSize: 16,
                  fontWeight: 700,
                  letterSpacing: 1,
                  textAlign: "center",
                }}
              >
                {label}
              </div>
              <div style={{ color: COLORS.white, fontFamily: FONTS.mono, fontSize: 22 }}>
                {detail}
              </div>
            </div>
          );
        })}
      </div>

      <div
        style={{
          position: "absolute",
          left: 540,
          bottom: 66,
          padding: "18px 34px",
          borderRadius: 12,
          backgroundColor: COLORS.red,
          color: COLORS.white,
          fontFamily: FONTS.heading,
          fontSize: 30,
          opacity: summary,
          scale: 0.9 + summary * 0.1,
        }}
      >
        One alert operators can act on.
      </div>
    </AbsoluteFill>
  );
};
