/*
  Run-detail local helpers. Pure derivations over the shared domain types — no
  new data invented here. Where the API must supply a field the run detail lacks
  today, it is marked `// TODO(api)` at the call site.
*/

import type { Attempt, DaemonStatus, RunDetail, RunEvent } from "@/types/domain";
import { duration } from "@/lib/time";

/** A derived stat cell for the run summary grid (design §07 — four headline numbers). */
export interface RunStat {
  label: string;
  value: string;
}

/** The run headline numbers avoid usage totals because runner telemetry is diagnostic-only. */
export function runStats(run: RunDetail, waitingForDaemon = false): RunStat[] {
  return [
    { label: "Elapsed", value: waitingForDaemon ? "Paused" : duration(run.elapsedSec) },
    { label: "Attempts", value: String(run.attemptCount) },
    { label: "Liveness", value: run.liveness },
  ];
}

export function isLiveHeaderRun(run: Pick<RunDetail, "leaseState" | "outcome">): boolean {
  return run.leaseState === "held" && (run.outcome === "running" || run.outcome === "stale");
}

export function waitingForDaemonReason(
  run: Pick<RunDetail, "outcome">,
  daemon: Pick<DaemonStatus, "daemonAlive" | "daemonDownReason"> | undefined,
): string | null {
  if (run.outcome !== "retry-queued" || daemon?.daemonAlive !== false) return null;
  return (
    daemon.daemonDownReason ??
    "Daemon process is not running. Start the daemon to dispatch this queued run."
  );
}

/** Compact attempt metadata without treating usage snapshots as a total. */
export function attemptMeta(a: Attempt): string {
  return duration(a.durationSec);
}

/**
 * Level → text tone classes for the event console. The design colors each line
 * by severity; normal protocol events read calm, warn/error pop (packet §4.3).
 */
export function eventToneClasses(level: RunEvent["level"]): { kind: string; text: string } {
  switch (level) {
    case "error":
      return { kind: "text-fail font-semibold", text: "text-fail" };
    case "warn":
      return { kind: "text-warn font-semibold", text: "text-warn" };
    default:
      return { kind: "text-info", text: "text-ink-soft" };
  }
}

/**
 * ISO → "18:22:05" wall clock (UTC, to stay consistent with the frozen mock
 * timeline). Defensive on purpose: a missing or unparseable timestamp renders
 * as "--:--:--", never "NaN:NaN:NaN". The event tail should
 * degrade to a placeholder, not shout NaN at the operator, if the API ever
 * emits a timestamp shape this can't parse.
 */
export function clockTime(iso: string): string {
  const d = new Date(iso);
  if (!iso || Number.isNaN(d.getTime())) return "--:--:--";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getUTCHours())}:${p(d.getUTCMinutes())}:${p(d.getUTCSeconds())}`;
}
