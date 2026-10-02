<script lang="ts">
  import Shell from "../components/Shell.svelte";
  import LiveVisualizer from "../components/LiveVisualizer.svelte";
  import Controls from "../components/Controls.svelte";
  import Progress from "../components/Progress.svelte";
  import Lyrics from "../components/Lyrics.svelte";
  import UpNext from "../components/UpNext.svelte";
  import { api } from "../lib/api";
  import { Feed } from "../lib/feed.svelte";
  import { LivePlayer, appleMobile } from "../lib/live/player.svelte";
  import { LIVE_VISUALIZERS, liveVizById } from "../lib/live/visualizers";
  import { prefs } from "../lib/prefs.svelte";
  import { FADE } from "../lib/sync";
  import type { NowPlaying } from "../lib/types";

  const params = new URLSearchParams(location.search);
  const debug = params.has("debug");
  const feed = new Feed();
  // ?shadow forces iOS's way of feeding the visualizers, to test it on another browser.
  const player = new LivePlayer("hls/live.m3u8", appleMobile || params.has("shadow"));
  $effect(() => feed.start());

  // The listener hears the stream some seconds behind what's on air; which track and where in it come
  // from the stamps in the stream, and the track's details from the API. Stopped, the page shows what's
  // on air.
  let details = $state<Record<string, NowPlaying>>({});
  const about = (file: string): NowPlaying | undefined => (file === feed.now.file ? feed.now : details[file]);
  let heard = $state<{ id: string; position: number } | null>(null);
  let info = $state("");
  $effect(() => {
    const t = setInterval(() => {
      heard = player.now(FADE, (file) => !!about(file)?.dj);
      if (debug) info = player.debugInfo();
    }, 100);
    return () => clearInterval(t);
  });
  $effect(() => {
    const file = heard?.id;
    if (file && !about(file)) api.meta(file).then((m) => (details = { ...details, [file]: m }), () => {});
  });
  let shown: NowPlaying = $derived(heard ? (about(heard.id) ?? { title: "…" }) : feed.now);

  // Counted as a listener while playing: Icecast only counts its own.
  const beatId = Array.from(crypto.getRandomValues(new Uint8Array(12)), (b) => b.toString(16).padStart(2, "0")).join("");
  $effect(() => {
    if (!player.playing) return;
    const beat = () => fetch(`api/listening?id=${beatId}`, { cache: "no-store" }).catch(() => {});
    beat();
    const t = setInterval(beat, 10000);
    return () => clearInterval(t);
  });

  $effect(() => player.setAnalysis(liveVizById(prefs.visualizer).kind !== "off"));

  // Lock screen and media keys.
  $effect(() => {
    if (!("mediaSession" in navigator) || !shown.title) return;
    navigator.mediaSession.metadata = new MediaMetadata({ title: shown.title, artist: shown.by ?? "" });
  });
  $effect(() => {
    if (!("mediaSession" in navigator)) return;
    navigator.mediaSession.setActionHandler("play", () => player.start());
    navigator.mediaSession.setActionHandler("pause", () => player.stop());
    navigator.mediaSession.setActionHandler("stop", () => player.stop());
  });
</script>

<Shell page="listen" visualizers={LIVE_VISUALIZERS}>
  <div class="card">
    <div class="label">Now playing {#if feed.now.listeners != null}<span>· {feed.now.listeners} listening</span>{/if}</div>
    <h1>{feed.offline && !heard ? "Radio is offline" : shown.title || "Nothing playing"}</h1>
    <div class="by">
      {[shown.by && `queued by ${shown.by}`, shown.requested_by && shown.requested_by !== shown.by && `requested by ${shown.requested_by}`].filter(Boolean).join(" · ")}
    </div>
    {#if heard && shown.seconds}<Progress position={heard.position} seconds={shown.seconds} />{/if}
    <LiveVisualizer source={player.source} vizId={prefs.visualizer} trackId={heard?.id} />
    <Controls {player} />
    {#if debug}<pre class="debug">{heard ? `track ${heard.id} · ${heard.position.toFixed(2)} s · nudge ${prefs.nudge.toFixed(1)} s` : "no stamp heard"}{"\n" + info}</pre>{/if}
  </div>
  <Lyrics now={shown} position={heard?.position ?? null} />
  <UpNext queue={feed.queue} />
</Shell>

<style>
  h1 { font-size: 24px; font-weight: 650; margin: 4px 0 2px; overflow-wrap: anywhere; }
  .by { color: var(--muted); font-size: 14px; min-height: 21px; }
  .debug { margin: 10px 0 0; font: 12px/1.4 ui-monospace, monospace; color: var(--muted); white-space: pre-wrap; }
</style>
