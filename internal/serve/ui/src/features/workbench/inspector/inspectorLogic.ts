/*
  WUX-T-0006 — contextual task inspection: pure selection/identity logic.

  These helpers are UI-framework-free so the focused behavior checks can
  exercise them directly. The component (TaskInspector.tsx) must call them
  rather than re-deriving the same facts inline.

  Three invariants matter here:

  1. Late responses never win. The caller fetches per selection; when the
     user moves A -> B while A's fetch is still in flight, the late A
     payload must not render inside B's panel. resolveVisibleTask returns
     the task only when its id matches the current selection.
  2. Identity is stage-specific and observed-only. A past worker's model is
     never presented as the current reviewer's model: anything unobserved
     or missing renders as Unavailable, never as a guess.
  3. Evidence presence is not acceptance. Uploaded artifacts stay
     "available"; only a run delivery whose proofStatus records acceptance
     becomes an "accepted result".
*/

import type { Attempt, ProofInvalidation, RunDetail, RunEvent, TaskDetail, VerificationRow } from "@/types/domain";
import { harnessLabel } from "@/lib/harness";

export type Transport = string;

/** Stage-specific execution identity supplied by the host (integration). */
export interface InspectorExecutionIdentity {
  provider?: string;
  model?: string;
  transport?: Transport;
  stage?: string;
  observed: boolean;
}

const INVALID_PROOF_OUTCOMES = new Set(["interrupted", "parked-budget", "parked-no-progress", "failed", "stale"]);

function runInvalidatesProof(run: RunDetail | null): boolean {
  return Boolean(run && (INVALID_PROOF_OUTCOMES.has(run.outcome) || (run.outcome === "running" && run.liveness !== "fresh")));
}

const PROOF_INVALIDATION_SUMMARY: Record<ProofInvalidation["kind"], string> = {
  missing: "proof not recorded",
  failed: "proof failed",
  unavailable: "proof unavailable",
  changed: "proof out of date",
};

export function proofInvalidationSummary(invalidation: ProofInvalidation | undefined): string | undefined {
  return invalidation ? PROOF_INVALIDATION_SUMMARY[invalidation.kind] : undefined;
}

/**
 * Race guard for rapid selection. Returns the task only when it is the
 * currently selected one; a late response for a previous selection resolves
 * to null so the caller renders loading instead of another task's data.
 */
export function resolveVisibleTask(
  task: TaskDetail | null,
  selectedTaskId: string | null,
): TaskDetail | null {
  if (!task || !selectedTaskId) return null;
  return task.id === selectedTaskId ? task : null;
}

/** The run read must belong to the selected task as well as the task payload. */
export function resolveVisibleRun(
  run: RunDetail | null,
  selectedTaskId: string | null,
): RunDetail | null {
  if (!run || !selectedTaskId || run.taskId !== selectedTaskId) return null;
  return run;
}

export type IdentityDisplay =
  | { state: "unavailable" }
  | {
      state: "identity";
      provider: string;
      model: string;
      transport: string;
      stage: string;
    };

const UNAVAILABLE = "Unavailable";

/**
 * Stage-specific identity with no guessing. Missing values, an unobserved
 * stage, or a missing identity object all render as Unavailable — a past
 * worker's model is never shown as the current reviewer's model.
 */
export function identityDisplay(
  identity: InspectorExecutionIdentity | undefined,
): IdentityDisplay {
  if (!identity || !identity.observed) return { state: "unavailable" };
  const provider = identity.provider?.trim() || "";
  const model = identity.model?.trim() || "";
  const transport = identity.transport?.trim() || "";
  const stage = identity.stage?.trim() || "";
  if (!provider || !model || !transport || !stage) return { state: "unavailable" };
  return { state: "identity", provider, model, transport, stage };
}

export function identitySummary(display: IdentityDisplay): string {
  if (display.state === "unavailable") return UNAVAILABLE;
  return `${display.provider} · ${display.model} · ${harnessLabel(display.transport)} · ${display.stage}`;
}

export function observedRunIdentity(run: RunDetail | null): InspectorExecutionIdentity | undefined {
  if (!run?.runnerProfile || !run.model || !run.runnerHarness) return undefined;
  return { provider: run.runnerProfile, model: run.model, transport: run.runnerHarness, stage: run.lane, observed: true };
}

/** Choose the newest usable event without trusting array order. */
export function latestRunEvent(run: RunDetail | null): RunEvent | null {
  if (!run?.events?.length) return null;
  return run.events.reduce<RunEvent | null>((latest, event) => {
    if (!latest) return event;
    const latestMs = Date.parse(latest.ts);
    const eventMs = Date.parse(event.ts);
    return Number.isNaN(latestMs) || (!Number.isNaN(eventMs) && eventMs > latestMs) ? event : latest;
  }, null);
}

