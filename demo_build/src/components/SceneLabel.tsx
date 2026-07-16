import { COLORS, FONTS } from "../theme";

type SceneLabelProps = {
  readonly children: React.ReactNode;
  readonly color?: string;
};

export const SceneLabel: React.FC<SceneLabelProps> = ({
  children,
  color = COLORS.lime,
}) => (
  <div
    style={{
      display: "inline-flex",
      alignItems: "center",
      gap: 12,
      color,
      fontFamily: FONTS.mono,
      fontSize: 20,
      fontWeight: 700,
      letterSpacing: 4.5,
      textTransform: "uppercase",
    }}
  >
    <span
      style={{
        width: 26,
        height: 2,
        backgroundColor: color,
        display: "inline-block",
      }}
    />
    {children}
  </div>
);
