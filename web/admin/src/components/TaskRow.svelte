<script lang="ts">
  import type { Dashboard } from "../lib/dashboard.svelte";
  import type { Task } from "../lib/types";
  import { ago, when } from "../lib/format";
  import Badge from "./Badge.svelte";
  let { task, board }: { task: Task; board: Dashboard } = $props();

  const tones = { running: "busy", done: "good", failed: "bad", cancelled: "bad", queued: "plain", proposed: "plain" } as const;
  let details = $derived([
    task.network, task.channel, `by ${task.owner}`,
    task.kind === "schedule" && task.intervalSeconds ? `every ${ago(task.intervalSeconds)}` : "",
    task.runs ? `${task.runs}${task.maxRuns ? `/${task.maxRuns}` : ""} runs` : "",
    task.active && task.nextRun ? `next ${when(task.nextRun)}` : "",
    !task.active && task.finished ? `ended ${when(task.finished)}` : "",
  ].filter(Boolean).join(" · "));

  function cancel() {
    if (confirm(`Cancel ${task.kind} #${task.id}?\n\n${task.objective.slice(0, 200)}`)) {
      void board.act(() => board.api.cancelTask(task), `cancelled ${task.kind} #${task.id}`);
    }
  }
</script>

<li>
  <div class="grow">
    <div><Badge text={task.status} tone={tones[task.status]} /><Badge text="{task.kind} #{task.id}" />{task.objective}</div>
    <div class="sub">{details}</div>
    {#if task.result}
      <details><summary>result</summary><pre>{task.result}</pre></details>
    {/if}
  </div>
  {#if task.active}<button class="danger" onclick={cancel}>Cancel</button>{/if}
</li>

<style>
  details summary { cursor: pointer; color: var(--muted); font-size: 13px; margin-top: 2px; }
  pre { white-space: pre-wrap; font: 13px/1.4 ui-monospace, monospace; color: var(--muted); margin: 4px 0 0; }
</style>
