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

import type { Attempt, ProofInvalidation, RunDetail, RunEvent, TaskDetail, VerificationRow, WaveReviewMember } from "@/types/domain";
import { harnessLabel } from "@/lib/harness";
import { RECOVERY_STATE_LABEL } from "@/lib/recovery";
import { DISPLAY_STATE_LABEL, type FlowDisplayState } from "../flow/flowGraph";

export type Transport = string;

/** Stage-specific execution identity supplied by the host (integration). */
export interface InspectorExecutionIdentity {
  provider?: string;
  model?: string;
  transport?: Transport;
  stage?: string;
  observed: boolean;
}

export type StageTone = "neutral" | "info" | "pass" | "warn" | "fail";

export interface ActualStage {
  /** Detailed stage phrase; the chip shows DISPLAY_STATE_LABEL[state]. */
  label: string;
  /** The unified Work status shared with graph, board, and wave list. */
  state: FlowDisplayState;
  tone: StageTone;
  /** True when a live run fact refines the durable task status. */
  live: boolean;
}

/**
 * The wave review is the shared status authority for member tasks. Keep this
 * mapping in the inspector's pure logic so a stale task/run read cannot paint
 * over a phase already shown by the wave header, list, or dependency graph.
 */
export function stageFromWaveReviewMember(member: WaveReviewMember | undefined): ActualStage | undefined {
  if (!member) return undefined;
  if (member.phase === "completed" || member.state === "completed") {
    return { label: "Completed", state: "completed", tone: "pass", live: false };
  }
  if (member.phase === "proof_blocked") {
    return { label: "Verification required", state: "proof_blocked", tone: "warn", live: false };
  }
  if (member.phase === "outcome_unknown") {
    return { label: RECOVERY_STATE_LABEL, state: "unknown", tone: "warn", live: false };
  }
  if (member.phase === "failed") {
    return { label: "Failed", state: "failed", tone: "fail", live: false };
  }
  if (member.phase === "reviewing") {
    return { label: "Reviewing", state: "reviewing", tone: "info", live: true };
  }
  if (member.phase === "awaiting_review" || member.completionReported || member.waitingReason?.includes("independent review")) {
    return { label: "Awaiting review", state: "awaiting_review", tone: "info", live: false };
  }
  if (member.phase === "executing" || member.state === "running") {
    return { label: "Executing", state: "executing", tone: "info", live: true };
  }
  if (member.state === "cancelled") {
    return { label: "Cancelled", state: "cancelled", tone: "neutral", live: false };
  }
  if (member.state === "blocked") {
    return { label: "Blocked", state: "blocked", tone: "fail", live: false };
  }
  if (member.state === "ready") {
    return { label: "Ready", state: "ready", tone: "neutral", live: false };
  }
  if (member.state === "waiting") {
    return { label: "Queued", state: "queued", tone: "neutral", live: false };
  }
  return undefined;
}

const FAILED_ATTEMPT_OUTCOMES = new Set([
  "interrupted",
  "parked-budget",
  "parked-no-progress",
  "failed",
]);
const INVALID_PROOF_OUTCOMES = new Set([...FAILED_ATTEMPT_OUTCOMES, "stale"]);

function runForTask(task: TaskDetail, run: RunDetail | null): RunDetail | null {
  return run?.taskId === task.id ? run : null;
}

function runFailed(run: RunDetail | null): boolean {
  return Boolean(run && FAILED_ATTEMPT_OUTCOMES.has(run.outcome));
}

function runInvalidatesProof(run: RunDetail | null): boolean {
  return Boolean(run && (INVALID_PROOF_OUTCOMES.has(run.outcome) || (run.outcome === "running" && run.liveness !== "fresh")));
}

function runActive(run: RunDetail | null): boolean {
  return Boolean(run && run.outcome === "running" && run.liveness === "fresh");
}

/**
 * The task's actual stage: durable lifecycle first, refined — never replaced —
 * by a live run fact. Worker success is not accepted completion: a succeeded
 * run on an unreviewed task still reads as checking, not delivered.
 */
