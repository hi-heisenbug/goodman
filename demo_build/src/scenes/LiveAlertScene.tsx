import { AbsoluteFill, interpolate, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { BorderBeam } from "../components/BorderBeam";
import { BrandMark } from "../components/BrandMark";
import { KineticHeadline } from "../components/KineticHeadline";
import { SceneLabel } from "../components/SceneLabel";
import { WalkthroughFrame } from "../components/WalkthroughFrame";
import { progress, springIn, verdictPop } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

// The recording plays from the first frame of the scene, so the 8.8s alerts
// segment must cover the scene: rate <= 8.8 * fps / durationInFrames. At the
// master's 0.88x the live alert (4.4s into the segment) lands at frame 150
// and the rollback copy confirmation (5.45s) at frame 186; at the X cut's
// 1.0x they land at 132 and 164.
const VIDEO_ENTER = 14;

type LiveAlertSceneProps = {
  readonly playbackRate?: number;
};

export const LiveAlertScene: React.FC<LiveAlertSceneProps> = ({
  playbackRate = 0.88,
}) => {
  const RATE = playbackRate;
  const ALERT_FRAME = Math.round((4.4 / RATE) * 30);
  const COPY_FRAME = Math.round((5.45 / RATE) * 30);
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const title = progress(frame, 4, 20);
  const callout = verdictPop(frame, fps, ALERT_FRAME + 4);
  const copied = springIn(frame, fps, COPY_FRAME + 16);
  const flash = interpolate(
    frame,
    [ALERT_FRAME, ALERT_FRAME + 2, ALERT_FRAME + 8],
    [0, 0.15, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
  );

  return (
    <AbsoluteFill>
      <Backdrop accent={COLORS.red} glowX="70%" glowOpacity={0.1} />
      <div style={{ position: "absolute", left: SAFE_X, top: 62, opacity: title }}>
        <BrandMark compact />
      </div>
      <div
        style={{
          position: "absolute",
          left: SAFE_X,
          top: 128,
          opacity: title,
        }}
      >
        <SceneLabel color={COLORS.red}>Live attack replay</SceneLabel>
        <div style={{ marginTop: 14 }}>
          <KineticHeadline
            text="One live alert. One culprit package."
            frame={frame}
            startAt={8}
            fontSize={62}
            maxWidth={1300}
          />
        </div>
      </div>

      <div style={{ position: "absolute", left: SAFE_X - 10, top: 292 }}>
        <WalkthroughFrame
          segment="alerts"
          frame={frame}
          width={1310}
          enterAt={VIDEO_ENTER}
          playbackRate={RATE}
          zoomAt={ALERT_FRAME - 20}
          zoom={1.22}
          focus="56% 60%"
          shiftX={-60}
          glow={COLORS.red}
        />
      </div>

      <div
        style={{
          position: "absolute",
          right: SAFE_X - 20,
          top: 356,
          width: 470,
          padding: 34,
          borderRadius: 16,
          border: "1px solid rgba(255,80,96,0.35)",
          backgroundColor: "rgba(13,17,23,0.97)",
          boxShadow: "0 40px 110px rgba(0,0,0,0.7)",
          color: COLORS.white,
          opacity: Math.min(1, callout * 1.2),
          scale: 0.94 + callout * 0.06,
        }}
      >
        <BorderBeam color={COLORS.red} borderRadius={16} />
        <div
          style={{
            color: COLORS.red,
            fontFamily: FONTS.mono,
            fontSize: 19,
            fontWeight: 700,
            letterSpacing: 3.5,
          }}
        >
          CRITICAL DRIFT · LIVE
        </div>
        <div
          style={{
            marginTop: 20,
            fontFamily: FONTS.heading,
            fontWeight: 700,
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
            color: COLORS.muted,
            fontFamily: FONTS.mono,
            fontSize: 22,
          }}
        >
          1.0.0 → 1.0.1
        </div>
        <div
          style={{
            marginTop: 26,
            display: "grid",
            gridTemplateColumns: "1fr 1fr",
            gap: 10,
          }}
        >
          {["secret read", "cloud metadata", "new C2", "exec /bin/sh"].map(
            (label, index) => {
              const chip = springIn(frame, fps, ALERT_FRAME + 12 + index * 4);
              return (
                <div
                  key={label}
                  style={{
                    padding: "12px 14px",
                    borderRadius: 8,
                    backgroundColor: "rgba(255,80,96,0.12)",
                    border: "1px solid rgba(255,80,96,0.3)",
                    color: "#ff9eaa",
                    fontFamily: FONTS.mono,
                    fontSize: 17,
                    fontWeight: 700,
                    opacity: Math.min(1, chip * 1.4),
                    translate: `0px ${(1 - chip) * 12}px`,
                  }}
                >
                  {label}
                </div>
              );
            },
          )}
        </div>
        <div
          style={{
            marginTop: 26,
            paddingTop: 22,
            borderTop: `1px solid ${COLORS.line}`,
            display: "flex",
            alignItems: "center",
            gap: 12,
            color: COLORS.mint,
            fontFamily: FONTS.mono,
            fontSize: 19,
            fontWeight: 700,
            opacity: Math.min(1, copied * 1.3),
            translate: `0px ${(1 - copied) * 10}px`,
          }}
        >
          <span style={{ color: COLORS.lime, fontSize: 24 }}>✓</span>
          Rollback command copied
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          right: SAFE_X,
          bottom: 44,
          color: COLORS.faint,
          fontFamily: FONTS.mono,
          fontSize: 16,
          letterSpacing: 1,
        }}
      >
        recorded live · benign replay of Shai-Hulud behaviors
      </div>

      <AbsoluteFill
        style={{
          backgroundColor: COLORS.red,
          opacity: flash,
          pointerEvents: "none",
        }}
      />
    </AbsoluteFill>
  );
};
