<script lang="ts">
  import type { Dashboard } from "../lib/dashboard.svelte";
  import type { GpuJob } from "../lib/types";
  import { ago } from "../lib/format";
  import Badge from "./Badge.svelte";
  import Card from "./Card.svelte";
  let { board }: { board: Dashboard } = $props();
  let g = $derived(board.gpu);
  let now = $derived(board.status?.now ?? Date.now() / 1000);

  function cancel(job: GpuJob) {
    const what = `${job.kind}${job.tool ? ` from ${job.tool}` : ""}`;
    const how = job.state === "running" ? "Stop the running" : "Remove the waiting";
    if (confirm(`${how} ${what}?\n\n${job.summary.slice(0, 200)}\n\nThe tool that asked for it will report a failure.`)) {
      void board.act(() => board.api.cancelGpuJob(job.id), `${job.state === "running" ? "stopped" : "removed"} the ${what}`);
    }
  }
</script>

<Card title="GPU queue">
  {#if !g}<p class="empty">Loading…</p>
  {:else if !g.configured}<p class="empty">No GPU backend configured.</p>
  {:else if !g.reachable}<p class="bad">The GPU backend is unreachable.</p>
  {:else if !g.jobs.length}<p class="empty">Idle.</p>
  {:else}
    <ul class="rows">
      {#each g.jobs as job (job.id)}
        <li>
          <div class="grow">
            <div>
              <Badge text={job.state === "running" ? "running" : `#${job.position} waiting`} tone={job.state === "running" ? "busy" : "plain"} />
              <Badge text={job.kind} />{#if job.tool}<span class="sub">from {job.tool}</span>{/if}
            </div>
            {#if job.summary}<div class="summary">{job.summary}</div>{/if}
          </div>
          <span class="sub" title={job.state === "running" ? "running for (at least)" : "waiting for (at least)"}>{ago(now - job.since)}</span>
          <button class="danger" onclick={() => cancel(job)}>{job.state === "running" ? "Stop" : "Remove"}</button>
        </li>
      {/each}
    </ul>
  {/if}
</Card>

<style>
  .summary { font-size: 13px; color: var(--muted); margin-top: 2px; overflow-wrap: anywhere; }
  .bad { color: var(--bad); }
</style>
