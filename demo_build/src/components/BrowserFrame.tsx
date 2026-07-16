import { Img, staticFile } from "remotion";
import { progress } from "../motion";
import { COLORS } from "../theme";

type BrowserFrameProps = {
  readonly screenshot: string;
  readonly frame: number;
  readonly width?: number;
  readonly enterAt?: number;
  readonly zoomAt?: number;
  readonly zoom?: number;
  readonly focus?: string;
  readonly shiftX?: number;
};

export const BrowserFrame: React.FC<BrowserFrameProps> = ({
  screenshot,
  frame,
  width = 1420,
  enterAt = 10,
  zoomAt = 90,
  zoom = 1.08,
  focus = "50% 50%",
  shiftX = 0,
}) => {
  const enter = progress(frame, enterAt, 26);
  const zoomProgress = progress(frame, zoomAt, 48);
  const imageHeight = width * (9 / 16);

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
            style={{ width: 11, height: 11, borderRadius: "50%", backgroundColor: color }}
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
          goodman.local / runtime evidence
        </div>
      </div>
      <div style={{ width, height: imageHeight, overflow: "hidden" }}>
        <Img
          src={staticFile(`screenshots/${screenshot}`)}
          style={{
            width: "100%",
            height: "100%",
            objectFit: "cover",
            scale: 1 + (zoom - 1) * zoomProgress,
            transformOrigin: focus,
          }}
        />
      </div>
    </div>
  );
};
