<script lang="ts">
  let { text, tone = "plain" }: { text: string; tone?: "plain" | "busy" | "good" | "bad" } = $props();
</script>

<span class="badge {tone}">{text}</span>

<style>
  .badge { display: inline-block; font-size: 11px; border: 1px solid var(--line); border-radius: 999px;
    padding: 0 7px; color: var(--muted); margin-right: 6px; white-space: nowrap; vertical-align: 1px; }
  .busy { color: var(--accent); border-color: var(--accent); }
  .good { color: var(--ok); border-color: var(--ok); }
  .bad { color: var(--bad); border-color: var(--bad); }
</style>
