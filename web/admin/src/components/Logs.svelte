<script lang="ts">
  import { LogFeed } from "../lib/live.svelte";
  import type { LiveEvent } from "../lib/types";
  import Card from "./Card.svelte";
  let { token }: { token: string } = $props();

  const feed = new LogFeed();
  $effect(() => feed.start(token));

  const LEVELS = ["DEBUG", "INFO", "WARN", "ERROR"] as const;
  let minLevel = $state<(typeof LEVELS)[number]>("INFO");
  let filter = $state("");
  let follow = $state(true);
  let box: HTMLDivElement | undefined = $state();

  const text = (e: LiveEvent) => `${e.msg} ${Object.entries(e.attrs ?? {}).map(([k, v]) => `${k}=${v}`).join(" ")}`;
  let shown = $derived(feed.entries.filter((e) =>
    LEVELS.indexOf((e.level ?? "INFO") as (typeof LEVELS)[number]) >= LEVELS.indexOf(minLevel) &&
    (!filter.trim() || text(e).toLowerCase().includes(filter.trim().toLowerCase()))));

  $effect(() => { void shown.length; if (follow && box) box.scrollTop = box.scrollHeight; });
  const time = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour12: false });
</script>

<Card title="Log">
  {#snippet actions()}
    <span class="state {feed.state}">{feed.state}</span>
    <select bind:value={minLevel} aria-label="Lowest level shown">
      {#each LEVELS as l (l)}<option value={l}>{l.toLowerCase()}+</option>{/each}
    </select>
    <button onclick={() => (feed.paused ? feed.resume() : (feed.paused = true))}>
      {feed.paused ? `Resume${feed.held ? ` (${feed.held})` : ""}` : "Pause"}
    </button>
  {/snippet}
  <input class="filter" bind:value={filter} placeholder="Filter, e.g. tool_started or a nick" aria-label="Filter log" />
  <div class="log" bind:this={box} onscroll={() => box && (follow = box.scrollHeight - box.scrollTop - box.clientHeight < 40)}>
    {#each shown as e (e.seq)}
      <div class="row {e.level?.toLowerCase()}">
        <span class="t">{time(e.time)}</span><span class="lv">{e.level?.slice(0, 4)}</span><span class="m">{e.msg}</span>
        {#each Object.entries(e.attrs ?? {}) as [k, v] (k)}<span class="a"><i>{k}=</i>{v}</span>{/each}
      </div>
    {:else}
      <div class="empty">Nothing yet.</div>
    {/each}
  </div>
  {#if !follow}<button class="jump" onclick={() => { follow = true; if (box) box.scrollTop = box.scrollHeight; }}>Jump to latest</button>{/if}
</Card>

<style>
  .filter { width: 100%; margin-bottom: 8px; }
  .log { height: 62vh; overflow: auto; font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; background: var(--bg);
    border: 1px solid var(--line); border-radius: 8px; padding: 6px 8px; }
  .row { white-space: pre-wrap; overflow-wrap: anywhere; padding: 1px 0; }
  .t { color: var(--muted); margin-right: 6px; }
  .lv { display: inline-block; width: 3.6em; color: var(--muted); }
  .warn .lv { color: var(--accent); } .error .lv, .error .m { color: var(--bad); }
  .debug { opacity: .65; }
  .m { font-weight: 600; margin-right: 6px; }
  .a { margin-right: 8px; } .a i { font-style: normal; color: var(--muted); }
  .state { font-size: 12px; color: var(--muted); align-self: center; } .state.live { color: var(--ok); }
  select { font: inherit; font-size: 13px; color: var(--fg); background: var(--bg); border: 1px solid var(--line); border-radius: 7px; padding: 3px 6px; }
  .jump { margin-top: 8px; }
</style>
