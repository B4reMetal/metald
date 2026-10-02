<script lang="ts">
  import type { Dashboard } from "../lib/dashboard.svelte";
  import { session } from "../lib/session.svelte";
  import { ago } from "../lib/format";
  import Card from "./Card.svelte";
  let { board }: { board: Dashboard } = $props();

  let s = $derived(board.status);
  let running = $derived(s?.inflight.filter((r) => r.running).length ?? 0);
  let waiting = $derived(s?.inflight.filter((r) => !r.running).length ?? 0);
</script>

<Card title="Bot">
  {#snippet actions()}
    {#each s?.networks ?? [] as n (n.name)}
      <button onclick={() => board.act(() => board.api.setPaused(n.name, !n.paused), `${n.paused ? "resumed" : "paused"} work on ${n.name}`)}>
        {n.paused ? "Resume" : "Pause"} work on {n.name}
      </button>
    {/each}
    {#if session.state === "token"}<button onclick={() => session.signOut()}>Sign out</button>{/if}
  {/snippet}
  {#if s}
    <dl>
      <div><dt>up</dt><dd>{ago(s.now - s.started)}</dd></div>
      <div><dt>running / limit</dt><dd>{running} / {s.concurrency}</dd></div>
      <div><dt>waiting</dt><dd>{waiting}</dd></div>
      <div><dt>version</dt><dd>{s.version}</dd></div>
      {#if session.state === "proxy"}<div><dt>signed in</dt><dd>{session.user}</dd></div>{/if}
    </dl>
    {#each s.networks.filter((n) => n.paused) as n (n.name)}
      <p class="sub">Work on {n.name} is paused: no tasks, goals or schedules start until it's resumed.</p>
    {/each}
  {:else}
    <p class="empty">Loading…</p>
  {/if}
</Card>

<style>
  dl { display: flex; flex-wrap: wrap; gap: 8px 28px; margin: 0; }
  dd { margin: 0; font-size: 20px; font-variant-numeric: tabular-nums; }
  dt { order: 2; font-size: 13px; color: var(--muted); }
  dl div { display: flex; flex-direction: column; }
  p { margin: 10px 0 0; }
</style>
