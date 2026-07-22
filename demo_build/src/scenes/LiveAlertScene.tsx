import { AbsoluteFill, interpolate, useCurrentFrame, useVideoConfig } from "remotion";
import { Backdrop } from "../components/Backdrop";
import { BorderBeam } from "../components/BorderBeam";
import { BrandMark } from "../components/BrandMark";
import { KineticHeadline } from "../components/KineticHeadline";
import { SceneLabel } from "../components/SceneLabel";
import { WalkthroughFrame } from "../components/WalkthroughFrame";
import { fadeWindow, progress, springIn, verdictPop } from "../motion";
import { COLORS, FONTS, SAFE_X } from "../theme";

type LiveAlertSceneProps = {
  readonly playbackRate?: number;
};

type ProofCardProps = {
  readonly eyebrow: string;
  readonly packageName: string;
  readonly version: string;
  readonly chips: readonly string[];
  readonly color: string;
  readonly opacity: number;
  readonly scale: number;
  readonly copied?: boolean;
};

const ProofCard: React.FC<ProofCardProps> = ({
  eyebrow,
  packageName,
  version,
  chips,
  color,
  opacity,
  scale,
  copied = false,
}) => (
  <div
    style={{
      width: 470,
      padding: 32,
      borderRadius: 16,
      border: `1px solid ${color}66`,
      backgroundColor: "rgba(13,17,23,0.97)",
      boxShadow: "0 40px 110px rgba(0,0,0,0.72)",
      color: COLORS.white,
      opacity,
      scale,
    }}
  >
    <BorderBeam color={color} borderRadius={16} />
    <div
      style={{
        color,
        fontFamily: FONTS.mono,
        fontSize: 17,
        fontWeight: 700,
        letterSpacing: 3,
      }}
    >
      {eyebrow}
    </div>
    <div
      style={{
        marginTop: 18,
        fontFamily: FONTS.heading,
        fontWeight: 700,
        fontSize: 36,
        lineHeight: 1.06,
        overflowWrap: "anywhere",
      }}
    >
      {packageName}
    </div>
    <div
      style={{
        marginTop: 12,
        color: COLORS.muted,
        fontFamily: FONTS.mono,
        fontSize: 21,
      }}
    >
      {version}
    </div>
    <div style={{ marginTop: 24, display: "flex", flexWrap: "wrap", gap: 9 }}>
      {chips.map((chip) => (
        <span
          key={chip}
          style={{
            padding: "8px 11px",
            borderRadius: 7,
            backgroundColor: `${color}18`,
            border: `1px solid ${color}44`,
            color,
            fontFamily: FONTS.mono,
            fontSize: 14,
            fontWeight: 700,
          }}
        >
          {chip}
        </span>
      ))}
    </div>
    <div
      style={{
        marginTop: 24,
        color: copied ? COLORS.lime : COLORS.faint,
        fontFamily: FONTS.mono,
        fontSize: 16,
        fontWeight: 700,
        letterSpacing: 1,
      }}
    >
      {copied ? "✓ ROLLBACK COMMAND COPIED" : "LIVE COLLECTOR EVIDENCE"}
    </div>
  </div>
);

export const LiveAlertScene: React.FC<LiveAlertSceneProps> = ({
  playbackRate = 1,
}) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const openClawFrame = Math.round((1.25 / playbackRate) * fps);
  const openClawCopyFrame = Math.round((2.25 / playbackRate) * fps);
  const attackFrame = Math.round((6 / playbackRate) * fps);
  const attackCopyFrame = Math.round((7.15 / playbackRate) * fps);
  const title = progress(frame, 4, 18);
  const openClaw = springIn(frame, fps, openClawFrame);
  const openClawWindow = fadeWindow(
    frame,
    openClawFrame,
    openClawFrame + 8,
    attackFrame - 28,
    attackFrame - 12,
  );
  const attack = verdictPop(frame, fps, attackFrame + 3);
  const flash = interpolate(
    frame,
    [attackFrame, attackFrame + 2, attackFrame + 8],
    [0, 0.14, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
  );

  return (
    <AbsoluteFill>
      <Backdrop accent={COLORS.red} glowX="72%" glowOpacity={0.1} />
      <div style={{ position: "absolute", left: SAFE_X, top: 62, opacity: title }}>
        <BrandMark compact />
      </div>
      <div style={{ position: "absolute", left: SAFE_X, top: 126, opacity: title }}>
        <SceneLabel color={COLORS.red}>Scripted live product · real clicks</SceneLabel>
        <div style={{ marginTop: 12 }}>
          <KineticHeadline
            text="An AI skill drifts. Then a package attacks live."
            frame={frame}
            startAt={8}
            fontSize={60}
            maxWidth={1480}
            accentWords={["skill", "live"]}
            accentColor={COLORS.red}
          />
        </div>
      </div>

      <div style={{ position: "absolute", left: SAFE_X - 10, top: 284 }}>
        <WalkthroughFrame
          segment="alerts"
          frame={frame}
          width={1310}
          enterAt={12}
          playbackRate={playbackRate}
          zoomAt={openClawFrame - 4}
          zoom={1.13}
          focus="56% 62%"
          shiftX={-60}
          glow={COLORS.red}
        />
      </div>

      <div style={{ position: "absolute", right: SAFE_X - 18, top: 346 }}>
        <ProofCard
          eyebrow="OPENCLAW SKILL DRIFT"
          packageName="@goodman-demo/calendar-sync"
          version="1.2.2 → 1.2.3"
          chips={["SECRET READ", "NEW OUTBOUND"]}
          color={COLORS.amber}
          opacity={openClawWindow}
          scale={0.96 + openClaw * 0.04}
          copied={frame >= openClawCopyFrame + 8}
        />
      </div>

      <div style={{ position: "absolute", right: SAFE_X - 18, top: 346 }}>
        <ProofCard
          eyebrow="MINI-SHAI-HULUD · ARRIVED LIVE"
          packageName="mini-shai-hulud-loader"
          version="1.0.0 → 1.0.1"
          chips={[".NPMRC", "CLOUD METADATA", "C2", "SHELL"]}
          color={COLORS.red}
          opacity={Math.min(1, attack * 1.2)}
          scale={0.94 + attack * 0.06}
          copied={frame >= attackCopyFrame + 8}
        />
      </div>

      <div
        style={{
          position: "absolute",
          right: SAFE_X,
          bottom: 42,
          color: COLORS.faint,
          fontFamily: FONTS.mono,
          fontSize: 15,
          letterSpacing: 1,
        }}
      >
        API truth + DOM state verified after every click
      </div>

      <AbsoluteFill
        style={{ backgroundColor: COLORS.red, opacity: flash, pointerEvents: "none" }}
      />
    </AbsoluteFill>
  );
};
