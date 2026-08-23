import type { ReactNode } from "react";
import { AlertTriangle, CheckCircle2, FileText, Loader2 } from "lucide-react";
import { Link } from "@tanstack/react-router";
import type { InterruptResult, RedriveResult, RunDetail, TaskCapsule } from "@/types/domain";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { Button } from "@/components/ui/controls";
import { OutcomeChip, RunnerBadge } from "@/components/ui/chips";
import { CapsuleChips } from "@/components/ui/capsule";
import { LivenessIndicator } from "@/components/ui/liveness";
import {
  isInterruptibleRun,
  isLiveHeaderRun,
  redriveDisabledReason,
} from "@/features/runs/detail/helpers";

/** State of the in-flight / last redrive, surfaced inline under the actions. */
export interface RetryState {
  pending: boolean;
  result: RedriveResult | null;
  error?: unknown;
}

export interface InterruptState {
  confirming: boolean;
  pending: boolean;
  awaitingReadback: boolean;
  result: InterruptResult | null;
  error?: unknown;
}

/**
 * Run-detail header (design §07): task id + serif title, the task capsule chips
 * (the canonical status badge), a runner/model/lane/state meta
 * line with liveness, and the run actions. Interrupt is destructive (danger)
 * and only enabled while the run is active. Retry maps to `tusker redrive` and
 * says so ("Redrive"): it is disabled with an inline explanation when the
 * canonical task status makes redrive meaningless (review/done), and its result
 * — a requeue or a refusal reason — is surfaced, never swallowed.
 */
export function RunHeader({
  run,
  capsule,
  onInterrupt,
  onRetry,
  retry,
  interrupt,
  waitingForDaemonReason,
}: {
  run: RunDetail;
  capsule?: TaskCapsule;
  onInterrupt: () => void;
  onRetry: () => void;
  retry: RetryState;
  interrupt: InterruptState;
  waitingForDaemonReason?: string | null;
}) {
  const live = isLiveHeaderRun(run);
  const active = isInterruptibleRun(run);
  const interruptBusy = interrupt.confirming || interrupt.pending || interrupt.awaitingReadback;
  const actionBusy = interruptBusy || retry.pending;
  // Canonical task status drives whether redrive is allowed — never the run row.
  const disabledReason = redriveDisabledReason(capsule?.status, run, waitingForDaemonReason);
  const redriveDisabled = disabledReason !== null || actionBusy;
  return (
    <header className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div className="min-w-0">
        <Mono className="text-[11.5px] text-faint">{run.taskId}</Mono>
        <h1 className="mt-1 font-serif text-[26px] font-semibold leading-[1.05] tracking-[-0.02em] text-ink sm:text-[28px]">
          {run.taskTitle}
        </h1>

        {capsule && (
          <div className="mt-3">
            <CapsuleChips capsule={capsule} show={["status", "priority", "risk"]} />
          </div>
        )}

        <div className="mt-3 flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-[11px]">
          <RunnerBadge runner={run.runner} />
          <Mono className="text-muted">{run.model}</Mono>
          <span className="text-fainter">·</span>
          <Mono className="text-muted">lane {run.lane}</Mono>
          <span className="text-fainter">·</span>
          <Mono className="text-fainter">run</Mono>
          <OutcomeChip outcome={run.outcome} />
          {waitingForDaemonReason && (
            <Mono className="font-semibold text-warn" title={waitingForDaemonReason}>
              Waiting for daemon
            </Mono>
          )}
          {live && (
            <>
              <LivenessIndicator liveness={run.liveness} sinceSec={run.sinceLastEventSec} />
              <span className="text-fainter">·</span>
            </>
          )}
          {!waitingForDaemonReason && (
            <Mono className={cn(run.leaseState === "expired" ? "text-fail" : "text-muted")}>
              lease {run.leaseState}
            </Mono>
          )}
        </div>
      </div>

      <div className="flex flex-none flex-col items-stretch gap-2 sm:items-end">
        <div className="flex items-center gap-2.5">
          <Link
            to="/p/$projectId/docs"
            params={{ projectId: run.projectId }}
            search={{ path: run.taskId }}
            className="inline-flex items-center gap-1.5 rounded-lg border border-line bg-raised px-3.5 py-1.5 text-[12.5px] font-semibold text-ink-soft transition-colors hover:border-line-soft hover:bg-hover"
          >
            <FileText size={13} strokeWidth={2} />
            View ticket
          </Link>
          <Button variant="danger" onClick={onInterrupt} disabled={!active || actionBusy}>
            {interrupt.pending || interrupt.awaitingReadback ? "Interrupting…" : "Interrupt"}
          </Button>
          <Button
            variant="primary"
            onClick={onRetry}
            disabled={redriveDisabled}
            title={
              disabledReason ??
              "Redrive: reset the attempt window and requeue (tusker redrive)"
            }
          >
            {retry.pending ? "Redriving…" : "Redrive"}
          </Button>
        </div>
        <InterruptFeedback interrupt={interrupt} />
        <RedriveFeedback disabledReason={disabledReason} retry={retry} />
      </div>
    </header>
  );
}

