<script lang="ts">
  import { prefs } from "../lib/prefs.svelte";
  import { lineAt } from "../lib/sync";
  import type { NowPlaying } from "../lib/types";
  let { now, position }: { now: NowPlaying; position: number | null } = $props();

  interface Row { time: number | null; text: string; section: boolean }
  let timed = $derived((now.synced ?? []).length > 0);
  // A DJ interlude's words are shown whole, without line-by-line highlighting.
  let synced = $derived(timed && !now.dj);
  let rows: Row[] = $derived(
    (timed ? now.synced! : (now.lyrics ?? "").split("\n").map((l): [null, string] => [null, l])).map(([time, text]) => {
      const m = text.match(/^\s*\[(.+)\]\s*$/);
      return { time, text: m ? m[1] : text || " ", section: !!m };
    }),
  );
  let hasWords = $derived(rows.some((r) => r.text.trim()));
  let current = $derived(synced ? lineAt(now.synced!, position === null ? null : position + prefs.nudge) : -1);
  // A section label lights up with its first line.
  let lit = $derived(new Set([current, rows[current]?.section && rows[current + 1]?.time === rows[current].time ? current + 1 : -1]));

  let box: HTMLDivElement | undefined = $state();
  $effect(() => {
    const el = box?.children[current] as HTMLElement | undefined;
    if (box && el) box.scrollTop = el.offsetTop - box.offsetTop - box.clientHeight / 3;
  });
</script>

<section>
  <div class="head">
    <div class="label">Lyrics</div>
    {#if synced}
      <div class="nudge">sync
        <button title="Lyrics are ahead of the singing: show them later" onclick={() => prefs.setNudge(prefs.nudge - 0.5)}>&minus;</button>
        <span>{prefs.nudge > 0 ? "+" : ""}{prefs.nudge.toFixed(1)}s</span>
        <button title="Lyrics are behind the singing: show them sooner" onclick={() => prefs.setNudge(prefs.nudge + 0.5)}>+</button>
      </div>
    {/if}
  </div>
  {#if hasWords}
    <div class="lyrics" class:synced bind:this={box}>
      {#each rows as r, i (i)}
        <div class="ln" class:sec={r.section} class:on={lit.has(i)}>{r.text}</div>
      {/each}
    </div>
  {:else}
    <p class="empty">No lyrics for this track.</p>
  {/if}
</section>

<style>
  section { margin-top: 24px; }
  .head { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
  .nudge { display: flex; gap: 6px; align-items: center; font-size: 12px; color: var(--muted); }
  .nudge button { padding: 2px 8px; font-size: 12px; }
  .lyrics { margin-top: 8px; max-height: 60vh; overflow-y: auto; scroll-behavior: smooth; overflow-wrap: anywhere; }
  .ln { padding: 2px 0; transition: color .25s; }
  .sec { color: var(--accent); font-size: 13px; font-weight: 600; margin-top: 12px; }
  .synced .ln { color: var(--dim); }
  .synced .ln.on { color: var(--fg); font-weight: 600; }
  .synced .sec { color: var(--dim); }
  .synced .sec.on { color: var(--accent); }
</style>
