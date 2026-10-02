<script lang="ts">
  import Shell from "../components/Shell.svelte";
  import Lyrics from "../components/Lyrics.svelte";
  import Controls from "../components/Controls.svelte";
  import Progress from "../components/Progress.svelte";
  import LiveVisualizer from "../components/LiveVisualizer.svelte";
  import { prefs } from "../lib/prefs.svelte";
  import { StationFeed, type StationTrack } from "../lib/station/feed.svelte";
  import { LivePlayer, appleMobile } from "../lib/live/player.svelte";
  import { LIVE_VISUALIZERS, liveVizById } from "../lib/live/visualizers";

  const params = new URLSearchParams(location.search);
  const debug = params.has("debug");
  const feed = new StationFeed();
  // ?shadow forces iOS's way of feeding the visualizers, to test it on another browser.
  const player = new LivePlayer("hls/live.m3u8", appleMobile || params.has("shadow"));
  if (debug) Object.assign(window, { player });
  $effect(() => feed.start());

  // Which track is heard and where in it comes from the stamps in the audio; the relay only supplies the
  // details. Stopped, the page shows what's on air.
  let heard = $state<{ id: string; position: number } | null>(null);
  let info = $state("");
  $effect(() => {
    const t = setInterval(() => {
      heard = player.now();
      if (debug) info = player.debugInfo();
    }, 100);
    return () => clearInterval(t);
  });
  let shown: StationTrack | null = $derived(
    heard ? (feed.track(heard.id) ?? { id: heard.id, title: "…" }) : feed.state.now,
  );
  // Details for a track the feed hasn't reported yet (just started, or the page just opened).
  $effect(() => { if (heard && !feed.track(heard.id)) feed.refresh(); });

  $effect(() => player.setAnalysis(liveVizById(prefs.visualizer).kind !== "off"));

  // Lock screen and media keys.
  $effect(() => {
    if (!("mediaSession" in navigator) || !shown) return;
    navigator.mediaSession.metadata = new MediaMetadata({ title: shown.title, artist: shown.artist ?? "", album: shown.album ?? "" });
  });
  $effect(() => {
    if (!("mediaSession" in navigator)) return;
    navigator.mediaSession.setActionHandler("play", () => player.start());
    navigator.mediaSession.setActionHandler("pause", () => player.stop());
    navigator.mediaSession.setActionHandler("stop", () => player.stop());
  });

  let history = $derived(feed.state.history.filter((t) => t.id !== shown?.id).slice(0, 8));
  const clock = (s?: number) => (s ? new Date(s * 1000).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }) : "");

</script>

<Shell page="station" visualizers={LIVE_VISUALIZERS}>
  <div class="card">
    <div class="label">{heard ? "Now playing" : "On air"}{#if feed.state.library}<span> · {feed.state.library.toLocaleString()} tracks</span>{/if}</div>
    <h1>{feed.offline && !heard ? "Station is offline" : shown?.title || "Nothing playing"}</h1>
    <div class="by">{[shown?.artist, shown?.album, shown?.year].filter(Boolean).join(" · ")}</div>
    {#if heard && shown?.seconds}
      <Progress position={heard.position} seconds={shown.seconds} />
    {/if}
    <LiveVisualizer source={player.source} vizId={prefs.visualizer} trackId={heard?.id} />
    <Controls {player} />
    {#if debug}<pre class="debug">{heard ? `track ${heard.id} · ${heard.position.toFixed(2)} s · nudge ${prefs.nudge.toFixed(1)} s` : "no stamp heard"}{"\n" + info}</pre>{/if}
  </div>
  {#if shown}<Lyrics now={shown} position={heard?.position ?? null} />{/if}
  <section>
    <div class="label">Up next</div>
    {#if feed.state.next.length}
      <ol>{#each feed.state.next as t, i (i)}<li>{t.title}{#if t.artist}<span class="muted"> · {t.artist}</span>{/if}</li>{/each}</ol>
    {:else}<p class="empty">Choosing…</p>{/if}
  </section>
  {#if history.length}
    <section>
      <div class="label">Earlier</div>
      <ul>{#each history as t, i (i)}<li><span class="when">{clock(t.started)}</span>{t.title}{#if t.artist}<span class="muted"> · {t.artist}</span>{/if}</li>{/each}</ul>
    </section>
  {/if}
</Shell>

<style>
  h1 { font-size: 24px; font-weight: 650; margin: 4px 0 2px; overflow-wrap: anywhere; }
  .by { color: var(--muted); font-size: 14px; min-height: 21px; overflow-wrap: anywhere; }
  .debug { margin: 10px 0 0; font: 12px/1.4 ui-monospace, monospace; color: var(--muted); white-space: pre-wrap; }
  section { margin-top: 24px; }
  ol, ul { margin: 8px 0 0; padding-left: 22px; overflow-wrap: anywhere; }
  ul { list-style: none; padding-left: 0; }
  .muted { color: var(--muted); }
  .when { display: inline-block; min-width: 72px; color: var(--muted); font-size: 13px; font-variant-numeric: tabular-nums; }
</style>
