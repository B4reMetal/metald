// Where the listener is in which track, timed against the audio itself. The playout (the radio's
// contrib/radio/radio.liq, the station's contrib/station/edge/edge.liq) writes stamps.json beside the
// HLS playlist: as each segment ends, and
// wherever a track starts, the track id and how far into the track that point is, as an offset from
// the end of the last finished segment. hls.js says where each segment sits on the audio element's
// timeline, so the position needs no clock, no latency estimate and no server time.

/** One entry of stamps.json. */
export interface Mark { after: string; offset: number; id: string; pos: number }

/** A mark placed on the audio element's timeline. */
export interface Stamp { at: number; id: string; pos: number }

/** The marks whose segment the player has buffered, placed on its timeline. */
export function placeMarks(marks: Mark[], segmentEnds: Map<string, number>): Stamp[] {
  const out: Stamp[] = [];
  for (const m of marks) {
    const end = segmentEnds.get(m.after);
    if (end !== undefined) out.push({ at: end + m.offset, id: m.id, pos: m.pos });
  }
  return out;
}

/** The track playing at media time t and how far into it, from the latest stamp at or before t. */
export function positionAt(stamps: Stamp[], t: number): { id: string; position: number } | null {
  let best: Stamp | null = null;
  for (const s of stamps) if (s.at <= t + 1e-3 && (!best || s.at >= best.at)) best = s;
  return best ? { id: best.id, position: best.pos + (t - best.at) } : null;
}

/** The track to show at media time t: the one playing, except while a song fades in over the last one,
 *  when the last one stays until the fade is over (its closing words with it). No fade into or out of
 *  a DJ clip, which plays whole. */
export function shownAt(stamps: Stamp[], t: number, fade: number, isDj: (id: string) => boolean): { id: string; position: number } | null {
  const now = positionAt(stamps, t);
  if (!now || now.position >= fade || isDj(now.id)) return now;
  // Just before the new track's start (positionAt allows 1 ms of slack, so step back further).
  const step = 0.01, before = positionAt(stamps, t - now.position - step);
  if (!before || before.id === now.id || isDj(before.id)) return now;
  return { id: before.id, position: before.position + now.position + step };
}

/** Band levels (0..1) from an analyser's dB spectrum: n bands spaced evenly in pitch from lo to hi Hz,
 *  each the loudest bin in it, over a 60 dB range. */
export function bandsFrom(db: Float32Array, sampleRate: number, n = 12, lo = 40, hi = 16000): number[] {
  const binHz = sampleRate / 2 / db.length, out: number[] = [];
  for (let b = 0; b < n; b++) {
    const f0 = lo * (hi / lo) ** (b / n), f1 = lo * (hi / lo) ** ((b + 1) / n);
    const i0 = Math.floor(f0 / binHz), i1 = Math.max(i0 + 1, Math.ceil(f1 / binHz));
    let peak = -Infinity;
    for (let i = i0; i < i1 && i < db.length; i++) peak = Math.max(peak, db[i]);
    out.push(Math.max(0, Math.min(1, (peak + 75) / 60)));
  }
  return out;
}
