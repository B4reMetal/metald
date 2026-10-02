import { follow } from "./stream";
import { session } from "./session.svelte";
import type { LiveEvent } from "./types";

const KEEP_LOGS = 1500;
const KEEP_THOUGHTS = 25;

/** The bot's log, live. Paused, new records are held back and shown on resume. */
export class LogFeed {
  entries = $state<LiveEvent[]>([]);
  state = $state<"connecting" | "live" | "reconnecting">("connecting");
  paused = $state(false);
  held = $state(0);
  private pending: LiveEvent[] = [];

  start(token: string): () => void {
    return follow("logs/stream", token, {
      event: (e) => {
        if (this.paused) { this.pending.push(e); this.held = this.pending.length; return; }
        this.push([e]);
      },
      state: (s) => { this.state = s; },
      refused: () => session.refused(),
    });
  }

  resume() {
    this.paused = false;
    this.push(this.pending);
    this.pending = [];
    this.held = 0;
  }

  private push(list: LiveEvent[]) {
    // A reconnect replays the backlog; skip what is already shown.
    const last = this.entries.at(-1)?.seq ?? 0;
    const fresh = list.filter((e) => e.seq > last);
    if (fresh.length) this.entries = [...this.entries, ...fresh].slice(-KEEP_LOGS);
  }
}

export interface Thought { request: string; network: string; source: string; target: string; text: string; started: string; updated: string }

/** The model's reasoning, gathered per request, newest first. */
export class ThinkingFeed {
  thoughts = $state<Thought[]>([]);
  state = $state<"connecting" | "live" | "reconnecting">("connecting");
  private lastSeq = 0;

  start(token: string): () => void {
    return follow("thinking/stream", token, {
      event: (e) => this.add(e),
      state: (s) => { this.state = s; },
      refused: () => session.refused(),
    });
  }

  private add(e: LiveEvent) {
    if (e.seq <= this.lastSeq || !e.request) return;
    this.lastSeq = e.seq;
    const i = this.thoughts.findIndex((t) => t.request === e.request);
    if (i >= 0) {
      const t = this.thoughts[i];
      t.text += e.text ?? "";
      t.updated = e.time;
      if (i > 0) this.thoughts = [t, ...this.thoughts.filter((_, j) => j !== i)];
    } else {
      this.thoughts = [{ request: e.request, network: e.network ?? "", source: e.source ?? "", target: e.target ?? "",
        text: e.text ?? "", started: e.time, updated: e.time }, ...this.thoughts].slice(0, KEEP_THOUGHTS);
    }
  }
}
