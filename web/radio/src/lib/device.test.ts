import { expect, it } from "vitest";
import { isAppleMobile } from "./device";

it("knows iPhones, and iPads that claim to be Macs", () => {
  expect(isAppleMobile("Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)", "iPhone", 5)).toBe(true);
  expect(isAppleMobile("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Safari", "MacIntel", 5)).toBe(true);
});

it("leaves Macs and Android alone", () => {
  expect(isAppleMobile("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Safari", "MacIntel", 0)).toBe(false);
  expect(isAppleMobile("Mozilla/5.0 (Linux; Android 15; Pixel 9)", "Linux armv8l", 5)).toBe(false);
});
