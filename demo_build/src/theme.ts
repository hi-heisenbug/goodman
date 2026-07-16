export const COLORS = {
  // Canvas and surfaces: cool near-blacks, never pure #000 except letterbox.
  ink: "#0a0a0c",
  surface: "#0d1117",
  surfaceRaised: "#11161d",
  line: "rgba(255,255,255,0.08)",
  // Text.
  white: "#ededed",
  muted: "#8b8f98",
  faint: "rgba(255,255,255,0.42)",
  // Goodman brand greens (matches the live dashboard).
  lime: "#93cb52",
  green: "#1c9770",
  mint: "#bef3e2",
  // Semantic threat colors.
  red: "#ff5060",
  amber: "#fbbf24",
} as const;

export const FONTS = {
  heading: '"DM Sans", sans-serif',
  body: '"Inter", sans-serif',
  mono: '"JetBrains Mono", "DejaVu Sans Mono", monospace',
} as const;

export const TNUM = { fontFeatureSettings: '"tnum"' } as const;

export const SAFE_X = 110;
