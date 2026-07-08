/*
  Project Runs board — pure helpers.

  Kept out of the row components so the split/sort/tone logic can be reasoned
  about (and typechecked) on its own. No JSX here.
*/

import type { Lane, Liveness, RunSummary } from "@/types/domain";

/**
 * The 6-column grid shared by the table header and every run row:
 * Task · Runner · Lane · Lease · Tokens · State. Lease_state is its own labeled
 * column; the State column carries liveness (active) or the outcome chip
 * (recent), so both lease_state and outcome are surfaced per row (SRV-T-0015 A2).
 */
export const RUNS_GRID = "grid grid-cols-[1fr_104px_64px_88px_72px_128px] gap-3";

/** Total tokens for a run — the board shows one compact figure per run. */
export function tokenTotal(run: RunSummary): number {
  return run.tokens.input + run.tokens.output;
}

/** A run is "active" while it is still running. */
export function isActive(run: RunSummary): boolean {
  return run.outcome === "running";
}

/** Liveness severity — most alarming first, so a dead process floats up. */
const LIVENESS_RANK: Record<Liveness, number> = { dead: 0, stale: 1, fresh: 2 };

/**
 * Split the project's runs into active (running) and recent (finished).
 * Active runs are ordered dead → stale → fresh (then longest-running), so a
 * silently-dead "running" process surfaces at the top of the board. Recent runs
 * show most-recent event first.
 */
export function partitionRuns(runs: RunSummary[]): {
  active: RunSummary[];
  recent: RunSummary[];
} {
  const active = runs
    .filter(isActive)
    .sort(
      (a, b) =>
        LIVENESS_RANK[a.liveness] - LIVENESS_RANK[b.liveness] ||
        b.elapsedSec - a.elapsedSec,
    );
  const recent = runs
    .filter((r) => !isActive(r))
    .sort((a, b) => a.sinceLastEventSec - b.sinceLastEventSec);
  return { active, recent };
}

/** Elapsed-time colour escalates with liveness: calm → amber → red. */
export function livenessTextClass(liveness: Liveness): string {
  return liveness === "dead"
    ? "text-fail"
    : liveness === "stale"
      ? "text-warn"
      : "text-ink-soft";
}

/** Row wash — only a dead "running" process gets an alarming tint. */
export function livenessRowClass(liveness: Liveness): string {
  return liveness === "dead" ? "bg-fail-soft" : "hover:bg-hover";
}

/** Lane hue: execute stays quiet, review is a visibly distinct phase. */
export function laneTextClass(lane: Lane): string {
  return lane === "review" ? "text-info" : "text-muted";
}

/** Lease state: expired is alarming, released/unclaimed are non-active, held is normal-quiet. */
export function leaseTextClass(lease: RunSummary["leaseState"]): string {
  return lease === "expired"
    ? "text-fail"
    : lease === "released" || lease === "unclaimed"
      ? "text-muted"
      : "text-faint";
}