/**
 * Pick the attempt represented by the run summary, not merely the last row in
 * a task snapshot. The runtime gives us a stable active-attempt id; the lane
 * and timestamp fallbacks keep older servers useful without merging a worker
 * row into a later reviewer row.
 */
export function currentAttemptRecord(run: RunDetail | null): Attempt | null {
  if (!run?.attempts?.length) return null;
  if (run.activeAttemptId) {
    const active = run.attempts.find((attempt) => attempt.id === run.activeAttemptId);
    if (active) return active;
  }
  const ordered = [...run.attempts].sort((left, right) => {
    const leftTime = Date.parse(left.startedAt);
    const rightTime = Date.parse(right.startedAt);
    if (!Number.isNaN(leftTime) && !Number.isNaN(rightTime) && leftTime !== rightTime) return leftTime - rightTime;
    if (!Number.isNaN(leftTime) && Number.isNaN(rightTime)) return 1;
    if (Number.isNaN(leftTime) && !Number.isNaN(rightTime)) return -1;
    return left.n - right.n;
  });
  const laneMatch = ordered.filter((attempt) => !attempt.lane || attempt.lane === run.lane);
  return laneMatch.at(-1) ?? ordered.at(-1) ?? null;
}

/** Historical attempts are canonical attempt records minus the current one. */
export function historicalAttemptRecords(run: RunDetail | null): Attempt[] {
  if (!run?.attempts?.length) return [];
  const current = currentAttemptRecord(run);
  if (!current) return [...run.attempts];
  return run.attempts.filter((attempt) => {
    if (current.id && attempt.id) return attempt.id !== current.id;
    return attempt !== current;
  });
}

/** Render intent only when it looks authored; otherwise expose the title as a title. */
export function visibleTaskIntent(task: Pick<TaskDetail, "intent" | "title">): { markdown: string; fromTitle: boolean } {
  const intent = typeof task.intent === "string" ? task.intent.trim() : "";
  const placeholder = /^(no intent recorded\.?|intent not supplied\.?|task contract and objective proof\.?)$/i.test(intent);
  if (intent && !placeholder) return { markdown: intent, fromTitle: false };
  return { markdown: `Task title: ${task.title}`, fromTitle: true };
}

/** A failed/stale current attempt cannot inherit a pass from an older attempt. */
export function proofStatusForCurrentAttempt(
  status: VerificationRow["result"],
  run: RunDetail | null,
): VerificationRow["result"] | "not-current" {
  return status === "pass" && runInvalidatesProof(run) ? "not-current" : status;
}

export function laneLabel(lane: RunDetail["lane"] | Attempt["lane"] | undefined): string {
  return lane === "review" ? "Independent review" : "Implementation";
}

export function outcomeLabel(run: Pick<RunDetail, "lane" | "outcome">): string {
  switch (run.outcome) {
    case "running": return `${laneLabel(run.lane)} active`;
    case "succeeded": return `${laneLabel(run.lane)} finished`;
    case "failed": return `${laneLabel(run.lane)} failed`;
    case "stale": return `${laneLabel(run.lane)} activity unavailable`;
    case "interrupted": return `${laneLabel(run.lane)} interrupted`;
    default: return `${laneLabel(run.lane)} ${run.outcome.replaceAll("-", " ")}`;
  }
}

export { UNAVAILABLE as IDENTITY_UNAVAILABLE };

/** Acceptance rows and verification checks that are currently failing. */
export function failingRows(task: TaskDetail): {
  acceptance: TaskDetail["acceptance"];
  verification: TaskDetail["verification"];
} {
  return {
    acceptance: task.acceptance.filter((row) => row.proof === "fail"),
    verification: task.verification.filter((row) => row.result === "fail"),
  };
}

/** Proof statuses that record an accepted run delivery. */
const ACCEPTED_DELIVERY = new Set(["accepted", "accept", "pass", "verified"]);

/**
 * The run's accepted result, if its delivery proof records acceptance.
 * Anything else — pending, missing, failed — leaves the accepted slot empty
 * so unreviewed evidence is never stamped as a verified result.
 */
export function acceptedDelivery(run: RunDetail | null): RunDetail["delivery"] | null {
  const delivery = run?.delivery;
  if (!delivery) return null;
  if (!run || runInvalidatesProof(run)) return null;
  if (!delivery.summary) return null;
  if (!ACCEPTED_DELIVERY.has(delivery.proofStatus.trim().toLowerCase())) return null;
  return delivery;
}
