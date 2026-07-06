/*
  Project Work — pure helpers (no JSX). Filtering, status ordering, and the
  rank tables the sortable table sorts by. Kept framework-free so the board and
  table share one source of truth.
*/

import type { EpicSummary, Priority, Risk, TaskCapsule, TaskStatus } from "@/types/domain";

export type WorkView = "board" | "table";

/** Board column order — the six task statuses, left → right (packet §4.4). */
export const STATUS_COLUMNS: TaskStatus[] = [
  "backlog",
  "ready",
  "in_progress",
  "review",
  "blocked",
  "done",
];

// Rank tables: lower = "earlier"/"lighter". Sort direction toggles in the UI.
export const PRIORITY_RANK: Record<Priority, number> = { p0: 0, p1: 1, p2: 2, p3: 3 };
export const RISK_RANK: Record<Risk, number> = { low: 0, medium: 1, high: 2, critical: 3 };
export const STATUS_RANK: Record<TaskStatus, number> = {
  backlog: 0,
  ready: 1,
  in_progress: 2,
  review: 3,
  blocked: 4,
  done: 5,
};

export const RISK_VALUES: Risk[] = ["low", "medium", "high", "critical"];

// ----------------------------------------------------------------------------
// Filters
// ----------------------------------------------------------------------------

export type StatusFilter = TaskStatus | "all";
export type RiskFilter = Risk | "all";

export interface WorkFilters {
  /** Selected epic ids; empty = all epics. */
  epics: string[];
  status: StatusFilter;
  risk: RiskFilter;
  gateOnly: boolean;
}

export const EMPTY_FILTERS: WorkFilters = {
  epics: [],
  status: "all",
  risk: "all",
  gateOnly: false,
};

export function filtersActive(f: WorkFilters): boolean {
  return f.epics.length > 0 || f.status !== "all" || f.risk !== "all" || f.gateOnly;
}

export function applyFilters(tasks: TaskCapsule[], f: WorkFilters): TaskCapsule[] {
  return tasks.filter((t) => {
    if (f.epics.length > 0 && !f.epics.includes(t.epicId)) return false;
    if (f.status !== "all" && t.status !== f.status) return false;
    if (f.risk !== "all" && t.risk !== f.risk) return false;
    if (f.gateOnly && !t.hasGate) return false;
    return true;
  });
}

/** Distinct epics present in a task list, in first-seen order. */
export function epicsInTasks(tasks: TaskCapsule[]): Array<{ id: string; title: string }> {
  const seen = new Map<string, string>();
  for (const t of tasks) if (!seen.has(t.epicId)) seen.set(t.epicId, t.epicTitle);
  return [...seen].map(([id, title]) => ({ id, title }));
}

// ----------------------------------------------------------------------------
// Epic rollups
// ----------------------------------------------------------------------------

export interface Rollup {
  total: number;
  done: number;
  inProgress: number;
  blocked: number;
}

export function rollupFromCounts(counts: Record<TaskStatus, number>): Rollup {
  const total = STATUS_COLUMNS.reduce((n, s) => n + (counts[s] ?? 0), 0);
  return {
    total,
    done: counts.done ?? 0,
    inProgress: counts.in_progress ?? 0,
    blocked: counts.blocked ?? 0,
  };
}

export function rollupFromTasks(tasks: TaskCapsule[]): Rollup {
  return {
    total: tasks.length,
    done: tasks.filter((t) => t.status === "done").length,
    inProgress: tasks.filter((t) => t.status === "in_progress").length,
    blocked: tasks.filter((t) => t.status === "blocked").length,
  };
}

/** Prefer the authoritative epic rollup (useEpics); fall back to the visible tasks. */
export function resolveRollup(
  epicId: string,
  epicsById: Map<string, EpicSummary>,
  bucketTasks: TaskCapsule[],
): Rollup {
  const e = epicsById.get(epicId);
  return e ? rollupFromCounts(e.counts) : rollupFromTasks(bucketTasks);
}
