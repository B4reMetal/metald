import { describe, expect, it } from "vitest";
import { adtsFromTs } from "./adts";

/** One 188-byte TS packet: header, optional adaptation field of `adapt` bytes, payload, 0xff padding. */
function packet(pid: number, start: boolean, payload: number[], adapt = 0): number[] {
  const control = adapt ? 3 : 1;
  const head = [0x47, (start ? 0x40 : 0) | (pid >> 8), pid & 0xff, (control << 4)];
  const af = adapt ? [adapt - 1, ...new Array(adapt - 1).fill(0xff)] : [];
  const body = [...head, ...af, ...payload];
  return [...body, ...new Array(188 - body.length).fill(0xff)].slice(0, 188);
}

const pes = (data: number[]) => [0, 0, 1, 0xc0, 0, 0, 0x80, 0x80, 5, 1, 2, 3, 4, 5, ...data];

describe("adtsFromTs", () => {
  it("joins the audio stream's payload without PES headers", () => {
    const other = packet(0x1000, true, [0, 0, 1, 0xe0, 9, 9]); // a non-audio stream
    // The payload fills each packet: an adaptation field pads the short last one, as muxers do.
    const first = packet(0x100, true, pes(new Array(170).fill(0xaa)));
    const rest = packet(0x100, false, new Array(150).fill(0xbb), 34);
    const ts = new Uint8Array([...other, ...first, ...rest]);
    const out = adtsFromTs(ts);
    expect(out.length).toBe(170 + 150);
    expect(out[0]).toBe(0xaa);
    expect(out[169]).toBe(0xaa);
    expect(out[170]).toBe(0xbb);
    expect(out[out.length - 1]).toBe(0xbb);
  });

  it("is empty for a segment without audio", () => {
    expect(adtsFromTs(new Uint8Array(packet(0x1000, true, [0, 0, 1, 0xe0])))).toHaveLength(0);
  });
});
