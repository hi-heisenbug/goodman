import { Video } from "@remotion/media";
import { staticFile, useVideoConfig } from "remotion";
import plan from "../../interaction_plan.json";
import { progress } from "../motion";
import { COLORS, FONTS } from "../theme";

type SegmentName = keyof typeof plan.segments;

type WalkthroughFrameProps = {
  readonly segment: SegmentName;
  readonly frame: number;
  readonly width?: number;
  readonly enterAt?: number;
  readonly zoomAt?: number;
  readonly zoom?: number;
  readonly focus?: string;
  readonly shiftX?: number;
  readonly playbackRate?: number;
  readonly glow?: string;
};

export const WalkthroughFrame: React.FC<WalkthroughFrameProps> = ({
  segment,
  frame,
  width = 1420,
  enterAt = 10,
  zoomAt = 90,
  zoom = 1.08,
  focus = "50% 50%",
  shiftX = 0,
  playbackRate = 1,
  glow = COLORS.green,
}) => {
  const { fps } = useVideoConfig();
  const timing = plan.segments[segment];
  const enter = progress(frame, enterAt, 26);
  const zoomProgress = progress(frame, zoomAt, 48);
  const videoHeight = width * (9 / 16);

  return (
    <div
      style={{
        width,
        borderRadius: 16,
        overflow: "hidden",
        border: "1px solid rgba(255,255,255,0.12)",
        backgroundColor: COLORS.surface,
        boxShadow: `0 50px 130px rgba(0,0,0,0.65), 0 0 90px -30px ${glow}55`,
        opacity: enter,
        scale: 0.96 + enter * 0.04,
        translate: `${(1 - enter) * 50 + shiftX * zoomProgress}px ${(1 - enter) * 70}px`,
      }}
    >
      <div
        style={{
          height: 46,
          display: "flex",
          alignItems: "center",
          gap: 9,
          padding: "0 20px",
          backgroundColor: COLORS.surfaceRaised,
          borderBottom: `1px solid ${COLORS.line}`,
        }}
      >
        {[0, 1, 2].map((index) => (
          <div
            key={index}
            style={{
              width: 12,
              height: 12,
              borderRadius: "50%",
              backgroundColor: "#3a3f47",
            }}
          />
        ))}
        <div
          style={{
            marginLeft: 14,
            color: COLORS.muted,
            fontFamily: FONTS.mono,
            fontSize: 17,
            letterSpacing: 0.3,
          }}
        >
          goodman.local — live product
        </div>
        <div
          style={{
            marginLeft: "auto",
            display: "flex",
            alignItems: "center",
            gap: 8,
            color: COLORS.lime,
            fontFamily: FONTS.mono,
            fontSize: 15,
            fontWeight: 700,
            letterSpacing: 2,
          }}
        >
          <span
            style={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              backgroundColor: COLORS.lime,
              boxShadow: `0 0 10px ${COLORS.lime}`,
            }}
          />
          LIVE
        </div>
      </div>
      <div style={{ width, height: videoHeight, overflow: "hidden" }}>
        <Video
          src={staticFile(`recordings/${plan.output}`)}
          trimBefore={Math.round(timing.start * fps)}
          trimAfter={Math.round(timing.end * fps)}
          playbackRate={playbackRate}
          objectFit="cover"
          muted
          style={{
            width: "100%",
            height: "100%",
            scale: 1 + (zoom - 1) * zoomProgress,
            transformOrigin: focus,
          }}
        />
      </div>
    </div>
  );
};
