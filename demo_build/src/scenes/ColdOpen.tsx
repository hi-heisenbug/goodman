import { AbsoluteFill, interpolate, useCurrentFrame } from "remotion";
import { BrandMark } from "../components/BrandMark";
import { SceneBackground } from "../components/SceneBackground";
import { SceneLabel } from "../components/SceneLabel";
import { fadeWindow, progress } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

const EVENTS = [
  ["READ", "/home/app/.npmrc"],
  ["CONNECT", "169.254.169.254:80"],
  ["EXEC", "/bin/sh"],
] as const;

export const ColdOpen: React.FC = () => {
  const frame = useCurrentFrame();
  const first = fadeWindow(frame, 0, 14, 52, 70);
  const second = fadeWindow(frame, 58, 75, 112, 132);
  const final = progress(frame, 124, 24);

  return (
    <AbsoluteFill>
      <SceneBackground accent={COLORS.red} />
      <div style={{ position: "absolute", left: SAFE_X, top: 72 }}>
        <BrandMark light compact />
      </div>
      <div
        style={{
          position: "absolute",
          right: SAFE_X,
          top: 78,
          color: "rgba(255,255,255,0.48)",
          fontFamily: FONTS.mono,
          fontSize: 18,
          letterSpacing: 1.5,
        }}
      >
        PROD / RUNTIME / 02:17:41
      </div>

      <div
        style={{
          position: "absolute",
          inset: "250px 140px 160px",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          opacity: first,
          translate: `0px ${interpolate(first, [0, 1], [42, 0])}px`,
        }}
      >
        <div style={{ textAlign: "center" }}>
          <SceneLabel>A routine release</SceneLabel>
          <div
            style={{
              marginTop: 28,
              color: COLORS.white,
              fontFamily: FONTS.heading,
              fontSize: 124,
              letterSpacing: -5,
              lineHeight: 0.98,
            }}
          >
            A dependency updated.
          </div>
          <div
            style={{
              width: 230 * first,
              height: 10,
              margin: "36px auto 0",
              borderRadius: 8,
              backgroundColor: COLORS.lime,
            }}
          />
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          inset: "220px 150px 130px",
          display: "grid",
          gridTemplateColumns: "0.9fr 1.1fr",
          alignItems: "center",
          gap: 100,
          opacity: second,
        }}
      >
        <div>
          <SceneLabel>Seconds later</SceneLabel>
          <div
            style={{
              marginTop: 28,
              color: COLORS.white,
              fontFamily: FONTS.heading,
              fontSize: 88,
              letterSpacing: -3,
              lineHeight: 1.02,
            }}
          >
            The syscall trail changed.
          </div>
        </div>
        <div
          style={{
            padding: 34,
            borderRadius: 16,
            border: "1px solid rgba(213,63,79,0.5)",
            backgroundColor: "rgba(8,12,11,0.88)",
            boxShadow: "0 30px 90px rgba(0,0,0,0.45)",
          }}
        >
          <div
            style={{
              color: "rgba(255,255,255,0.46)",
              fontFamily: FONTS.mono,
              fontSize: 19,
              marginBottom: 18,
            }}
          >
            live kernel events
          </div>
          {EVENTS.map(([type, value], index) => {
            const row = progress(frame, 70 + index * 12, 14);
            return (
              <div
                key={type}
                style={{
                  display: "grid",
                  gridTemplateColumns: "160px 1fr",
                  gap: 20,
                  padding: "18px 0",
                  borderTop: "1px solid rgba(255,255,255,0.08)",
                  opacity: row,
                  translate: `${(1 - row) * 30}px 0px`,
                  fontFamily: FONTS.mono,
                  fontSize: 24,
                }}
              >
                <span style={{ color: COLORS.red }}>+ {type}</span>
                <span style={{ color: COLORS.white }}>{value}</span>
              </div>
            );
          })}
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          inset: "250px 120px 120px",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          textAlign: "center",
          opacity: final,
          scale: 0.92 + final * 0.08,
        }}
      >
        <div>
          <SceneLabel>The question every scanner misses</SceneLabel>
          <div
            style={{
              marginTop: 28,
              color: COLORS.white,
              fontFamily: FONTS.heading,
              fontSize: 138,
              letterSpacing: -6,
              lineHeight: 0.95,
            }}
          >
            Which package did it?
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};
