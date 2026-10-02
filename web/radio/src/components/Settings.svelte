<script lang="ts">
  import { prefs } from "../lib/prefs.svelte";
  import { THEMES, palette } from "../lib/themes";
  import { VISUALIZERS } from "../lib/visualizers";
  import { appearance } from "../lib/appearance.svelte";
  let { visualizers = VISUALIZERS }: { visualizers?: { id: string; label: string }[] } = $props();
  let open = $state(false);
  let box: HTMLDivElement | undefined = $state();

  function outside(e: PointerEvent) { if (open && box && !box.contains(e.target as Node)) open = false; }
  function escape(e: KeyboardEvent) { if (e.key === "Escape") open = false; }
</script>

<svelte:window onpointerdown={outside} onkeydown={escape} />

<div class="settings" bind:this={box}>
  <button aria-expanded={open} aria-haspopup="dialog" title="Theme and visualizer" onclick={() => (open = !open)}>
    <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3" /><path d="M12 2v3M12 19v3M4.2 4.2l2.1 2.1M17.7 17.7l2.1 2.1M2 12h3M19 12h3M4.2 19.8l2.1-2.1M17.7 6.3l2.1-2.1" /></svg>
    <span class="hide-narrow">Style</span>
  </button>
  {#if open}
    <div class="panel" role="dialog" aria-label="Theme and visualizer">
      <div class="label">Theme</div>
      <div class="themes">
        {#each THEMES as t (t.id)}
          {@const p = palette(t, appearance.prefersDark)}
          <button class="theme" class:on={prefs.theme === t.id} aria-pressed={prefs.theme === t.id} onclick={() => prefs.setTheme(t.id)}
            style="--sw-bg:{p.bg};--sw-a:{p.accent};--sw-b:{p.accent2};--sw-fg:{p.fg}">
            <span class="swatch"></span>{t.label}
          </button>
        {/each}
      </div>
      <div class="label">Visualizer</div>
      <div class="viz">
        {#each visualizers as v (v.id)}
          <button class:on={prefs.visualizer === v.id} aria-pressed={prefs.visualizer === v.id} onclick={() => prefs.setVisualizer(v.id)}>{v.label}</button>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .settings { position: relative; }
  button { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; }
  svg { width: 16px; height: 16px; fill: none; stroke: currentColor; stroke-width: 2; stroke-linecap: round; }
  .panel { position: absolute; right: 0; top: calc(100% + 8px); z-index: 10; width: min(320px, calc(100vw - 32px));
    background: var(--card); border: 1px solid var(--line); border-radius: 12px; padding: 14px; box-shadow: 0 12px 32px rgb(0 0 0 / .25); }
  .label { margin: 4px 0 8px; }
  .themes { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; margin-bottom: 12px; }
  .theme { justify-content: flex-start; }
  .swatch { width: 18px; height: 18px; border-radius: 50%; border: 2px solid var(--sw-bg); flex: none;
    background: linear-gradient(135deg, var(--sw-a) 50%, var(--sw-b) 50%); box-shadow: 0 0 0 1px var(--line); }
  .viz { display: flex; flex-wrap: wrap; gap: 6px; }
  .on { border-color: var(--accent); color: var(--accent); }
  @media (max-width: 420px) { .hide-narrow { display: none; } }
</style>
