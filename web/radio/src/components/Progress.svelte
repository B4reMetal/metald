<script lang="ts">
  // How far into the track the listener is, and its length.
  let { position, seconds }: { position: number; seconds: number } = $props();
  const mmss = (s: number) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;
</script>

<div class="progress" aria-hidden="true"><div style="width:{Math.min(100, (position / seconds) * 100)}%"></div></div>
<div class="times"><span>{mmss(Math.min(position, seconds))}</span><span>{mmss(seconds)}</span></div>

<style>
  .progress { margin-top: 12px; height: 3px; border-radius: 2px; background: var(--line); overflow: hidden; }
  .progress div { height: 100%; background: var(--accent); transition: width .1s linear; }
  .times { display: flex; justify-content: space-between; font-size: 11px; color: var(--muted); margin-top: 4px; font-variant-numeric: tabular-nums; }
</style>
