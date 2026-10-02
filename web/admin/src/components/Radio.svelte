<script lang="ts">
  import type { Dashboard } from "../lib/dashboard.svelte";
  import Card from "./Card.svelte";
  let { board }: { board: Dashboard } = $props();
  let r = $derived(board.radio);
</script>

<Card title="Radio">
  {#if !r}<p class="empty">Loading…</p>
  {:else if !r.configured}<p class="empty">No radio configured.</p>
  {:else if !r.reachable}<p class="bad">The radio is unreachable.</p>
  {:else}
    <p class="title">{r.title || "Nothing playing"}</p>
    <p class="sub">{[r.by && `queued by ${r.by}`, r.listeners != null && `${r.listeners} listening`, `${r.queue.length} queued`].filter(Boolean).join(" · ")}</p>
    {#if r.queue.length}<p class="sub">Next: {r.queue.slice(0, 3).join(" · ")}{r.queue.length > 3 ? " …" : ""}</p>{/if}
  {/if}
</Card>

<style>
  p { margin: 0 0 4px; overflow-wrap: anywhere; }
  .title { font-weight: 600; }
  .bad { color: var(--bad); }
</style>
