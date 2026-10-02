// The AAC audio (ADTS frames) inside an MPEG-TS segment, so the browser can decode it on its own: iOS
// gives Web Audio silence for a Media Source element, so there the visualizers listen to a muted copy
// decoded from the same segments (player.svelte.ts).

const PACKET = 188;

/** The payload of the segment's audio stream, PES headers removed. Empty if it has none. */
export function adtsFromTs(ts: Uint8Array): Uint8Array {
  const chunks: Uint8Array[] = [];
  let audioPid = -1, total = 0;
  for (let o = 0; o + PACKET <= ts.length; o += PACKET) {
    if (ts[o] !== 0x47) continue;
    const start = (ts[o + 1] & 0x40) !== 0, pid = ((ts[o + 1] & 0x1f) << 8) | ts[o + 2];
    const control = (ts[o + 3] >> 4) & 3;
    if (!(control & 1)) continue; // no payload
    let p = o + 4 + (control === 3 ? 1 + ts[o + 4] : 0);
    if (start && audioPid === -1 && isAudioPes(ts, p)) audioPid = pid;
    if (pid !== audioPid) continue;
    if (start) p += 9 + ts[p + 8]; // PES header
    if (p < o + PACKET) {
      chunks.push(ts.subarray(p, o + PACKET));
      total += o + PACKET - p;
    }
  }
  const out = new Uint8Array(total);
  let at = 0;
  for (const c of chunks) { out.set(c, at); at += c.length; }
  return out;
}

/** A PES packet start for an MPEG audio stream (stream ids 0xC0-0xDF). */
function isAudioPes(b: Uint8Array, p: number): boolean {
  return b[p] === 0 && b[p + 1] === 0 && b[p + 2] === 1 && (b[p + 3] & 0xe0) === 0xc0;
}
