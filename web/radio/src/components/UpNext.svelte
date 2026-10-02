<script lang="ts">
  let { queue }: { queue: string[] } = $props();
</script>

<section>
  <div class="head"><div class="label">Up next</div><a class="label" href="songs.html">All songs</a></div>
  {#if queue.length}
    <ol>{#each queue as title, i (i)}<li>{title}</li>{/each}</ol>
  {:else}
    <p class="empty">Nothing queued.</p>
  {/if}
</section>

<style>
  section { margin-top: 24px; }
  .head { display: flex; justify-content: space-between; }
  a.label { color: var(--accent); text-decoration: none; }
  ol { margin: 8px 0 0; padding-left: 22px; }
</style>
