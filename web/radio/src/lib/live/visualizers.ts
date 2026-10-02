// The visualizers of the radio and station pages. All of them listen to the audio in the browser: the
// radio's own canvas styles (fed from an analyser), audioMotion-analyzer's spectrum views, and Milkdrop
// presets through butterchurn (WebGL, loaded only when picked).

import type { ConstructorOptions } from "audiomotion-analyzer";
import { VISUALIZERS } from "../visualizers";

export type LiveViz =
  | { id: string; label: string; kind: "canvas" }
  | { id: string; label: string; kind: "spectrum"; options: ConstructorOptions }
  | { id: string; label: string; kind: "milkdrop" }
  | { id: string; label: string; kind: "off" };

const spectrum = (id: string, label: string, options: ConstructorOptions): LiveViz => ({ id, label, kind: "spectrum", options });

export const LIVE_VISUALIZERS: LiveViz[] = [
  // Winamp's own: a couple of dozen bars with falling peaks.
  spectrum("winamp", "Winamp", { mode: 6, barSpace: 0.35, showPeaks: true, peakFadeTime: 0, peakHoldTime: 300, gravity: 2.8, maxFreq: 14000 }),
  spectrum("spectrum", "Spectrum", { mode: 3, reflexRatio: 0.25, reflexAlpha: 0.2, barSpace: 0.25, lumiBars: false }),
  spectrum("octaves", "Octave LEDs", { mode: 6, ledBars: true, barSpace: 0.3 }),
  spectrum("halo", "Halo", { mode: 4, radial: true, spinSpeed: 1, barSpace: 0.1 }),
  spectrum("stereo", "Stereo", { mode: 5, channelLayout: "dual-horizontal", barSpace: 0.2, mirror: 0 }),
  spectrum("line", "Line", { mode: 10, lineWidth: 2, fillAlpha: 0.25 }),
  { id: "milkdrop", label: "Milkdrop", kind: "milkdrop" },
  ...VISUALIZERS.map((v): LiveViz => ({ id: v.id, label: v.label, kind: "canvas" })),
  { id: "off", label: "Off", kind: "off" },
];

export function liveVizById(id: string): LiveViz {
  return LIVE_VISUALIZERS.find((v) => v.id === id) ?? LIVE_VISUALIZERS[0];
}
