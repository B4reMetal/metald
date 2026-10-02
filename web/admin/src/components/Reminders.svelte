<script lang="ts">
  import type { Dashboard } from "../lib/dashboard.svelte";
  import type { Reminder } from "../lib/types";
  import { when } from "../lib/format";
  import Card from "./Card.svelte";
  let { board }: { board: Dashboard } = $props();

  function cancel(r: Reminder) {
    if (confirm(`Cancel the reminder for ${r.nick}?\n\n${r.text.slice(0, 200)}`)) {
      void board.act(() => board.api.cancelReminder(r), `cancelled the reminder for ${r.nick}`);
    }
  }
</script>

<Card title="Reminders">
  {#if board.reminders.length}
    <ul class="rows">
      {#each board.reminders as r (r.network + r.id)}
        <li>
          <div class="grow">
            <div>{r.text}</div>
            <div class="sub">{r.network} · {r.channel} · for {r.nick} · set by {r.setBy} · due {when(r.due)}</div>
          </div>
          <button class="danger" onclick={() => cancel(r)}>Cancel</button>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="empty">No reminders pending.</p>
  {/if}
</Card>