export function actualStage(task: TaskDetail, run: RunDetail | null, reviewMember?: WaveReviewMember): ActualStage {
  const reviewStage = stageFromWaveReviewMember(reviewMember);
  if (reviewStage) return reviewStage;
  const currentRun = runForTask(task, run);
  const liveOutcome = currentRun?.outcome ?? null;
  const liveRunning = runActive(currentRun);
  if (liveOutcome === "parked-no-progress") {
    return { label: "Stopped — no progress", state: "failed", tone: "warn", live: false };
  }
  if (task.status === "done") {
    return { label: "Delivered", state: "completed", tone: "pass", live: false };
  }
  if (task.status === "blocked") {
    return { label: "Blocked", state: "blocked", tone: "fail", live: false };
  }

  // The observed lane wins when the durable task projection is one update
  // behind. A successful worker run is never presented as an active reviewer.
  if (currentRun?.lane === "review") {
    if (liveRunning) return { label: "Reviewing now", state: "reviewing", tone: "info", live: true };
    if (runFailed(currentRun)) return { label: "Review failed — action needed", state: "failed", tone: "fail", live: false };
    if (liveOutcome === "succeeded" || liveOutcome === "terminal") {
      return { label: "Review complete — delivery pending", state: "awaiting_review", tone: "info", live: false };
    }
    if (liveOutcome) return { label: "Review activity unavailable", state: "reviewing", tone: "warn", live: false };
  }
	if (currentRun?.lane === "execute") {
		if (liveRunning) return { label: "Building now", state: "executing", tone: "info", live: true };
		if (runFailed(currentRun)) return currentRun.attemptCount === 0
			? { label: "Couldn’t start — action needed", state: "failed", tone: "warn", live: false }
			: { label: "Implementation failed — action needed", state: "failed", tone: "fail", live: false };
    if (liveOutcome === "stale" || (liveOutcome === "running" && currentRun.liveness !== "fresh")) {
      return { label: "Implementation activity unavailable", state: "unknown", tone: "warn", live: false };
    }
    if (liveOutcome === "succeeded" || liveOutcome === "terminal") {
      return { label: "Checking verification · waiting for independent review", state: "awaiting_review", tone: "info", live: false };
    }
  }

  switch (task.status) {
    case "review":
      // Keep the wording familiar to existing users while saying exactly what
      // is happening: the worker has finished and an independent review is next.
      return { label: "Checking verification · waiting for independent review", state: "awaiting_review", tone: "info", live: false };
    case "in_progress":
      return {
        label: "Implementation in progress",
        state: "executing",
        tone: "info",
        live: false,
      };
    case "ready":
      return task.readiness === "ready"
        ? { label: "Ready to start", state: "ready", tone: "neutral", live: false }
        : { label: "Waiting for prerequisites", state: "blocked", tone: "warn", live: false };
    default:
      return { label: "Planned", state: "backlog", tone: "neutral", live: false };
  }
}

/** Plain-language action shown beside the stage, without exposing raw states. */
export function nextActionForStage(task: TaskDetail, run: RunDetail | null, stage = actualStage(task, run)): string {
  if (stage.label === RECOVERY_STATE_LABEL) {
    return "Verify and preserve existing work, then continue what remains.";
  }
  if (stage.label === "Delivered" || stage.label === "Completed") {
    return acceptedDelivery(run)
      ? "No action needed. Review the accepted result below."
      : "Delivery is recorded, but accepted proof is unavailable.";
  }
  if (stage.label === "Failed" || stage.label.includes("failed") || stage.label.includes("Stopped")) {
    return "Open the latest run details to inspect the failure before retrying.";
  }
  if (stage.label === "Verification required") return "Run the required verification before treating this task as complete.";
  if (stage.label === "Reviewing" || stage.label.includes("Reviewing now")) return "Watch the independent reviewer; its latest activity is shown below.";
  if (stage.label === "Awaiting review" || stage.label.includes("waiting for independent review")) {
    return task.humanActions?.length || task.humanAction
      ? "Complete the requested review decision below."
      : "Review the implementation, then record the outcome.";
  }
  if (stage.label === "Executing") return "Watch the current attempt; its latest event is shown below.";
  if (stage.label === "Cancelled") return "Re-authorize this task only after confirming it should run again.";
  if (stage.label === "Queued") return "Wait for the authorized wave to pick up this task.";
  if (stage.label === "Ready") return "Start this task when its prerequisites and routes are ready.";
  if (stage.label.includes("delivery pending")) return "Review the delivery evidence before marking it delivered.";
  if (stage.label.includes("prerequisites")) return "Wait until the listed prerequisites are complete.";
  if (stage.label.includes("Blocked") || task.deps.some((dependency) => dependency.status !== "done")) {
    return "Resolve the listed prerequisites before starting.";
  }
  if (stage.live) return "Watch the current attempt; its latest event is shown below.";
  if (stage.label.includes("unavailable")) return "Open run details to see whether activity can be recovered.";
  return "Start this task when its prerequisites and routes are ready.";
}

const DEPENDENCY_WAIT_STATES = new Set<FlowDisplayState>(["queued", "blocked", "backlog", "ready"]);

/**
 * The one short phrase beside the drawer's status chip. Dependency ids appear
 * here once; proof explanations belong in the Proof section, not above the fold.
 */
export function stagePhrase(task: TaskDetail, run: RunDetail | null, stage: ActualStage, reviewMember: WaveReviewMember | undefined, hasDecision: boolean): string {
  if (reviewMember?.phase === "failed" && reviewMember.waitingReason) return reviewMember.waitingReason;
  if (reviewMember?.phase === "proof_blocked") return stage.label;
  if (hasDecision && !reviewMember?.recovery) return "Waiting on you";
  const pending = task.deps.filter((dep) => dep.status !== "done").map((dep) => dep.id);
  if (pending.length > 0 && DEPENDENCY_WAIT_STATES.has(stage.state)) return `Waiting for ${pending.join(", ")}`;
  if (stage.label !== RECOVERY_STATE_LABEL && stage.label !== DISPLAY_STATE_LABEL[stage.state]) return stage.label;
  return nextActionForStage(task, run, stage);
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
