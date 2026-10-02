// Lyric and meter timing helpers. Pure functions, so they can be tested apart from the page; where the
// listener is comes from the stream itself (lib/live/stamps.ts).

import type { TimedLine } from "./types";

/** Songs crossfade over this long (contrib/radio/radio.liq); DJ interludes don't. */
export const FADE = 3;

/** The line being sung at this position: the last whose time has come, or -1 before the first. */
export function lineAt(lines: TimedLine[], position: number | null): number {
  if (position === null) return -1;
  let idx = -1;
  for (let i = 0; i < lines.length && lines[i][0] <= position; i++) idx = i;
  return idx;
}

/** Meter ballistics: rise at once, fall slowly. */
export function settle(shown: number, target: number): number {
  return target > shown ? target : shown * 0.92 + target * 0.08;
}
