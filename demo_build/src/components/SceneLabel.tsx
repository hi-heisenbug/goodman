import { COLORS, FONTS } from "../theme";

type SceneLabelProps = {
  readonly children: React.ReactNode;
  readonly light?: boolean;
};

export const SceneLabel: React.FC<SceneLabelProps> = ({
  children,
  light = true,
}) => (
  <div
    style={{
      color: light ? COLORS.lime : COLORS.green,
      fontFamily: FONTS.body,
      fontSize: 22,
      fontWeight: 700,
      letterSpacing: 4,
      textTransform: "uppercase",
    }}
  >
    {children}
  </div>
);
