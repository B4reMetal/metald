import type { Gpu, NetworkState, Radio, Reminder, Status, Task } from "./types";

export class Unauthorized extends Error {
  constructor() { super("the token was refused"); }
}

/** The operator API (/api/v1). The token, when there is one, goes in a header, never a cookie; behind
 *  the auth proxy there is none and the proxy's session does the work. */
export class Api {
  constructor(private readonly token: string) {}

  private async call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(`api/v1/${path}`, {
      method,
      cache: "no-store",
      headers: {
        ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}),
        ...(body ? { "Content-Type": "application/json" } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
    });
    if (res.status === 401) throw new Unauthorized();
    const out = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(out.error ?? `failed (${res.status})`);
    return out as T;
  }

  status = () => this.call<Status>("GET", "status");
  tasks = () => this.call<{ tasks: Task[] }>("GET", "tasks").then((r) => r.tasks);
  reminders = () => this.call<{ reminders: Reminder[] }>("GET", "reminders").then((r) => r.reminders);
  gpu = () => this.call<Gpu>("GET", "gpu");
  radio = () => this.call<Radio>("GET", "radio");

  cancelTask = (t: Pick<Task, "network" | "id">) =>
    this.call<{ cancelled: number }>("POST", `tasks/${encodeURIComponent(t.network)}/${t.id}/cancel`);
  setPaused = (network: string, paused: boolean) =>
    this.call<NetworkState>("PUT", `networks/${encodeURIComponent(network)}/paused`, { paused });
  cancelGpuJob = (id: string) => this.call<{ cancelled: string }>("POST", `gpu/${encodeURIComponent(id)}/cancel`);
  cancelReminder = (r: Pick<Reminder, "network" | "id">) =>
    this.call<{ cancelled: string }>("DELETE", `reminders/${encodeURIComponent(r.network)}/${encodeURIComponent(r.id)}`);
}
