import { describe, expect, it } from "vitest";
import { bandsFrom, placeMarks, positionAt, shownAt } from "./stamps";

describe("placeMarks", () => {
  it("puts each mark at its segment's end plus the offset", () => {
    const ends = new Map([["aac_5.ts", 10], ["aac_6.ts", 12]]);
    const marks = [
      { after: "aac_4.ts", offset: 0, id: "7", pos: 50 },
      { after: "aac_5.ts", offset: 0, id: "7", pos: 52 },
      { after: "aac_5.ts", offset: 1.25, id: "8", pos: 0 },
      { after: "aac_6.ts", offset: 0, id: "8", pos: 0.75 },
    ];
    expect(placeMarks(marks, ends)).toEqual([
      { at: 10, id: "7", pos: 52 },
      { at: 11.25, id: "8", pos: 0 },
      { at: 12, id: "8", pos: 0.75 },
    ]);
  });
});

describe("positionAt", () => {
  const stamps = [
    { at: 100, id: "7", pos: 180 },
    { at: 102, id: "7", pos: 182.01 },
    { at: 103.5, id: "8", pos: 0 },
    { at: 105.5, id: "8", pos: 2.0 },
  ];
  it("counts on from the last stamp", () => {
    expect(positionAt(stamps, 101)).toEqual({ id: "7", position: 181 });
    expect(positionAt(stamps, 103)?.position).toBeCloseTo(183.01);
  });
  it("switches tracks exactly where the new one starts", () => {
    expect(positionAt(stamps, 103.49)?.id).toBe("7");
    expect(positionAt(stamps, 103.5)).toEqual({ id: "8", position: 0 });
  });
  it("knows nothing before the first stamp", () => {
    expect(positionAt(stamps, 99)).toBeNull();
  });
  it("doesn't depend on cue order", () => {
    expect(positionAt([...stamps].reverse(), 106)).toEqual({ id: "8", position: 2.5 });
  });
});

describe("shownAt", () => {
  // Song 7 until 103.5, then song 8 fading in over it; dj9 is a DJ clip at 200.
  const stamps = [
    { at: 100, id: "7", pos: 180 },
    { at: 103.5, id: "8", pos: 0 },
    { at: 105.5, id: "8", pos: 2 },
    { at: 200, id: "dj9", pos: 0 },
  ];
  const dj = (id: string) => id.startsWith("dj");
  it("keeps the last song through the fade", () => {
    const shown = shownAt(stamps, 105, 3, dj);
    expect(shown?.id).toBe("7");
    expect(shown?.position).toBeCloseTo(185);
  });
  it("shows the new song once the fade is over", () => {
    expect(shownAt(stamps, 106.5, 3, dj)).toEqual({ id: "8", position: 3 });
  });
  it("switches to a DJ clip at once", () => {
    expect(shownAt(stamps, 200.5, 3, dj)).toEqual({ id: "dj9", position: 0.5 });
  });
});

describe("bandsFrom", () => {
  it("maps loud bins high and silence to zero", () => {
    const db = new Float32Array(1024).fill(-120);
    db[2] = -15; // ~43 Hz at 44.1 kHz: the lowest band
    const b = bandsFrom(db, 44100);
    expect(b).toHaveLength(12);
    expect(b[0]).toBe(1);
    expect(b.slice(1).every((v) => v === 0)).toBe(true);
  });
});
