// Per-browser choices, remembered between visits when the browser allows it.

function load(key: string, fallback: string): string {
  try { return localStorage.getItem(key) ?? fallback; } catch { return fallback; }
}
function save(key: string, value: string) {
  try { localStorage.setItem(key, value); } catch { /* private mode: lasts for this visit */ }
}

class Prefs {
  theme = $state(load("theme", "ember"));
  visualizer = $state(load("visualizer", "bars"));
  /** Seconds the lyrics are shifted by, for a listener whose stream runs early or late. */
  nudge = $state(parseFloat(load("lyricNudge", "0")) || 0);
  volume = $state(Math.min(1, Math.max(0, parseFloat(load("volume", "1")) || 0)));

  setTheme(id: string) { this.theme = id; save("theme", id); }
  setVisualizer(id: string) { this.visualizer = id; save("visualizer", id); }
  setNudge(v: number) { this.nudge = Math.round(v * 2) / 2; save("lyricNudge", String(this.nudge)); }
  setVolume(v: number) { this.volume = v; save("volume", String(v)); }
}

export const prefs = new Prefs();
