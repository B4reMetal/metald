import type { LiveEvent } from "./types";

/** Read a Server-Sent Events stream with fetch, so the token can go in a header (EventSource can't send
 *  one). Reconnects with a growing delay until stop() is called; onRefused fires on 401. */
export function follow(
  path: string,
  token: string,
  on: { event: (e: LiveEvent) => void; state: (s: "live" | "reconnecting") => void; refused: () => void },
): () => void {
  let stopped = false;
  let abort = new AbortController();
  let delay = 1000;

  const run = async () => {
    while (!stopped) {
      abort = new AbortController();
      try {
        const res = await fetch(`api/v1/${path}`, {
          cache: "no-store",
          headers: token ? { Authorization: `Bearer ${token}` } : {},
          signal: abort.signal,
        });
        if (res.status === 401) { on.refused(); return; }
        if (!res.ok || !res.body) throw new Error(String(res.status));
        on.state("live");
        delay = 1000;
        const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
        let buf = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += value;
          let cut: number;
          while ((cut = buf.indexOf("\n\n")) >= 0) {
            const block = buf.slice(0, cut);
            buf = buf.slice(cut + 2);
            for (const line of block.split("\n")) {
              if (line.startsWith("data: ")) on.event(JSON.parse(line.slice(6)) as LiveEvent);
            }
          }
        }
      } catch { /* dropped or aborted: retry below unless stopped */ }
      if (stopped) return;
      on.state("reconnecting");
      await new Promise((r) => setTimeout(r, delay));
      delay = Math.min(delay * 2, 15000);
    }
  };
  void run();
  return () => { stopped = true; abort.abort(); };
}
