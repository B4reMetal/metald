// The radio API's shapes (contrib/radio/api.py).

/** [seconds into the track, line]; a line like "[Chorus]" is a section label. */
export type TimedLine = [number, string];

export interface NowPlaying {
  /** The library file name: the id the stream's stamps name a track by. */
  file?: string;
  title?: string;
  by?: string;
  requested_by?: string;
  lyrics?: string;
  synced?: TimedLine[];
  /** The track's length. */
  seconds?: number | null;
  /** Unix seconds the current track started on air, on the server's clock. */
  started?: number | null;
  server_time?: number;
  listeners?: number | null;
  /** A spoken DJ interlude: it plays whole, without the crossfade. */
  dj?: boolean;
}

export interface Queue { queue: string[]; files: string[] }

export interface Track {
  file: string;
  title: string;
  seconds: number | null;
  queued_at: number;
  timed: boolean;
  lyrics: boolean;
  dj: boolean;
  requested_by: string;
  downloads: number;
}
