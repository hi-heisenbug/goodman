import { AbsoluteFill, interpolate, useCurrentFrame } from "remotion";
import { BrandMark } from "../components/BrandMark";
import { SceneBackground } from "../components/SceneBackground";
import { SceneLabel } from "../components/SceneLabel";
import { progress } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

const NODES = [
  ["01", "Kernel event", "READ /home/app/.npmrc", COLORS.green],
  ["02", "User stack", "V8 frame → source path", COLORS.lime],
  ["03", "Package", "mini-shai-hulud-loader@1.0.1", COLORS.amber],
  ["04", "Drift alert", "new secret-read", COLORS.red],
] as const;

export const AttributionScene: React.FC = () => {
  const frame = useCurrentFrame();
  const headline = progress(frame, 4, 28);
  const result = progress(frame, 132, 28);

  return (
    <AbsoluteFill>
      <SceneBackground accent={COLORS.green} />
      <div style={{ position: "absolute", left: SAFE_X, top: 64 }}>
        <BrandMark light compact />
      </div>
      <div
        style={{
          position: "absolute",
          left: SAFE_X,
          right: SAFE_X,
          top: 150,
          opacity: headline,
          translate: `0px ${(1 - headline) * 34}px`,
        }}
      >
        <SceneLabel>Package attribution, end to end</SceneLabel>
        <div
          style={{
            marginTop: 20,
            maxWidth: 1320,
            color: COLORS.white,
            fontFamily: FONTS.heading,
            fontSize: 82,
            lineHeight: 1.02,
            letterSpacing: -3.5,
          }}
        >
          Goodman finds the package behind the syscall.
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          left: 120,
          right: 120,
          top: 450,
          display: "grid",
          gridTemplateColumns: "repeat(4, 1fr)",
          gap: 28,
        }}
      >
        <div
          style={{
            position: "absolute",
            left: 180,
            right: 180,
            top: 73,
            height: 3,
            backgroundColor: "rgba(190,243,226,0.12)",
          }}
        >
          <div
            style={{
              width: `${progress(frame, 52, 74) * 100}%`,
              height: "100%",
              background: `linear-gradient(90deg, ${COLORS.green}, ${COLORS.lime}, ${COLORS.red})`,
              boxShadow: `0 0 22px ${COLORS.green}`,
            }}
          />
        </div>
        {NODES.map(([number, title, detail, color], index) => {
          const reveal = progress(frame, 42 + index * 22, 22);
          const active = progress(frame, 48 + index * 22, 14);
          return (
            <div
              key={number}
              style={{
                position: "relative",
                minHeight: 250,
                padding: "90px 28px 30px",
                borderRadius: 16,
                border: `1px solid ${color}66`,
                backgroundColor: "rgba(8,14,12,0.9)",
                boxShadow: `0 ${22 * active}px ${50 * active}px rgba(0,0,0,0.32)`,
                opacity: reveal,
                translate: `0px ${(1 - reveal) * 54}px`,
              }}
            >
              <div
                style={{
                  position: "absolute",
                  left: 28,
                  top: 42,
                  width: 62,
                  height: 62,
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  borderRadius: 14,
                  backgroundColor: color,
                  color: COLORS.ink,
                  fontFamily: FONTS.heading,
                  fontSize: 24,
                  scale: 0.84 + active * 0.16,
                }}
              >
                {number}
              </div>
              <div
                style={{
                  color: COLORS.white,
                  fontFamily: FONTS.heading,
                  fontSize: 34,
                  marginBottom: 16,
                }}
              >
                {title}
              </div>
              <div
                style={{
                  color: "rgba(255,255,255,0.62)",
                  fontFamily: index === 0 || index === 2 ? FONTS.mono : FONTS.body,
                  fontSize: 21,
                  lineHeight: 1.45,
                  overflowWrap: "anywhere",
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
          left: 320,
          right: 320,
          bottom: 78,
          padding: "24px 36px",
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          borderRadius: 14,
          backgroundColor: COLORS.mint,
          color: COLORS.ink,
          opacity: result,
          scale: 0.96 + result * 0.04,
          boxShadow: "0 20px 70px rgba(28,151,112,0.25)",
        }}
      >
        <span style={{ fontSize: 24, fontWeight: 700 }}>ATTRIBUTED PACKAGE</span>
        <span style={{ fontFamily: FONTS.mono, fontSize: 30, fontWeight: 700 }}>
          mini-shai-hulud-loader@1.0.1
        </span>
        <span
          style={{
            padding: "10px 18px",
            borderRadius: 8,
            backgroundColor: COLORS.red,
            color: COLORS.white,
            fontSize: 20,
            fontWeight: 700,
          }}
        >
          CRITICAL
        </span>
      </div>
      <div
        style={{
          position: "absolute",
          right: 110,
          bottom: 30,
          color: "rgba(255,255,255,0.32)",
          fontFamily: FONTS.mono,
          fontSize: 16,
        }}
      >
        {Math.round(interpolate(frame, [0, 195], [0, 858]))} events resolved
      </div>
    </AbsoluteFill>
  );
};
