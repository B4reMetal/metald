/** iPhone, iPad and iPod: page volume is ignored there (the hardware buttons own it). Recent iOS even
 *  reports a changed volume back without applying it, so this goes by the device, not by trying. iPads
 *  present themselves as Macs, but only they have touch screens. */
export function isAppleMobile(ua: string, platform: string, maxTouchPoints: number): boolean {
  return /iPad|iPhone|iPod/.test(ua) || (platform === "MacIntel" && maxTouchPoints > 1);
}
