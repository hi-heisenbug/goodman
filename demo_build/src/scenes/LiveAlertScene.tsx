import { AbsoluteFill, useCurrentFrame } from "remotion";
import { BrandMark } from "../components/BrandMark";
import { BrowserFrame } from "../components/BrowserFrame";
import { SceneBackground } from "../components/SceneBackground";
import { SceneLabel } from "../components/SceneLabel";
import { progress } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

export const LiveAlertScene: React.FC = () => {
  const frame = useCurrentFrame();
  const title = progress(frame, 3, 24);
  const callout = progress(frame, 112, 26);

  return (
    <AbsoluteFill>
      <SceneBackground tone="light" accent={COLORS.red} />
      <div style={{ position: "absolute", left: SAFE_X, top: 52 }}>
        <BrandMark compact />
      </div>
      <div
        style={{
          position: "absolute",
          left: 330,
          right: 100,
          top: 47,
          display: "flex",
          alignItems: "baseline",
          justifyContent: "space-between",
          opacity: title,
        }}
      >
        <div>
          <SceneLabel light={false}>Live attack replay</SceneLabel>
          <div
            style={{
              marginTop: 8,
              color: COLORS.ink,
              fontFamily: FONTS.heading,
              fontSize: 58,
              letterSpacing: -2.5,
            }}
          >
            One live alert. One culprit package.
          </div>
        </div>
        <div
          style={{
            color: COLORS.charcoal,
            fontFamily: FONTS.mono,
            fontSize: 18,
          }}
        >
          Mini-Shai-Hulud / benign replay
        </div>
      </div>

      <div style={{ position: "absolute", left: 150, top: 185 }}>
        <BrowserFrame
          screenshot="02_mini_shai_hulud.png"
          frame={frame}
          width={1460}
          zoomAt={82}
          zoom={1.32}
          focus="58% 66%"
          shiftX={-110}
        />
      </div>

      <div
        style={{
          position: "absolute",
          right: 90,
          top: 380,
          width: 460,
          padding: 30,
          borderRadius: 16,
          border: "1px solid rgba(213,63,79,0.34)",
          backgroundColor: "rgba(5,8,7,0.94)",
          boxShadow: "0 30px 90px rgba(0,0,0,0.35)",
          color: COLORS.white,
          opacity: callout,
          translate: `${(1 - callout) * 70}px 0px`,
        }}
      >
        <div
          style={{
            color: COLORS.red,
            fontSize: 20,
            fontWeight: 700,
            letterSpacing: 3,
          }}
        >
          CRITICAL DRIFT
        </div>
        <div
          style={{
            marginTop: 18,
            fontFamily: FONTS.heading,
            fontSize: 38,
            lineHeight: 1.05,
            overflowWrap: "anywhere",
          }}
        >
          mini-shai-hulud-loader
        </div>
        <div
          style={{
            marginTop: 12,
            color: "rgba(255,255,255,0.6)",
            fontFamily: FONTS.mono,
            fontSize: 22,
          }}
        >
          1.0.0 → 1.0.1
        </div>
        <div
          style={{
            marginTop: 28,
            display: "grid",
            gridTemplateColumns: "1fr 1fr",
            gap: 10,
          }}
        >
          {["secret read", "cloud metadata", "new C2", "exec /bin/sh"].map(
            (label, index) => (
              <div
                key={label}
                style={{
                  padding: "12px 14px",
                  borderRadius: 8,
                  backgroundColor:
                    index === 3 ? COLORS.red : "rgba(213,63,79,0.14)",
                  color: index === 3 ? COLORS.white : "#ff9eaa",
                  fontSize: 17,
                  fontWeight: 700,
                }}
              >
                {label}
              </div>
            ),
          )}
        </div>
        <div
          style={{
            marginTop: 24,
            paddingTop: 20,
            borderTop: "1px solid rgba(255,255,255,0.12)",
            color: COLORS.mint,
            fontSize: 20,
            fontWeight: 700,
          }}
        >
          Rollback template ready
        </div>
      </div>
    </AbsoluteFill>
  );
};
