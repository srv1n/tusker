/*
  WUX-T-0006 — inspector preview fixtures. Clearly labeled sample data for
  the isolated preview only; nothing here is production or live data.
*/

import type { RunDetail, TaskDetail } from "@/types/domain";

const BASE_TASK = {
  projectId: "wux-preview",
  epicId: "WUX",
  epicTitle: "Work experience",
  priority: "p1",
  risk: "medium",
  hasGate: false,
  updatedAt: "2026-09-07T12:00:00Z",
  nonGoals: ["Do not modify backend execution.", "Do not arm any wave."],
  deps: [],
  gates: [],
  runHistory: [],
} as const;

export const readyTask: TaskDetail = {
  ...BASE_TASK,
  id: "WUX-T-0101",
  title: "Sample: review the wave graph performance",
  status: "review",
  readiness: "ready",
  hasGate: true,
  intent:
    "People can read a thirty-node wave graph without losing their place. " +
    "The graph keeps its viewport across selection and live updates.",
  acceptance: [
    { id: "A1", text: "Graph preserves viewport across selection", proof: "pass" },
    { id: "A2", text: "Live updates do not relayout stable topology", proof: "pending" },
  ],
  verification: [
    { id: "V1", command: "cd internal/serve/ui && bun test test/wux-flow.test.ts", result: "pending" },
  ],
  evidence: [
    { id: "E1", label: "Thirty-node render screenshot", kind: "image", ref: "docs/reports/wux/flow/desktop.png" },
    { id: "E2", label: "Render timing notes", kind: "file", ref: "docs/reports/wux/flow/report.md" },
  ],
  humanAction: {
    kind: "review",
    rawKind: "review",
    title: "Outcome review requested",
    action: "Read the thirty-node screenshot and confirm the labels stay legible.",
    whyAgentCannot: "Legibility is a subjective human judgment.",
    completionCondition: "Approve or send back with a note.",
    gateId: "WUX-G-0101",
    blockedTaskIds: ["WUX-T-0101"],
    covers: ["A1", "A2"],
    acceptance: [{ id: "A1", text: "Graph stays legible", proof: "pending" }],
  },
  runHistory: [],
};

export const readyRun: RunDetail = {
  taskId: readyTask.id,
  taskTitle: readyTask.title,
  projectId: "wux-preview",
  runner: "codex",
  model: "terra-preview-model",
  lane: "execute",
  leaseState: "held",
  outcome: "running",
  elapsedSec: 94,
  sinceLastEventSec: 3,
  liveness: "fresh",
  attemptCount: 2,
  workspacePath: "/tmp/wux-preview/WUX-T-0101",
  attempts: [],
  events: [
    { ts: "2026-09-07T12:01:00Z", kind: "started", text: "Attempt 2 started on a clean checkout.", level: "info" },
    { ts: "2026-09-07T12:02:34Z", kind: "progress", text: "Rendered thirty nodes; measuring pan latency.", level: "info" },
  ],
};

export const failedTask: TaskDetail = {
  ...BASE_TASK,
  id: "WUX-T-0102",
  title: "Sample: failing checks stay actionable",
  status: "in_progress",
  readiness: "ready",
  intent: "A failed attempt explains itself instead of going quiet.",
  acceptance: [{ id: "A1", text: "Failure names the first actionable check", proof: "fail" }],
  verification: [
    { id: "V1", command: "cd internal/serve/ui && bun test test/wux-inspector.test.ts", result: "fail", detail: "2 of 3 behavior checks pass." },
  ],
  evidence: [],
  deps: [{ id: "WUX-T-0101", title: "Sample: review the wave graph performance", status: "review" }],
  runHistory: [],
};

export const failedRun: RunDetail = {
  ...readyRun,
  taskId: failedTask.id,
  taskTitle: failedTask.title,
  outcome: "failed",
  liveness: "dead",
  error: "bun test exited 1: inspector late task response rejected.",
  events: [
    { ts: "2026-09-07T11:58:00Z", kind: "failed", text: "Attempt 1 failed on the race-guard check.", level: "error" },
  ],
};

export const acceptedTask: TaskDetail = {
  ...BASE_TASK,
  id: "WUX-T-0103",
  title: "Sample: accepted result is shown as accepted",
  status: "review",
  readiness: "ready",
  intent: "Reviewers see what was accepted without digging through logs.",
  acceptance: [{ id: "A1", text: "Accepted delivery renders first", proof: "pass" }],
  verification: [],
  evidence: [
    { id: "E9", label: "Inspector desktop screenshot", kind: "image", ref: "docs/reports/wux/inspector/desktop.png" },
  ],
  runHistory: [],
};

export const acceptedRun: RunDetail = {
  ...readyRun,
  taskId: acceptedTask.id,
  taskTitle: acceptedTask.title,
  outcome: "succeeded",
  liveness: "stale",
  delivery: {
    summary: "Inspector renders intent, stage, decision and evidence before metadata.",
    verification: "bun test test/wux-inspector.test.ts",
    proofStatus: "accepted",
    artifact: "docs/reports/wux/inspector/desktop.png",
  },
  events: [{ ts: "2026-09-07T11:00:00Z", kind: "delivered", text: "Delivery accepted by reviewer.", level: "info" }],
};

const LONG_PARAGRAPH =
  "This contract text is intentionally long so the preview proves the panel " +
  "keeps working when authors write more than a sentence. ".repeat(4);

export const longContractTask: TaskDetail = {
  ...BASE_TASK,
  id: "WUX-T-0104",
  title: "Sample: a very long contract with many rows and an extremely long title that must wrap instead of breaking the panel layout",
  status: "ready",
  readiness: "blocked_dependency",
  intent: [LONG_PARAGRAPH, LONG_PARAGRAPH, LONG_PARAGRAPH].join("\n\n"),
  acceptance: Array.from({ length: 12 }, (_, i) => ({
    id: `A${i + 1}`,
    text: `Acceptance row ${i + 1} with enough words to wrap onto a second line inside the disclosures`,
    proof: (i % 3 === 0 ? "pass" : "pending") as "pass" | "pending",
  })),
  verification: Array.from({ length: 6 }, (_, i) => ({
    id: `V${i + 1}`,
    command: `cd internal/serve/ui && bun test test/wux-part-${i + 1}.test.ts --with-a-very-long-flag-name-to-force-wrapping`,
    result: "pending" as const,
  })),
  evidence: Array.from({ length: 5 }, (_, i) => ({
    id: `EL${i + 1}`,
    label: `Screenshot ${i + 1} of the long-contract evidence set`,
    kind: "image" as const,
    ref: `docs/reports/wux/inspector/long-${i + 1}.png`,
  })),
  nonGoals: ["Do not paginate the disclosures.", "Do not truncate acceptance text."],
  deps: [
    { id: "WUX-T-0101", title: "Sample: review the wave graph performance", status: "review" },
    { id: "WUX-T-0102", title: "Sample: failing checks stay actionable", status: "in_progress" },
  ],
  runHistory: [],
};

export const unavailableTask: TaskDetail = {
  ...BASE_TASK,
  id: "WUX-T-0105",
  title: "Sample: everything missing stays unavailable",
  status: "backlog",
  readiness: "draft",
  intent: "",
  acceptance: [],
  verification: [],
  evidence: [],
  nonGoals: [],
  runHistory: [],
};

export const fixtureTasks: TaskDetail[] = [readyTask, failedTask, acceptedTask, longContractTask, unavailableTask];

export const fixtureRuns: Record<string, RunDetail> = {
  [readyTask.id]: readyRun,
  [failedTask.id]: failedRun,
  [acceptedTask.id]: acceptedRun,
};
