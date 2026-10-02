// The station's state from the relay (contrib/station/relay): what's on, what's next, what played.

import type { TimedLine } from "../types";

export interface StationTrack {
  id?: string;
  title: string;
  artist?: string;
  album?: string;
  year?: number | null;
  seconds?: number;
  started?: number;
  /** Only on the current and previous tracks. */
  lyrics?: string;
  synced?: TimedLine[];
}

export interface StationState {
  now: StationTrack | null;
  next: StationTrack[];
  history: StationTrack[];
  library: number;
}

export class StationFeed {
  state = $state<StationState>({ now: null, next: [], history: [], library: 0 });
  offline = $state(false);
  /** Tracks by id, kept a while: the listener hears a track for some seconds after the station has
   *  moved on from it. */
  private seen = new Map<string, StationTrack>();

  track(id: string | undefined): StationTrack | undefined {
    return id ? this.seen.get(id) : undefined;
  }

  async refresh() {
    try {
      const r = await fetch("api/state", { cache: "no-store" });
      if (!r.ok) throw new Error(String(r.status));
      const s = (await r.json()) as StationState;
      this.state = { now: s.now, next: s.next ?? [], history: s.history ?? [], library: s.library ?? 0 };
      for (const t of [s.now, ...(s.next ?? []), ...(s.history ?? [])]) {
        // A later copy without lyrics (history past the second entry) mustn't replace one with them.
        if (t?.id && !(this.seen.get(t.id)?.synced && !t.synced)) this.seen.set(t.id, t);
      }
      if (this.seen.size > 100) this.seen = new Map([...this.seen].slice(-50));
      this.offline = false;
    } catch {
      this.offline = true;
    }
  }

  /** Poll every few seconds; returns the stop function. */
  start(): () => void {
    this.refresh();
    const t = setInterval(() => this.refresh(), 5000);
    return () => clearInterval(t);
  }
}
