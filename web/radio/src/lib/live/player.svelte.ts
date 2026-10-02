import Hls from "hls.js/light";
import { isAppleMobile } from "../device";
import { prefs } from "../prefs.svelte";
import { adtsFromTs } from "./adts";
import { placeMarks, shownAt, type Mark } from "./stamps";

export const appleMobile = typeof navigator !== "undefined" && isAppleMobile(navigator.userAgent, navigator.platform, navigator.maxTouchPoints);

/** A segment hls.js has loaded: its decoded audio (shadow mode) and, once buffered, where it sits. */
interface Segment { audio?: AudioBuffer; start?: number; played?: boolean }

/** The station's HTML5 player: HLS through hls.js (Media Source; iOS 17.1+ included) or the browser's
 *  own HLS where that's all there is.
 *
 *  The visualizers listen through `source`, which comes one of two ways while analysis is on:
 *  - tap (everywhere but iOS): the element plays through Web Audio, element -> gain -> speakers, and
 *    the visualizers tap the element; the gain is the page's volume control.
 *  - shadow (iOS): iOS gives Web Audio silence for a Media Source element, so the element plays
 *    directly and the page decodes its own copy of each segment hls.js loads, played muted into
 *    `source` at the moment the element plays that segment. Music the listener hears never goes through
 *    Web Audio, so locking the screen doesn't stop it.
 *
 *  Where the listener is in a track comes from the edge's stamps.json and the segment positions hls.js
 *  reports (stamps.ts); the browser's own HLS reports neither, so there it isn't known. */
export class LivePlayer {
  playing = $state(false);
  muted = $state(false);
  /** What the visualizers listen to, while analysis is on and the audio is playing. */
  source = $state<AudioNode | null>(null);

  private readonly audio: HTMLAudioElement;
  private hls: Hls | null = null;
  private ctx: AudioContext | null = null;
  private gain: GainNode | null = null;
  private analysisWanted = false;
  private marks: Mark[] = [];
  /** Loaded segments by file name, oldest first. */
  private segments = new Map<string, Segment>();
  /** Where each buffered segment ends on the element's timeline, for placing the stamps. */
  private ends = new Map<string, number>();
  private polling: ReturnType<typeof setInterval> | undefined;
  /** For debugInfo: whether `source` carries sound. */
  private probe: AnalyserNode | undefined;

  /** shadow: decode a muted copy for the visualizers instead of tapping the element (see above). */
  constructor(private readonly url: string, private readonly shadow = appleMobile) {
    this.audio = new Audio();
    this.audio.preload = "none";
    // ManagedMediaSource (iOS) plays only with AirPlay off or an alternative source; this is a live page.
    this.audio.disableRemotePlayback = true;
    this.applyVolume();
  }

  get level() { return this.muted ? 0 : prefs.volume; }
  /** iOS ignores the element's volume, and in shadow mode nothing else carries the music. */
  get volumeWorks() { return !appleMobile && !this.shadow; }

  /** What's heard now: the track id and seconds into it. With a crossfade of `fade` seconds the last
   *  track counts until the fade is over (see shownAt). */
  now(fade = 0, isDj: (id: string) => boolean = () => false): { id: string; position: number } | null {
    if (!this.playing || this.audio.paused) return null;
    const heard = shownAt(placeMarks(this.marks, this.ends), this.audio.currentTime, fade, isDj);
    return heard?.id ? heard : null; // an empty id is the radio gone quiet
  }

  /** For the page's ?debug readout. */
  debugInfo() {
    let level = "-";
    if (this.source) {
      if (!this.probe) {
        this.probe = this.source.context.createAnalyser();
        this.source.connect(this.probe);
      }
      const buf = new Float32Array(this.probe.fftSize);
      this.probe.getFloatTimeDomainData(buf);
      level = Math.sqrt(buf.reduce((a, v) => a + v * v, 0) / buf.length).toFixed(4);
    }
    const decoded = [...this.segments.values()].filter((s) => s.audio).length;
    return `${this.hls ? "hls.js" : "native HLS"} · ${this.shadow ? `shadow (${decoded} decoded)` : "tap"} · audio ${this.ctx?.state ?? "off"} · level ${level}\n` +
      `marks ${this.marks.length} · placed ${placeMarks(this.marks, this.ends).length} · t ${this.audio.currentTime.toFixed(2)}`;
  }

  toggle() { this.playing ? this.stop() : this.start(); }

