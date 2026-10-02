<script lang="ts">
  /** The player both pages use (lib/live/player.svelte.ts) fits; so does a stand-in in a test. */
  interface Playable {
    playing: boolean; level: number; volumeWorks: boolean;
    toggle(): void; toggleMute(): void; setVolume(v: number): void;
  }
  let { player }: { player: Playable } = $props();
  let level = $derived(player.level);
</script>

<div class="controls">
  <button class="play" onclick={() => player.toggle()}>{player.playing ? "Stop" : "Play"}</button>
  {#if player.volumeWorks}
    <div class="vol">
      <button class="mute" onclick={() => player.toggleMute()} aria-label={level > 0 ? "Mute" : "Unmute"} title={level > 0 ? "Mute" : "Unmute"}>
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M4 9h4l5-4v14l-5-4H4z" fill="currentColor" />
          {#if level > 0}<path d="M16 9.5a3.5 3.5 0 0 1 0 5" />{/if}
          {#if level > 0.5}<path d="M18.5 7a7 7 0 0 1 0 10" />{/if}
          {#if level === 0}<path d="M16.5 9.5l5 5m0-5l-5 5" />{/if}
        </svg>
      </button>
      <input type="range" min="0" max="1" step="0.05" value={level} aria-label="Volume" style="--fill:{level * 100}%"
        oninput={(e) => player.setVolume(parseFloat(e.currentTarget.value))} />
    </div>
  {/if}
</div>

<style>
  .controls { display: flex; align-items: center; gap: 14px; margin-top: 16px; }
  .play { font-weight: 600; color: var(--card); background: var(--accent); border: 0; border-radius: 999px; padding: 10px 26px; min-width: 96px; }
  .play:hover { filter: brightness(1.1); }
  .vol { display: flex; align-items: center; gap: 8px; margin-left: auto; }
  .mute { display: grid; place-items: center; width: 36px; height: 36px; padding: 0; border: 0; border-radius: 50%; color: var(--muted); }
  .mute:hover { color: var(--fg); background: color-mix(in srgb, var(--fg) 8%, transparent); }
  svg { width: 22px; height: 22px; fill: none; stroke: currentColor; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
  input { -webkit-appearance: none; appearance: none; width: 110px; height: 20px; margin: 0; padding: 0; border: 0; background: transparent; cursor: pointer; }
  input::-webkit-slider-runnable-track { height: 4px; border-radius: 2px; background: linear-gradient(to right, var(--accent) var(--fill), var(--line) var(--fill)); }
  input::-moz-range-track { height: 4px; border-radius: 2px; background: var(--line); }
  input::-moz-range-progress { height: 4px; border-radius: 2px; background: var(--accent); }
  input::-webkit-slider-thumb { -webkit-appearance: none; width: 14px; height: 14px; margin-top: -5px; border-radius: 50%; background: var(--accent); border: 2px solid var(--card); }
  input::-moz-range-thumb { width: 12px; height: 12px; border-radius: 50%; background: var(--accent); border: 2px solid var(--card); }
</style>
