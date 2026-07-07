/*
  Tone system — the single place semantic meaning maps to color (packet §6:
  "color carries meaning and little else"). Every chip, dot, and indicator reads
  its classes from here so a gate kind / risk tier / run outcome has ONE hue
  everywhere it appears.
*/

import type {
  GateKind,
  Liveness,
  Priority,
  ProofStatus,
  Readiness,
  Risk,
  RunOutcome,
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

export const outcomeTone: Record<RunOutcome, Tone> = {
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

export const outcomeLabel: Record<RunOutcome, string> = {
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
