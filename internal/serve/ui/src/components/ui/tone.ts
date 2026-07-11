/*
  Tone system — the single place semantic meaning maps to color (packet §6:
  "color carries meaning and little else"). Every chip, dot, and indicator reads
  its classes from here so a gate kind / risk tier / run outcome has ONE hue
  everywhere it appears.
*/

import type {
  GateKind,
  KnownRunOutcome,
  Liveness,
  Priority,
  ProofStatus,
  Readiness,
  Risk,
  TaskStatus,
} from "@/types/domain";

export type Tone = "fail" | "pass" | "warn" | "info" | "accent" | "muted" | "neutral";

export interface ToneClasses {
  text: string;
  softBg: string;
  softText: string;
  dot: string;
  ring: string;
}

export const tone: Record<Tone, ToneClasses> = {
  fail: { text: "text-fail", softBg: "bg-fail-soft", softText: "text-fail", dot: "bg-fail", ring: "ring-fail/30" },
  pass: { text: "text-pass", softBg: "bg-pass-soft", softText: "text-pass", dot: "bg-pass", ring: "ring-pass/30" },
  warn: { text: "text-warn", softBg: "bg-warn-soft", softText: "text-warn", dot: "bg-warn", ring: "ring-warn/30" },
  info: { text: "text-info", softBg: "bg-info-soft", softText: "text-info", dot: "bg-info", ring: "ring-info/30" },
  accent: { text: "text-accent", softBg: "bg-accent-soft", softText: "text-accent", dot: "bg-accent", ring: "ring-accent/30" },
  muted: { text: "text-muted", softBg: "bg-hover", softText: "text-muted", dot: "bg-faint", ring: "ring-line" },
  neutral: { text: "text-faint", softBg: "bg-hover", softText: "text-faint", dot: "bg-fainter", ring: "ring-line" },
};

// ---- Semantic → tone maps ----

export const gateKindTone: Record<GateKind, Tone> = {
  clarify: "info",
  provision: "accent",
  "approve-spec": "warn",
  review: "pass",
  failed: "fail",
};

export const gateKindLabel: Record<GateKind, string> = {
  clarify: "Clarify",
  provision: "Provision",
  "approve-spec": "Approve spec",
  review: "Review",
  failed: "Failed",
};

export const statusTone: Record<TaskStatus, Tone> = {
  backlog: "neutral",
  ready: "info",
  in_progress: "accent",
  review: "warn",
  blocked: "fail",
  done: "pass",
};

export const statusLabel: Record<TaskStatus, string> = {
  backlog: "Backlog",
  ready: "Ready",
  in_progress: "In progress",
  review: "Review",
  blocked: "Blocked",
  done: "Done",
};

const extraStatusTone: Record<string, Tone> = {
  idea: "neutral",
  rework: "warn",
  cancelled: "muted",
  superseded: "muted",
};

const extraStatusLabel: Record<string, string> = {
  idea: "Idea",
  rework: "Rework",
  cancelled: "Cancelled",
  superseded: "Superseded",
};

export function statusToneOf(status: string): Tone {
  return (statusTone as Record<string, Tone>)[status] ?? extraStatusTone[status] ?? "neutral";
}

export function statusLabelOf(status: string): string {
  return (statusLabel as Record<string, string>)[status] ?? extraStatusLabel[status] ?? humanizeToken(status);
}

export const riskTone: Record<Risk, Tone> = {
  low: "muted",
  medium: "warn",
  high: "fail",
  critical: "fail",
};

export const priorityTone: Record<Priority, Tone> = {
  p0: "fail",
  p1: "warn",
  p2: "info",
  p3: "neutral",
};

export const readinessTone: Record<Readiness, Tone> = {
  ready: "pass",
  blocked_dependency: "warn",
  blocked_gate: "fail",
  draft: "neutral",
};

export const readinessLabel: Record<Readiness, string> = {
  ready: "Ready",
  blocked_dependency: "Blocked · dep",
  blocked_gate: "Blocked · gate",
  draft: "Draft",
};

const extraReadinessTone: Record<string, Tone> = {
  blocked_by_dependency: "warn",
  blocked_by_gate: "fail",
  waiting_on_review: "warn",
  waiting_on_human: "fail",
  waiting_on_ci: "warn",
  held: "neutral",
  done: "pass",
  cancelled: "muted",
  superseded: "muted",
};

const extraReadinessLabel: Record<string, string> = {
  blocked_by_dependency: "Blocked · dep",
  blocked_by_gate: "Blocked · gate",
  waiting_on_review: "Waiting · review",
  waiting_on_human: "Waiting · human",
  waiting_on_ci: "Waiting · CI",
  held: "Held",
  done: "Done",
  cancelled: "Cancelled",
  superseded: "Superseded",
};

export function readinessToneOf(readiness: string): Tone {
  return (readinessTone as Record<string, Tone>)[readiness] ?? extraReadinessTone[readiness] ?? "neutral";
}

export function readinessLabelOf(readiness: string): string {
  return (readinessLabel as Record<string, string>)[readiness] ?? extraReadinessLabel[readiness] ?? humanizeToken(readiness);
}

export const outcomeTone: Record<KnownRunOutcome, Tone> = {
  idle: "muted",
  running: "info",
  stale: "warn",
  succeeded: "pass",
  failed: "fail",
  interrupted: "warn",
  released: "muted",
  terminal: "muted",
  "retry-queued": "accent",
  "parked-no-progress": "warn",
  "parked-budget": "fail",
};

export const outcomeLabel: Record<KnownRunOutcome, string> = {
  idle: "Idle",
  running: "Running",
  stale: "Stale",
  succeeded: "Succeeded",
  failed: "Failed",
  interrupted: "Interrupted",
  released: "Released",
  terminal: "Terminal",
  "retry-queued": "Retry queued",
  "parked-no-progress": "Parked",
  "parked-budget": "Budget parked",
};

/**
 * Humanize an unknown enum token — "review-complete" / "awaiting_land" →
 * "Review complete" / "Awaiting land". Used as the fallback label so a run
 * outcome (or lease state) the API adds later still reads sensibly.
 */
export function humanizeToken(token: string): string {
  const cleaned = token.replace(/[_-]+/g, " ").trim();
  if (cleaned === "") return token;
  return cleaned.charAt(0).toUpperCase() + cleaned.slice(1);
}

/**
 * Tone for any run outcome, known or not. An open-enum value the daemon adds
 * later resolves to a neutral tone rather than an undefined class (packet §6:
 * color carries meaning; the absence of a known meaning is neutral, not blank).
 */
export function outcomeToneOf(outcome: string): Tone {
  return (outcomeTone as Record<string, Tone>)[outcome] ?? "neutral";
}

/** Label for any run outcome, known or not (falls back to a humanized token). */
export function outcomeLabelOf(outcome: string): string {
  return (outcomeLabel as Record<string, string>)[outcome] ?? humanizeToken(outcome);
}

export const livenessTone: Record<Liveness, Tone> = {
  fresh: "pass",
  stale: "warn",
  dead: "fail",
};

export const proofTone: Record<ProofStatus, Tone> = {
  pending: "neutral",
  pass: "pass",
  fail: "fail",
};