  start() {
    this.playing = true;
    this.segments.clear();
    this.ends.clear();
    if (Hls.isSupported()) {
      this.hls = new Hls({ liveSyncDurationCount: 3, enableWorker: true });
      this.hls.loadSource(this.url);
      this.hls.attachMedia(this.audio);
      this.hls.on(Hls.Events.FRAG_LOADED, (_e, d) => {
        // Copied now: hls.js hands the original to its worker afterwards.
        if (this.shadow && this.analysisWanted) this.decode(name(d.frag.relurl), d.payload.slice(0));
      });
      // Buffered means hls.js has placed the segment by its audio timestamps.
      this.hls.on(Hls.Events.FRAG_BUFFERED, (_e, d) => {
        const file = name(d.frag.relurl);
        this.segment(file).start = d.frag.start;
        this.ends.set(file, d.frag.start + d.frag.duration);
        if (this.ends.size > 100) this.ends.delete(this.ends.keys().next().value!);
        this.schedule();
      });
      this.hls.on(Hls.Events.ERROR, (_e, d) => { if (d.fatal) this.recover(d.type); });
      this.fetchMarks();
      this.polling = setInterval(() => this.fetchMarks(), 2000);
    } else {
      this.audio.src = this.url;
    }
    this.connectAnalysis();
    this.audio.play().catch(() => this.stop());
  }

  stop() {
    this.playing = false;
    clearInterval(this.polling);
    this.hls?.destroy();
    this.hls = null;
    this.audio.pause();
    this.audio.removeAttribute("src");
    this.audio.load();
  }

  /** Analysis wanted (a visualizer is showing). Takes effect at once while playing. */
  setAnalysis(on: boolean) {
    this.analysisWanted = on;
    if (on) this.connectAnalysis();
  }

  setVolume(v: number) {
    prefs.setVolume(v);
    this.muted = v === 0;
    this.applyVolume();
  }

  toggleMute() {
    if (this.muted || prefs.volume === 0) {
      this.muted = false;
      if (prefs.volume === 0) prefs.setVolume(0.5);
    } else {
      this.muted = true;
    }
    this.applyVolume();
  }

  private segment(file: string): Segment {
    let s = this.segments.get(file);
    if (!s) {
      s = {};
      this.segments.set(file, s);
      // A minute of segments is plenty; older ones are behind the playhead.
      if (this.segments.size > 30) this.segments.delete(this.segments.keys().next().value!);
    }
    return s;
  }

  private connectAnalysis() {
    if (!this.analysisWanted || !this.playing) return;
    if (this.ctx) {
      this.ctx.resume();
      return;
    }
    // Created inside the Play (or style) tap: browsers start audio only from a user gesture.
    this.ctx = new AudioContext();
    if (this.shadow) {
      // The visualizers hear `bus`; the muted gain keeps the graph pulled without making a sound.
      const bus = this.ctx.createGain(), mute = this.ctx.createGain();
      mute.gain.value = 0;
      bus.connect(mute).connect(this.ctx.destination);
      this.source = bus;
      return;
    }
    const src = this.ctx.createMediaElementSource(this.audio);
    this.gain = this.ctx.createGain();
    src.connect(this.gain).connect(this.ctx.destination);
    this.source = src;
    this.applyVolume();
  }

  private async decode(file: string, ts: ArrayBuffer) {
    if (!this.ctx) return;
    const adts = adtsFromTs(new Uint8Array(ts));
    if (!adts.length) return;
    try {
      this.segment(file).audio = await this.ctx.decodeAudioData(adts.buffer as ArrayBuffer);
      this.schedule();
    } catch {
      // An undecodable segment just leaves a gap in the visuals.
    }
  }

  /** Start each decoded, placed segment's muted copy when the element reaches it. */
  private schedule() {
    if (!this.ctx || !this.source || this.audio.paused) return;
    const now = this.audio.currentTime;
    for (const s of this.segments.values()) {
      if (s.played || !s.audio || s.start === undefined) continue;
      const end = s.start + s.audio.duration;
      if (end <= now) { s.played = true; continue; }
      const node = this.ctx.createBufferSource();
      node.buffer = s.audio;
      node.connect(this.source);
      const wait = s.start - now;
      // Ahead of the playhead: start when it gets there. Already under way: start part-way in.
      if (wait >= 0) node.start(this.ctx.currentTime + wait);
      else node.start(this.ctx.currentTime, -wait);
      s.played = true;
    }
  }

  private applyVolume() {
    if (this.gain) {
      this.gain.gain.value = this.level;
      this.audio.volume = 1;
    } else {
      this.audio.volume = this.level;
    }
  }

  private recover(type: string) {
    if (type === Hls.ErrorTypes.MEDIA_ERROR) return this.hls?.recoverMediaError();
    // Network trouble (the station restarting, a lost connection): start again at the live edge.
    this.stop();
    setTimeout(() => this.start(), 3000);
  }

  private async fetchMarks() {
    try {
      const r = await fetch(this.url.replace(/[^/]*$/, "stamps.json"), { cache: "no-store" });
      if (r.ok) this.marks = await r.json();
    } catch {
      // A missed poll: the next one catches up.
    }
  }
}

const name = (relurl: string | undefined) => (relurl ?? "").split("/").pop()!;
