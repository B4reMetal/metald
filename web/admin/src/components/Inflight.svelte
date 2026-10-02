<script lang="ts">
  import type { Dashboard } from "../lib/dashboard.svelte";
  import { ago } from "../lib/format";
  import Badge from "./Badge.svelte";
  import Card from "./Card.svelte";
  let { board }: { board: Dashboard } = $props();
</script>

<Card title="Working on now">
  {#if board.status?.inflight.length}
    <ul class="rows">
      {#each board.status.inflight as r (r.key + r.since)}
        <li>
          <Badge text={r.running ? "running" : "waiting"} tone={r.running ? "busy" : "plain"} />
          <span class="grow">{r.source ? `${r.source} · ` : ""}{r.key} · {r.operation}</span>
          <span class="sub" title="how long it has been in">{ago(board.status.now - r.since)}</span>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="empty">Idle.</p>
  {/if}
</Card>
