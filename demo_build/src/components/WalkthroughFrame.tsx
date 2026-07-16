import { Video } from "@remotion/media";
import { staticFile, useVideoConfig } from "remotion";
import plan from "../../interaction_plan.json";
import { progress } from "../motion";
import { COLORS } from "../theme";

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
        borderRadius: 18,
        overflow: "hidden",
        border: "1px solid rgba(190,243,226,0.28)",
        backgroundColor: COLORS.white,
        boxShadow: "0 35px 90px rgba(0,0,0,0.32)",
        opacity: enter,
        scale: 0.95 + enter * 0.05,
        translate: `${(1 - enter) * 70 + shiftX * zoomProgress}px ${(1 - enter) * 90}px`,
      }}
    >
      <div
        style={{
          height: 44,
          display: "flex",
          alignItems: "center",
          gap: 10,
          padding: "0 18px",
          backgroundColor: "#101715",
          borderBottom: "1px solid rgba(255,255,255,0.08)",
        }}
      >
        {[COLORS.red, COLORS.amber, COLORS.lime].map((color) => (
          <div
            key={color}
            style={{
              width: 11,
              height: 11,
              borderRadius: "50%",
              backgroundColor: color,
            }}
          />
        ))}
        <div
          style={{
            marginLeft: 16,
            color: "rgba(255,255,255,0.48)",
            fontSize: 17,
            letterSpacing: 0.3,
          }}
        >
          goodman.local / live walkthrough
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
