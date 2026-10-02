import { api, events } from "./api";
import type { NowPlaying } from "./types";

/** What the radio has on air and queued. One push connection carries both; polling stands in if it
 *  can't. The listener hears the stream some seconds behind this (Listen.svelte times that). */
export class Feed {
  now = $state<NowPlaying>({});
  queue = $state<string[]>([]);
  offline = $state(false);

  start(): () => void {
    const timers: ReturnType<typeof setInterval>[] = [];
    const poll = () => {
      const refresh = async () => {
        try {
          this.now = await api.now();
          this.queue = (await api.queue()).queue;
          this.offline = false;
        } catch { this.offline = true; }
      };
      void refresh();
      timers.push(setInterval(refresh, 5000));
    };
    const stop = events({
      now: (n) => { this.now = n; this.offline = false; },
      queue: (q) => { this.queue = q.queue; },
      closed: poll,
    });
    return () => { stop(); timers.forEach(clearInterval); };
  }
}
