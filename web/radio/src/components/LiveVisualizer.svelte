<script lang="ts">
  import AudioMotionAnalyzer from "audiomotion-analyzer";
  import { appearance } from "../lib/appearance.svelte";
  import type { Palette } from "../lib/themes";
  import { settle } from "../lib/sync";
  import { visualizerById } from "../lib/visualizers";
  import { bandsFrom } from "../lib/live/stamps";
  import { liveVizById, type LiveViz } from "../lib/live/visualizers";

  // Draws whichever visualizer is picked from the player's analysis tap. Each kind owns the stage while
  // it's showing and tears down when the choice (or the tap) changes.
  let { source, vizId, trackId }: { source: AudioNode | null; vizId: string; trackId: string | undefined } = $props();
  let stage: HTMLDivElement | undefined = $state();
  let viz: LiveViz = $derived(liveVizById(vizId));
  let presetName = $state("");
  let nextPreset: (() => void) | null = $state(null);

  function themeGradient(p: Palette, stops: string[]) {
    return { bgColor: p.card, colorStops: stops };
  }

  function canvas(src: AudioNode, el: HTMLDivElement, id: string) {
    const c = document.createElement("canvas");
    el.append(c);
    const g = c.getContext("2d")!;
    const an = src.context.createAnalyser();
    an.fftSize = 4096;
    an.smoothingTimeConstant = 0.6;
    src.connect(an);
    const db = new Float32Array(an.frequencyBinCount), shown = new Array(12).fill(0);
    let raf = 0;
    const draw = (ms: number) => {
      raf = requestAnimationFrame(draw);
      const dpr = devicePixelRatio || 1, w = c.clientWidth, h = c.clientHeight;
      if (c.width !== w * dpr || c.height !== h * dpr) { c.width = w * dpr; c.height = h * dpr; }
      g.setTransform(dpr, 0, 0, dpr, 0, 0);
      g.clearRect(0, 0, w, h);
      an.getFloatFrequencyData(db);
      bandsFrom(db, src.context.sampleRate).forEach((v, i) => (shown[i] = settle(shown[i], v)));
      visualizerById(id).draw(g, shown, w, h, appearance.palette, ms / 1000);
    };
    raf = requestAnimationFrame(draw);
    return () => { cancelAnimationFrame(raf); src.disconnect(an); c.remove(); };
  }

  function spectrum(src: AudioNode, el: HTMLDivElement, v: Extract<LiveViz, { kind: "spectrum" }>) {
    const am = new AudioMotionAnalyzer(el, {
      source: src, connectSpeakers: false, overlay: true, showBgColor: false, bgAlpha: 0, showScaleX: false,
      showPeaks: true, smoothing: 0.7, minFreq: 30, maxFreq: 16000, ...v.options,
    });
    const stop = $effect.root(() => {
      $effect(() => { am.registerGradient("theme", themeGradient(appearance.palette, appearance.spectrum)); am.gradient = "theme"; });
    });
    return () => { stop(); am.destroy(); };
  }

  function milkdrop(src: AudioNode, el: HTMLDivElement) {
    let alive = true, dispose = () => {};
    // Milkdrop is a megabyte of presets and a WebGL renderer: fetched only when picked.
    Promise.all([import("butterchurn"), import("butterchurn-presets")]).then(([bc, bp]) => {
      if (!alive) return;
      const presets = bp.default.getPresets();
      const names = Object.keys(presets);
      const c = document.createElement("canvas");
      el.append(c);
      // butterchurn draws its output over width x height buffer pixels (its pixelRatio only scales its
      // textures), so it's given buffer pixels; capped at 2x, as 3x phones would pay for it in heat.
      const size = () => {
        const r = Math.min(devicePixelRatio || 1, 2);
        return [Math.round((c.clientWidth || 640) * r), Math.round((c.clientHeight || 220) * r)];
      };
      let [w, h] = size();
      c.width = w; c.height = h;
      const v = bc.default.createVisualizer(src.context, c, { width: w, height: h, pixelRatio: 1 });
      v.connectAudio(src);
      const pick = (blend: number) => {
        presetName = names[Math.floor(Math.random() * names.length)];
        v.loadPreset(presets[presetName], blend);
      };
      pick(0);
      nextPreset = () => pick(2.7);
      let raf = 0;
      const draw = () => {
        raf = requestAnimationFrame(draw);
        const [nw, nh] = size();
        if (nw !== w || nh !== h) {
          [w, h] = [nw, nh];
          c.width = w; c.height = h;
          v.setRendererSize(w, h);
        }
        v.render();
      };
      raf = requestAnimationFrame(draw);
      // butterchurn has no teardown; dropping its canvas and frames lets it go.
      dispose = () => { cancelAnimationFrame(raf); c.remove(); };
    });
    return () => { alive = false; nextPreset = null; presetName = ""; dispose(); };
  }

  $effect(() => {
    const el = stage, src = source, v = viz;
    if (!el || !src || v.kind === "off") return;
    if (v.kind === "canvas") return canvas(src, el, v.id);
    if (v.kind === "spectrum") return spectrum(src, el, v);
    return milkdrop(src, el);
  });

  // A new song gets a new Milkdrop preset.
  $effect(() => { if (trackId) nextPreset?.(); });
</script>

{#if viz.kind !== "off"}
  <div class="stage" bind:this={stage}>
    {#if !source}<div class="idle">Press play to see the music</div>{/if}
    {#if presetName}
      <button class="preset" title="Another preset" onclick={() => nextPreset?.()}>{presetName}</button>
    {/if}
  </div>
{/if}

<style>
  .stage { position: relative; margin-top: 16px; height: 200px; border-radius: 10px; overflow: hidden;
    background: color-mix(in srgb, var(--accent) 6%, var(--card)); }
  .stage :global(canvas) { position: absolute; inset: 0; width: 100% !important; height: 100% !important; }
  .idle { position: absolute; inset: 0; display: grid; place-items: center; color: var(--muted); font-size: 13px; }
  .preset { position: absolute; left: 8px; right: 8px; bottom: 8px; z-index: 1; font-size: 11px; padding: 3px 8px;
    max-width: max-content; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; opacity: .55;
    background: rgb(0 0 0 / .45); color: #fff; border: 0; border-radius: 6px; }
  .preset:hover { opacity: 1; }
</style>
