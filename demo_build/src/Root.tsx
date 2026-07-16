import { Composition } from "remotion";
import { GoodmanDemo } from "./GoodmanDemo";
import { FPS, TOTAL_FRAMES, TOTAL_FRAMES_X } from "./storyboard";
import "./index.css";

export const RemotionRoot: React.FC = () => (
  <>
    <Composition
      id="GoodmanDemo"
      component={GoodmanDemo}
      durationInFrames={TOTAL_FRAMES}
      fps={FPS}
      width={1920}
      height={1080}
      defaultProps={{ cut: "master" as const }}
    />
    <Composition
      id="GoodmanDemoX"
      component={GoodmanDemo}
      durationInFrames={TOTAL_FRAMES_X}
      fps={FPS}
      width={1920}
      height={1080}
      defaultProps={{ cut: "x" as const }}
    />
  </>
);
