/*
  Run-detail local helpers. Pure derivations over the shared domain types — no
  new data invented here. Where the API must supply a field the run detail lacks
  today, it is marked `// TODO(api)` at the call site.
*/

import type { Attempt, DaemonStatus, RunDetail, RunEvent, TaskStatus } from "@/types/domain";
import { compactNumber, duration } from "@/lib/time";

/** A derived stat cell for the run summary grid (design §07 — four headline numbers). */
export interface RunStat {
  label: string;
  value: string;
}

/** The four headline numbers shown above the run body. */
export function runStats(run: RunDetail, waitingForDaemon = false): RunStat[] {
  return [
    { label: "Elapsed", value: waitingForDaemon ? "Paused" : duration(run.elapsedSec) },
    { label: "Input", value: compactNumber(run.tokens.input) },
    { label: "Output", value: compactNumber(run.tokens.output) },
    { label: "Attempts", value: String(run.attemptCount) },
  ];
}

export function isLiveHeaderRun(run: Pick<RunDetail, "leaseState" | "outcome">): boolean {
  return run.leaseState === "held" && (run.outcome === "running" || run.outcome === "stale");
}

export function isInterruptibleRun(
  run: Pick<RunDetail, "leaseState" | "leaseStateRaw" | "outcome" | "processRunning">,
): boolean {
  const raw = run.leaseStateRaw;
  return (
    run.processRunning === true ||
    raw === "claimed" ||
    raw === "running" ||
    raw === "retry_queued" ||
    isLiveHeaderRun(run) ||
    run.outcome === "retry-queued"
  );
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

/** Compact "1m 04s · 21.1k→1.5k tok" attempt meta line. */
export function attemptMeta(a: Attempt): string {
  return `${duration(a.durationSec)} · ${compactNumber(a.tokens.input)}→${compactNumber(
    a.tokens.output,
  )} tok`;
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
 * as "--:--:--", never "NaN:NaN:NaN" (SRV-T-0015 A1). The event tail should
 * degrade to a placeholder, not shout NaN at the operator, if the API ever
 * emits a timestamp shape this can't parse.
 */
export function clockTime(iso: string): string {
  const d = new Date(iso);
  if (!iso || Number.isNaN(d.getTime())) return "--:--:--";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getUTCHours())}:${p(d.getUTCMinutes())}:${p(d.getUTCSeconds())}`;
}

/**
 * When redrive (Retry) is meaningless for the task's canonical status, return
 * the operator-facing reason to show inline and disable the control; otherwise
 * null (redrive is allowed). A review/done task has no execution to redrive —
 * clicking Retry there previously requeued into a silent daemon retire behind a
 * stale "Ready" badge (SRV-T-0016 A3). Point the operator at the real lane.
 */
export function redriveDisabledReason(
  status: TaskStatus | undefined,
  run?: Pick<RunDetail, "leaseState" | "leaseStateRaw" | "outcome" | "processRunning">,
  daemonDownReason?: string | null,
): string | null {
  switch (status) {
    case "review":
      return "Task is in review — resolve it in the review/land lane; there is no run to redrive.";
    case "done":
      return "Task is done — nothing to redrive.";
    default:
      break;
  }
  if (run?.outcome === "retry-queued" || run?.leaseStateRaw === "retry_queued") {
    return daemonDownReason
      ? `Redrive is already queued. ${daemonDownReason}`
      : "Redrive is already queued. Wait for the daemon to claim it, or interrupt the queued run first.";
  }
  if (run && isInterruptibleRun(run)) {
    return "Interrupt the current run and wait for canonical lease/process readback before redriving.";
  }
  return null;
}
