/*
  Project Runs board — table header + row components.

  Active rows link to run detail and read alarming when a "running" process has
  gone silent (amber → red). Recent rows are quiet, settled, and end in an
  outcome chip — styled distinctly (muted wash, "terminal" tag, attempt count)
  so a finished 15-attempt churn row can never be mistaken for a live runner
  (SRV-T-0015 A3). The header is labeled and shared by both tables (A2).
*/

import { Link } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { OutcomeChip, RunnerBadge } from "@/components/ui/chips";
import { LivenessIndicator } from "@/components/ui/liveness";
import { Skeleton } from "@/components/ui/states";
import { compactNumber, duration } from "@/lib/time";
import type { RunSummary } from "@/types/domain";
import {
  RUNS_GRID,
  laneTextClass,
  leaseTextClass,
  livenessRowClass,
  livenessTextClass,
  tokenTotal,
} from "@/features/runs/board/helpers";

const ROW_BORDER = "border-b border-line-soft last:border-b-0";

/** Column headings, shared by the active and recent tables (design: mono micro-caps). */
export function RunsTableHeader() {
  return (
    <div
      className={cn(
        RUNS_GRID,
        "border-b border-line bg-panel px-4 py-[9px] font-mono text-[9px] uppercase tracking-[0.08em] text-fainter",
      )}
    >
      <span>Task</span>
      <span>Runner</span>
      <span>Lane</span>
      <span>Lease</span>
      <span>Tokens</span>
      <span>State</span>
    </div>
  );
}

/** Task id over its (truncated) title, with an optional churn subtitle. */
function TaskCell({
  run,
  titleClass,
  subtitle,
}: {
  run: RunSummary;
  titleClass: string;
  subtitle?: string;
}) {
  return (
    <span className="min-w-0">
      <Mono className="block text-[10px] text-faint">{run.taskId}</Mono>
      <span className={cn("block truncate text-[13px] font-medium", titleClass)}>
        {run.taskTitle}
      </span>
      {subtitle && <Mono className="block text-[9px] text-fainter">{subtitle}</Mono>}
    </span>
  );
}

/** Runner badge over the model id (both mono). */
function RunnerCell({ run }: { run: RunSummary }) {
  return (
    <span className="flex min-w-0 flex-col items-start gap-1">
      <RunnerBadge runner={run.runner} />
      <Mono className="block w-full truncate text-[9.5px] text-faint" title={run.model}>
        {run.model}
      </Mono>
    </span>
  );
}

/**
 * Active run row. Links to run detail; the State column pairs a prominent
 * elapsed clock (coloured by liveness) with the shared <LivenessIndicator>
 * (dot + time-since-last-event) so a dead process reads as alarming. Lane and
 * lease_state are their own labeled columns.
 */
export function ActiveRunRow({ run }: { run: RunSummary }) {
  const dead = run.liveness === "dead";
  return (
    <Link
      to="/p/$projectId/runs/$taskId"
      params={{ projectId: run.projectId, taskId: run.taskId }}
      className={cn(
        RUNS_GRID,
        "items-center px-4 py-3 text-left transition-colors",
        ROW_BORDER,
        livenessRowClass(run.liveness),
      )}
    >
      <TaskCell run={run} titleClass="text-ink" />
      <RunnerCell run={run} />
      <Mono className={cn("self-center text-[11px]", laneTextClass(run.lane))}>{run.lane}</Mono>
      <Mono className={cn("self-center text-[10px]", leaseTextClass(run.leaseState))}>
        {run.leaseState}
      </Mono>
      <Mono className="self-center text-[11px] text-ink-soft">
        {compactNumber(tokenTotal(run))}
      </Mono>
      <span className="flex flex-col items-start gap-1">
        <Mono
          className={cn(
            "text-[13px] leading-none",
            livenessTextClass(run.liveness),
            dead && "font-semibold",
          )}
        >
          {duration(run.elapsedSec)}
        </Mono>
        <LivenessIndicator liveness={run.liveness} sinceSec={run.sinceLastEventSec} />
      </span>
    </Link>
  );
}

/**
 * Recent (finished) run row — quiet, non-interactive, visually settled. The
 * muted wash, the "terminal" tag, and the attempt-count subtitle make a settled
 * churn row unmistakably distinct from a live run (SRV-T-0015 A3). Ends in the
 * outcome chip.
 */
export function RecentRunRow({ run }: { run: RunSummary }) {
  const churn = run.attemptCount > 1 ? `${run.attemptCount} attempts` : undefined;
  return (
    <div
      className={cn(RUNS_GRID, "items-center bg-panel/40 px-4 py-[11px] opacity-90", ROW_BORDER)}
    >
      <TaskCell run={run} titleClass="text-ink-soft" subtitle={churn} />
      <span className="justify-self-start">
        <RunnerBadge runner={run.runner} />
      </span>
      <Mono className="self-center text-[11px] text-faint">{run.lane}</Mono>
      <Mono className={cn("self-center text-[10px]", leaseTextClass(run.leaseState))}>
        {run.leaseState}
      </Mono>
      <Mono className="self-center text-[11px] text-muted">{compactNumber(tokenTotal(run))}</Mono>
      <span className="flex items-center gap-1.5 justify-self-start">
        <OutcomeChip outcome={run.outcome} />
        {run.terminal && (
          <Mono className="text-[8.5px] uppercase tracking-[0.08em] text-fainter">terminal</Mono>
        )}
      </span>
    </div>
  );
}

/** Skeleton table body while runs load (design: skeletons, never spinners). */
export function RunsBoardSkeleton() {
  return (
    <div className="overflow-hidden rounded-[10px] border border-line">
      {Array.from({ length: 4 }).map((_, i) => (
        <div key={i} className={cn(RUNS_GRID, "items-center px-4 py-3", ROW_BORDER)}>
          <div className="min-w-0 space-y-1.5">
            <Skeleton className="h-2 w-16" />
            <Skeleton className="h-3 w-3/4" />
          </div>
          <Skeleton className="h-4 w-16" />
          <Skeleton className="h-3 w-10" />
          <Skeleton className="h-3 w-10" />
          <Skeleton className="h-3 w-12" />
          <Skeleton className="h-4 w-20" />
        </div>
      ))}
    </div>
  );
}
