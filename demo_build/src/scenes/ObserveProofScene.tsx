import { AbsoluteFill, useCurrentFrame, useVideoConfig } from "remotion";
import proof from "../../evidence/observe_proof.json";
import { Backdrop } from "../components/Backdrop";
import { BorderBeam } from "../components/BorderBeam";
import { BrandMark } from "../components/BrandMark";
import { KineticHeadline } from "../components/KineticHeadline";
import { SceneLabel } from "../components/SceneLabel";
import { TerminalCard, type TerminalLine } from "../components/TerminalCard";
import { progress, springIn, verdictPop } from "../motion";
import { COLORS, FONTS, SAFE_X, TNUM } from "../theme";

const proofLines: readonly TerminalLine[] = [
  {
    text: "bash scripts/setup-everything.sh observe --pid <PID>",
    at: 24,
    kind: "command",
  },
  {
    text: "target pid=<PID>  comm=MainThread  runtime=node",
    at: 66,
    kind: "output",
    typed: false,
  },
  {
    text: "Tier-1 attribution ready · host kernel",
    at: 76,
    kind: "output",
    typed: false,
  },
  {
    text: `workload | ${proof.package}@${proof.version} | ${proof.behavior}`,
    at: 92,
    kind: "success",
    typed: false,
  },
  {
    text: `${proof.events} events · ${proof.exact_dependency_events} exact dependency events`,
    at: 112,
    kind: "output",
    typed: false,
  },
  { text: proof.pass, at: 132, kind: "success", typed: false },
];

const Badge: React.FC<{ readonly children: React.ReactNode }> = ({ children }) => (
  <div
    style={{
      padding: "12px 16px",
      borderRadius: 9,
      border: `1px solid ${COLORS.line}`,
      backgroundColor: "rgba(255,255,255,0.035)",
      color: COLORS.muted,
      fontFamily: FONTS.mono,
      fontSize: 17,
      letterSpacing: 1,
    }}
  >
    {children}
  </div>
);

export const ObserveProofScene: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const terminal = springIn(frame, fps, 18);
  const badge = progress(frame, 42, 18);
  const pass = verdictPop(frame, fps, 132);

  return (
    <AbsoluteFill>
      <Backdrop accent={COLORS.green} glowX="72%" glowY="50%" glowOpacity={0.15} />
      <div style={{ position: "absolute", left: SAFE_X, top: 62 }}>
        <BrandMark compact />
      </div>

      <div style={{ position: "absolute", left: SAFE_X, top: 178, width: 620 }}>
        <SceneLabel>Run it on your workload</SceneLabel>
        <div style={{ marginTop: 18 }}>
          <KineticHeadline
            text="Your app. Your traffic. One exact answer."
            frame={frame}
            startAt={4}
            fontSize={72}
            maxWidth={620}
            accentWords={["Your", "exact"]}
            accentColor={COLORS.lime}
          />
        </div>
        <div
          style={{
            marginTop: 34,
            color: COLORS.muted,
            fontFamily: FONTS.body,
            fontSize: 25,
            lineHeight: 1.5,
            opacity: badge,
          }}
        >
          Goodman selects a running Node or Python process, traces real syscalls,
          and fails unless a versioned dependency is proven.
        </div>
        <div
          style={{
            marginTop: 38,
            display: "flex",
            flexWrap: "wrap",
            gap: 12,
            opacity: badge,
          }}
        >
          <Badge>HOST PROCESS</Badge>
          <Badge>HOST KERNEL</Badge>
          <Badge>NO MOCK EVENTS</Badge>
        </div>
      </div>

      <div
        style={{
          position: "absolute",
          right: SAFE_X,
          top: 218,
          opacity: Math.min(1, terminal * 1.25),
          translate: `${(1 - terminal) * 60}px 0px`,
        }}
      >
        <TerminalCard
          lines={proofLines}
          frame={frame}
          width={1040}
          title="goodman observe — verified host-kernel run"
          fontSize={23}
          minHeight={440}
        />
      </div>

      <div
        style={{
          position: "absolute",
          left: 760,
          right: SAFE_X,
          bottom: 90,
          height: 82,
          borderRadius: 14,
          border: "1px solid rgba(147,203,82,0.35)",
          backgroundColor: "rgba(13,17,23,0.96)",
          boxShadow: `0 0 90px -28px ${COLORS.lime}`,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          padding: "0 28px",
          opacity: Math.min(1, pass * 1.2),
          scale: 0.97 + pass * 0.03,
        }}
      >
        <BorderBeam color={COLORS.lime} borderRadius={14} />
        <span
          style={{
            color: COLORS.lime,
            fontFamily: FONTS.mono,
            fontSize: 18,
            fontWeight: 700,
            letterSpacing: 3,
          }}
        >
          VERIFIED LIVE
        </span>
        <span
          style={{
            color: COLORS.white,
            fontFamily: FONTS.heading,
            fontSize: 32,
            fontWeight: 700,
            ...TNUM,
          }}
        >
          {proof.events} / {proof.events} events attributed to {proof.package}@{proof.version}
        </span>
      </div>
    </AbsoluteFill>
  );
};
