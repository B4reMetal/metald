// Each theme is a set of colours, which the page and the visualizer read as CSS variables, and optionally
// a look: extra styling in app.css under [data-look="..."], for themes that are more than colours.

export interface Palette {
  bg: string; fg: string; muted: string; dim: string; card: string; line: string;
  /** Main colour: buttons, highlights, the visualizer. */
  accent: string;
  /** Second colour, for visualizers that blend two. */
  accent2: string;
}

export interface Theme {
  id: string; label: string; light: Palette; dark?: Palette;
  look?: string;
  /** Spectrum colours, top (loudest) first; otherwise accent2 over accent. */
  spectrum?: string[];
}

const ember: Theme = {
  id: "ember", label: "Ember",
  light: { bg: "#faf8f5", fg: "#1d1b19", muted: "#6f6a64", dim: "#a8a29b", card: "#ffffff", line: "#e6e1da", accent: "#c2410c", accent2: "#eab308" },
  dark: { bg: "#141312", fg: "#ece8e3", muted: "#9c958d", dim: "#5e5953", card: "#1d1c1a", line: "#2e2c29", accent: "#fb923c", accent2: "#facc15" },
};

export const THEMES: Theme[] = [
  ember,
  { id: "midnight", label: "Midnight", light: { bg: "#0b1020", fg: "#e6ecff", muted: "#8b95b8", dim: "#4a5478", card: "#121a33", line: "#1f2a4d", accent: "#38bdf8", accent2: "#818cf8" } },
  { id: "synthwave", label: "Synthwave", light: { bg: "#170b2b", fg: "#fbe7ff", muted: "#b48fd1", dim: "#6b4a8c", card: "#221040", line: "#3a1d63", accent: "#ff3cac", accent2: "#2bd2ff" } },
  { id: "terminal", label: "Terminal", light: { bg: "#050805", fg: "#c9f7c9", muted: "#5fae5f", dim: "#2f5e2f", card: "#0a110a", line: "#163016", accent: "#39ff14", accent2: "#a3ff8f" } },
  { id: "paper", label: "Paper", light: { bg: "#fbfbf8", fg: "#111111", muted: "#666666", dim: "#aaaaaa", card: "#ffffff", line: "#e3e3df", accent: "#111111", accent2: "#888888" } },
  // Winamp 2's classic skin: a bevelled grey case, a black display, its playlist's green text with the
  // playing line in white, and the green-yellow-red spectrum.
  { id: "winamp", label: "Winamp", look: "winamp", spectrum: ["#ef3110", "#ceb710", "#a5d608", "#18a518", "#00d610"],
    light: { bg: "#1c1c26", fg: "#ffffff", muted: "#9c9cb4", dim: "#00e000", card: "#000000", line: "#3c3c52", accent: "#00e000", accent2: "#d6b508" } },
  { id: "spam", label: "Spam", light: { bg: "#fff4f2", fg: "#2b1412", muted: "#8a5a55", dim: "#c9a19c", card: "#ffffff", line: "#f4d6d1", accent: "#e8577a", accent2: "#c2410c" },
    dark: { bg: "#1c1012", fg: "#ffe6ea", muted: "#c08a93", dim: "#6e4249", card: "#26161a", line: "#3b2227", accent: "#ff7a9c", accent2: "#fb923c" } },
];

export function themeById(id: string): Theme {
  return THEMES.find((t) => t.id === id) ?? ember;
}

/** The palette in force: a theme with a dark variant follows the system's light or dark setting. */
export function palette(theme: Theme, prefersDark: boolean): Palette {
  return prefersDark && theme.dark ? theme.dark : theme.light;
}

export function applyPalette(p: Palette, root: HTMLElement = document.documentElement) {
  for (const [k, v] of Object.entries(p)) root.style.setProperty(`--${k}`, v);
  root.style.colorScheme = isDark(p.bg) ? "dark" : "light";
}

function isDark(hex: string): boolean {
  const n = parseInt(hex.slice(1), 16);
  return ((n >> 16) * 299 + ((n >> 8) & 255) * 587 + (n & 255) * 114) / 1000 < 128;
}
