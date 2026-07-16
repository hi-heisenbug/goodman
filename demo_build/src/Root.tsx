import { Composition } from "remotion";
import { GoodmanDemo } from "./GoodmanDemo";
import { FPS, TOTAL_FRAMES } from "./storyboard";
import "./index.css";

export const RemotionRoot: React.FC = () => (
  <Composition
    id="GoodmanDemo"
    component={GoodmanDemo}
    durationInFrames={TOTAL_FRAMES}
    fps={FPS}
    width={1920}
    height={1080}
  />
);
