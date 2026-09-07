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

import type { RunDetail, TaskDetail } from "@/types/domain";

export type Transport = "ACP" | "CLI exec";

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
  label: string;
  tone: StageTone;
  /** True when a live run fact refines the durable task status. */
  live: boolean;
}

/**
 * The task's actual stage: durable lifecycle first, refined — never replaced —
 * by a live run fact. Worker success is not accepted completion: a succeeded
 * run on an unreviewed task still reads as checking, not delivered.
 */
export function actualStage(task: TaskDetail, run: RunDetail | null): ActualStage {
  const liveOutcome = run?.outcome ?? null;
  const liveRunning = liveOutcome === "running";
  switch (task.status) {
    case "done":
      return { label: "Delivered", tone: "pass", live: false };
    case "review":
      return {
        label: liveRunning ? "Checking the work · run active" : "Checking the work",
        tone: "info",
        live: liveRunning,
      };
    case "blocked":
      return { label: "Blocked", tone: "fail", live: false };
    case "in_progress":
      return {
        label: liveRunning ? "Building now" : "Building",
        tone: "info",
        live: liveRunning,
      };
    case "ready":
      return task.readiness === "ready"
        ? { label: "Ready to start", tone: "neutral", live: false }
        : { label: "Waiting for prerequisites", tone: "warn", live: false };
    default:
      return { label: "Planned", tone: "neutral", live: false };
  }
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
  return `${display.provider} · ${display.model} · ${display.transport} · ${display.stage}`;
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
  if (!delivery.summary) return null;
  if (!ACCEPTED_DELIVERY.has(delivery.proofStatus.trim().toLowerCase())) return null;
  return delivery;
}
