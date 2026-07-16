import { AbsoluteFill, useCurrentFrame } from "remotion";
import { BrandMark } from "../components/BrandMark";
import { SceneBackground } from "../components/SceneBackground";
import { SceneLabel } from "../components/SceneLabel";
import { fadeWindow, progress } from "../motion";
import { COLORS, FONTS } from "../theme";

const STEPS = [
  ["01", "Package update"],
  ["02", "Behavior drift"],
  ["03", "Culprit package"],
  ["04", "Rollback"],
] as const;

export const ClosingScene: React.FC = () => {
  const frame = useCurrentFrame();
  const outcome = fadeWindow(frame, 0, 20, 96, 112);
  const final = progress(frame, 110, 26);

  return (
    <AbsoluteFill>
      <SceneBackground accent={COLORS.lime} />

      <div
        style={{
          position: "absolute",
          inset: "150px 120px 100px",
          opacity: outcome,
        }}
      >
        <div style={{ textAlign: "center" }}>
          <SceneLabel>The operational outcome</SceneLabel>
          <div
            style={{
              marginTop: 22,
              color: COLORS.white,
              fontFamily: FONTS.heading,
              fontSize: 94,
              letterSpacing: -4,
              lineHeight: 0.98,
            }}
          >
            From package update to rollback in seconds.
          </div>
        </div>

        <div
          style={{
            position: "relative",
            marginTop: 120,
            display: "grid",
            gridTemplateColumns: "repeat(4, 1fr)",
            gap: 34,
          }}
        >
          <div
            style={{
              position: "absolute",
              left: 170,
              right: 170,
              top: 45,
              height: 4,
              backgroundColor: "rgba(255,255,255,0.12)",
            }}
          >
            <div
              style={{
                width: `${progress(frame, 42, 66) * 100}%`,
                height: "100%",
                background: `linear-gradient(90deg, ${COLORS.lime}, ${COLORS.green}, ${COLORS.red})`,
              }}
            />
          </div>
          {STEPS.map(([number, label], index) => {
            const reveal = progress(frame, 34 + index * 18, 18);
            const isLast = index === STEPS.length - 1;
            return (
              <div key={number} style={{ textAlign: "center", opacity: reveal }}>
                <div
                  style={{
                    width: 94,
                    height: 94,
                    margin: "0 auto",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    borderRadius: 22,
                    backgroundColor: isLast ? COLORS.red : COLORS.mint,
                    color: isLast ? COLORS.white : COLORS.ink,
                    fontFamily: FONTS.heading,
                    fontSize: 32,
                    scale: 0.82 + reveal * 0.18,
                    boxShadow: isLast
                      ? "0 20px 60px rgba(213,63,79,0.3)"
                      : "0 20px 60px rgba(28,151,112,0.18)",
                  }}
                >
                  {number}
                </div>
                <div
                  style={{
                    marginTop: 24,
                    color: COLORS.white,
                    fontFamily: FONTS.heading,
                    fontSize: 30,
                  }}
                >
                  {label}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          textAlign: "center",
          opacity: final,
          scale: 0.94 + final * 0.06,
        }}
      >
        <BrandMark light />
        <div
          style={{
            marginTop: 58,
            maxWidth: 1350,
            color: COLORS.white,
            fontFamily: FONTS.heading,
            fontSize: 102,
            lineHeight: 0.98,
            letterSpacing: -4.5,
          }}
        >
          See which dependency actually did it.
        </div>
        <div
          style={{
            marginTop: 46,
            padding: "17px 26px",
            borderRadius: 10,
            backgroundColor: COLORS.mint,
            color: COLORS.ink,
            fontFamily: FONTS.mono,
            fontSize: 25,
            fontWeight: 700,
          }}
        >
          github.com/hi-heisenbug/goodman
        </div>
        <div
          style={{
            marginTop: 28,
            color: "rgba(255,255,255,0.5)",
            fontSize: 21,
            letterSpacing: 1.2,
          }}
        >
          Runtime dependency security at package precision
        </div>
      </div>
    </AbsoluteFill>
  );
};
