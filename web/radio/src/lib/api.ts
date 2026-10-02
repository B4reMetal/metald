import type { NowPlaying, Queue, Track } from "./types";

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`api/${path}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
  return (await res.json()) as T;
}

export const api = {
  now: () => get<NowPlaying>("now"),
  queue: () => get<Queue>("queue"),
  /** One track's details, by its library file name. */
  meta: (file: string) => get<NowPlaying>(`meta/${encodeURIComponent(file)}`),
  library: () => get<{ tracks: Track[] }>("library").then((r) => r.tracks),
};

/** The radio's one push connection: "now" and "queue" when they change. */
export function events(handlers: {
  now: (n: NowPlaying) => void;
  queue: (q: Queue) => void;
  /** The stream can't be held open (no EventSource, or the server refused it): fall back to polling. */
  closed: () => void;
}): () => void {
  if (typeof EventSource === "undefined") {
    handlers.closed();
    return () => {};
  }
  const es = new EventSource("api/events");
  es.addEventListener("now", (e) => handlers.now(JSON.parse(e.data)));
  es.addEventListener("queue", (e) => handlers.queue(JSON.parse(e.data)));
  // EventSource retries on its own; it only gives up (CLOSED) on an error response such as a full server.
  es.onerror = () => { if (es.readyState === EventSource.CLOSED) handlers.closed(); };
  return () => es.close();
}
