<script lang="ts">
  import Shell from "../components/Shell.svelte";
  import { api } from "../lib/api";
  import type { Track } from "../lib/types";

  let tracks = $state<Track[]>([]);
  let playing = $state("");
  let queued = $state(new Set<string>());
  let failed = $state(false);
  let loaded = $state(false);
  let term = $state("");

  // DJ interludes are spoken links between songs, not songs.
  let songs = $derived(tracks.filter((t) => !t.dj && !/\(dj\)$/.test(t.title)));
  let shown = $derived(songs.filter((s) => !term.trim() || `${s.title} ${s.requested_by}`.toLowerCase().includes(term.trim().toLowerCase())));

  async function load() {
    try {
      const [lib, now, q] = await Promise.all([api.library(), api.now(), api.queue()]);
      tracks = lib; playing = now.title ?? ""; queued = new Set(q.files); failed = false;
    } catch { failed = true; }
    loaded = true;
  }
  $effect(() => { void load(); const t = setInterval(load, 30000); return () => clearInterval(t); });

  const clock = (s: number | null) => (s == null ? "" : `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`);
  const day = (t: number) => new Date(t * 1000).toLocaleDateString(undefined, { month: "short", day: "numeric" });
  /** Saved under the song's title rather than the library's short name. */
  const saveAs = (t: Track) => `${t.title.replace(/[\\/:*?"<>|\x00-\x1f]+/g, " ").trim().slice(0, 100) || "song"}.${t.file.split(".").pop()}`;
</script>

<Shell page="songs">
  <div class="bar">
    <input bind:value={term} placeholder="Search songs or requesters" aria-label="Search songs or requesters" />
    <span class="sub">{term.trim() ? `${shown.length} of ` : ""}{songs.length} songs</span>
  </div>
  <ul class="card list">
    {#if failed && !tracks.length}<li class="empty">Can't reach the radio right now.</li>
    {:else if !loaded}<li class="empty">Loading…</li>
    {:else if !shown.length}<li class="empty">{songs.length ? "No songs match." : "No songs yet."}</li>
    {:else}
      {#each shown as t (t.file)}
        <li class:on={t.title === playing}>
          <div class="main">
            <div class="title">
              {t.title}
              {#if t.title === playing}<span class="tag hot">playing</span>{:else if queued.has(t.file)}<span class="tag hot">up next</span>{/if}
              {#if t.timed}<span class="tag">read-along</span>{:else if t.lyrics}<span class="tag">lyrics</span>{/if}
            </div>
            {#if t.requested_by}<div class="sub">requested by {t.requested_by}</div>{/if}
          </div>
          <div class="sub meta">{[clock(t.seconds), day(t.queued_at), t.downloads ? `${t.downloads} download${t.downloads === 1 ? "" : "s"}` : ""].filter(Boolean).join(" · ")}</div>
          <a class="dl" href="dl/{encodeURIComponent(t.file)}" download={saveAs(t)} title="Download {t.title}">download</a>
        </li>
      {/each}
    {/if}
  </ul>
</Shell>

<style>
  .bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
  .bar input { flex: 1; min-width: 0; }
  .list { list-style: none; margin: 0; padding: 0; }
  li { display: flex; gap: 12px; align-items: baseline; padding: 10px 16px; border-top: 1px solid var(--line); }
  li:first-child { border-top: 0; }
  li.empty { display: block; padding: 16px; }
  .main { flex: 1; min-width: 0; overflow-wrap: anywhere; }
  li.on .title { font-weight: 600; }
  .meta { white-space: nowrap; font-variant-numeric: tabular-nums; }
  .tag { font-size: 11px; border: 1px solid var(--line); border-radius: 999px; padding: 0 7px; color: var(--muted); margin-left: 6px; white-space: nowrap; }
  .tag.hot { color: var(--accent); border-color: var(--accent); }
  .dl { font-size: 13px; text-decoration: none; white-space: nowrap; }
  .dl:hover { text-decoration: underline; }
  @media (max-width: 480px) { li { flex-wrap: wrap; } .main { flex-basis: 100%; } }
</style>
