<script lang="ts">
  import { ThinkingFeed } from "../lib/live.svelte";
  import Card from "./Card.svelte";
  let { token }: { token: string } = $props();

  const feed = new ThinkingFeed();
  $effect(() => feed.start(token));
  const time = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour12: false });
  // A request still producing reasoning in the last few seconds counts as live.
  let now = $state(Date.now());
  $effect(() => { const t = setInterval(() => (now = Date.now()), 1000); return () => clearInterval(t); });
</script>

<Card title="Thinking">
  {#snippet actions()}<span class="state {feed.state}">{feed.state}</span>{/snippet}
  {#each feed.thoughts as t (t.request)}
    {@const live = now - Date.parse(t.updated) < 4000}
    <article class:live>
      <header>
        <b>{t.source || "—"}</b> <span class="sub">{[t.target, t.network, time(t.started), live ? "thinking…" : ""].filter(Boolean).join(" · ")}</span>
      </header>
      <pre>{t.text}</pre>
    </article>
  {:else}
    <p class="empty">No reasoning yet. It appears here as the model thinks, request by request.</p>
  {/each}
</Card>

<style>
  article { border-top: 1px solid var(--line); padding: 10px 0; }
  article:first-child { border-top: 0; }
  header { margin-bottom: 4px; }
  pre { margin: 0; max-height: 40vh; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere;
    font: 13px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; color: var(--muted); }
  .live pre { color: var(--fg); }
  .live b { color: var(--accent); }
  .state { font-size: 12px; color: var(--muted); } .state.live { color: var(--ok); }
</style>
