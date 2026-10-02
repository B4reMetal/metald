// Types for modules that ship none: butterchurn (Milkdrop in WebGL), of which the station page uses little,
// and hls.js's smaller build, which is hls.js without subtitles, DRM and alternate audio.
declare module "hls.js/light" {
  export * from "hls.js";
  export { default } from "hls.js";
}

declare module "butterchurn" {
  export interface MilkdropVisualizer {
    connectAudio(node: AudioNode): void;
    loadPreset(preset: object, blendSeconds: number): void;
    setRendererSize(width: number, height: number): void;
    render(): void;
  }
  const butterchurn: {
    createVisualizer(ctx: BaseAudioContext, canvas: HTMLCanvasElement, opts: { width: number; height: number; pixelRatio?: number }): MilkdropVisualizer;
  };
  export default butterchurn;
}

declare module "butterchurn-presets" {
  const presets: { getPresets(): Record<string, object> };
  export default presets;
}
