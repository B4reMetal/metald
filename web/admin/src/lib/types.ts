// Mirrors internal/admin/types.go.

export interface NetworkState { name: string; paused: boolean }

export interface InflightRequest { key: string; operation: string; source: string; since: number; running: boolean }

export interface Status {
  version: string; started: number; now: number; concurrency: number;
  networks: NetworkState[]; inflight: InflightRequest[];
}

export type TaskKind = "task" | "goal" | "schedule";
export type TaskStatus = "proposed" | "queued" | "running" | "done" | "failed" | "cancelled";

export interface Task {
  id: number; network: string; kind: TaskKind; status: TaskStatus; channel: string; owner: string;
  objective: string; result: string; runs: number; maxRuns: number; intervalSeconds: number;
  created: number; nextRun: number; finished: number; active: boolean;
}

export interface Reminder { id: string; network: string; nick: string; channel: string; text: string; due: number; setBy: string }

export interface GpuJob {
  id: string; state: "running" | "waiting"; position: number;
  kind: string; tool: string; summary: string;
  /** When the page first saw it queued (unix seconds). */
  since: number;
}

export interface Gpu { configured: boolean; reachable: boolean; jobs: GpuJob[] }

export interface Radio {
  configured: boolean; reachable: boolean; title: string; by: string; listeners: number | null; queue: string[];
}

/** A log record or a piece of reasoning (internal/core/live.go). */
export interface LiveEvent {
  seq: number; time: string;
  level?: string; msg?: string; attrs?: Record<string, string>;
  request?: string; network?: string; source?: string; target?: string; text?: string;
}
