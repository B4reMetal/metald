import { describe, expect, it } from "vitest";
import { lineAt, settle } from "./sync";

describe("lineAt", () => {
  const lines: [number, string][] = [[1.5, "[Verse]"], [1.5, "one"], [4, "two"]];
  it("is -1 before the first line or without a position", () => {
    expect(lineAt(lines, 1)).toBe(-1);
    expect(lineAt(lines, null)).toBe(-1);
  });
  it("is the last line whose time has come", () => {
    expect(lineAt(lines, 2)).toBe(1);
    expect(lineAt(lines, 99)).toBe(2);
  });
});

it("settle rises at once and falls slowly", () => {
  expect(settle(0.2, 0.9)).toBe(0.9);
  expect(settle(0.9, 0)).toBeCloseTo(0.828);
});
