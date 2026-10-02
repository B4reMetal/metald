<script lang="ts">
  import type { Snippet } from "svelte";
  let { title, actions, children }: { title: string; actions?: Snippet; children: Snippet } = $props();
</script>

<section>
  <header>
    <h2>{title}</h2>
    {#if actions}<div class="actions">{@render actions()}</div>{/if}
  </header>
  {@render children()}
</section>

<style>
  section { background: var(--card); border: 1px solid var(--line); border-radius: 12px; padding: 16px; margin-bottom: 18px; }
  header { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
  h2 { flex: 1; margin: 0; font-size: 12px; font-weight: 600; color: var(--muted); text-transform: uppercase; letter-spacing: .08em; }
  .actions { display: flex; gap: 6px; flex-wrap: wrap; }
</style>