/**
 * Inline feedback for the redrive control. Shows, in priority order: why redrive
 * is disabled (review/done → point to review/land), an in-flight state, or the
 * last result — a requeue confirmation or the refusal reason. The refusal is
 * never swallowed.
 */
function RedriveFeedback({
  disabledReason,
  retry,
}: {
  disabledReason: string | null;
  retry: RetryState;
}) {
  if (disabledReason) {
    return (
      <FeedbackLine tone="info">
        <AlertTriangle size={12.5} strokeWidth={2.2} className="mt-px flex-none" />
        <span>{disabledReason}</span>
      </FeedbackLine>
    );
  }
  if (retry.pending) {
    return <FeedbackLine tone="muted">Requesting redrive…</FeedbackLine>;
  }
  if (retry.error) {
    return (
      <FeedbackLine tone="warn">
        <AlertTriangle size={12.5} strokeWidth={2.2} className="mt-px flex-none" />
        <span>{errorMessage(retry.error)}</span>
      </FeedbackLine>
    );
  }
  if (retry.result) {
    const refused = retry.result.refused || !retry.result.ok;
    return (
      <FeedbackLine tone={refused ? "warn" : "pass"}>
        {refused ? (
          <AlertTriangle size={12.5} strokeWidth={2.2} className="mt-px flex-none" />
        ) : (
          <CheckCircle2 size={12.5} strokeWidth={2.2} className="mt-px flex-none" />
        )}
        <span>{retry.result.reason}</span>
      </FeedbackLine>
    );
  }
  return null;
}

function InterruptFeedback({ interrupt }: { interrupt: InterruptState }) {
  if (interrupt.confirming) {
    return <FeedbackLine tone="muted">Waiting for interrupt confirmation…</FeedbackLine>;
  }
  if (interrupt.pending) {
    return (
      <FeedbackLine tone="muted">
        <Loader2 size={12.5} className="mt-px flex-none animate-spin" />
        <span>Sending interrupt…</span>
      </FeedbackLine>
    );
  }
  if (interrupt.awaitingReadback) {
    return (
      <FeedbackLine tone="info">
        <Loader2 size={12.5} className="mt-px flex-none animate-spin" />
        <span>Interrupt accepted; waiting for canonical lease/process readback…</span>
      </FeedbackLine>
    );
  }
  if (interrupt.error) {
    return (
      <FeedbackLine tone="warn">
        <AlertTriangle size={12.5} strokeWidth={2.2} className="mt-px flex-none" />
        <span>{errorMessage(interrupt.error)}</span>
      </FeedbackLine>
    );
  }
  if (!interrupt.result) return null;
  const refused = interrupt.result.refused || !interrupt.result.ok;
  return (
    <FeedbackLine tone={refused ? "warn" : "pass"}>
      {refused ? (
        <AlertTriangle size={12.5} strokeWidth={2.2} className="mt-px flex-none" />
      ) : (
        <CheckCircle2 size={12.5} strokeWidth={2.2} className="mt-px flex-none" />
      )}
      <span>{interrupt.result.reason}</span>
    </FeedbackLine>
  );
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function FeedbackLine({
  tone,
  children,
}: {
  tone: "info" | "warn" | "pass" | "muted";
  children: ReactNode;
}) {
  const toneClass =
    tone === "warn"
      ? "text-warn"
      : tone === "pass"
        ? "text-pass"
        : tone === "info"
          ? "text-info"
          : "text-muted";
  return (
    <div
      className={cn(
        "flex max-w-[280px] items-start gap-1.5 text-[11.5px] leading-snug sm:text-right",
        toneClass,
      )}
    >
      {children}
    </div>
  );
}
