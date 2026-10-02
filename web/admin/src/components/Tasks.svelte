<script lang="ts">
  import type { Dashboard } from "../lib/dashboard.svelte";
  import type { Task } from "../lib/types";
  import Card from "./Card.svelte";
  import TaskRow from "./TaskRow.svelte";
  let { board }: { board: Dashboard } = $props();

  const views = {
    active: { label: "Active", show: (t: Task) => t.active && t.kind !== "schedule" },
    schedules: { label: "Schedules", show: (t: Task) => t.active && t.kind === "schedule" },
    finished: { label: "Finished", show: (t: Task) => !t.active },
  } as const;
  type View = keyof typeof views;
  let view = $state<View>("active");
  let shown = $derived(board.tasks.filter(views[view].show));
</script>

<Card title="Tasks, goals and schedules">
  <div class="tabs" role="tablist">
    {#each Object.entries(views) as [key, v] (key)}
      <button role="tab" aria-selected={view === key} class:on={view === key} onclick={() => (view = key as View)}>
        {v.label} ({board.tasks.filter(v.show).length})
      </button>
    {/each}
  </div>
  {#if shown.length}
    <ul class="rows">
      {#each shown as task (task.network + task.id)}
        <TaskRow {task} {board} />
      {/each}
    </ul>
  {:else}
    <p class="empty">Nothing here.</p>
  {/if}
</Card>

<style>
  .tabs { display: flex; gap: 6px; margin-bottom: 10px; flex-wrap: wrap; }
  .on { border-color: var(--accent); color: var(--accent); }
</style>
