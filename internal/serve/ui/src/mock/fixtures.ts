/*
  Mock dataset for the Tusker Serve UI.

  Cross-referential: needs point to tasks that exist in `taskCapsules`, whose
  runs exist in `runs`, whose docs exist in `docs`. This keeps navigation
  coherent while the real JSON API is built. Timestamps are fixed ISO strings
  (relative "3m ago" formatting is computed at render time against a frozen
  `NOW`, so the mock reads sensibly without a live clock).

  Replace this module with real API calls — see api.ts / BACKEND-GAPS.md.
*/

import type {
  Attempt,
  DaemonStatus,
  DocContent,
  DocListEntry,
  EpicSummary,
  ProjectSummary,
  RunEvent,
  RunDetail,
  RunSummary,
  TaskCapsule,
  TaskDetail,
} from "@/types/domain";

/** Frozen "current time" the mock is authored against. */
export const NOW = new Date("2026-07-06T18:30:00Z");

// ----------------------------------------------------------------------------
// Projects
// ----------------------------------------------------------------------------

export const projects: ProjectSummary[] = [
  {
    id: "tusker",
    name: "tusker",
    needsCount: 3,
    activeRuns: 4,
    worstLiveness: "stale",
    daemonConnected: true,
  },
  {
    id: "rzn-browser",
    name: "rzn-browser",
    needsCount: 1,
    activeRuns: 1,
    worstLiveness: "fresh",
    daemonConnected: true,
  },
  {
    id: "headroom",
    name: "headroom",
    needsCount: 0,
    activeRuns: 0,
    worstLiveness: null,
    daemonConnected: true,
  },
];

export const daemon: DaemonStatus = {
  connected: true,
  addr: "localhost:7420",
  activeRuns: 5,
  queuedTasks: 11,
  parkedBudgetRuns: 0,
  budgetCircuit: { open: false },
};

// ----------------------------------------------------------------------------
// Needs-me queue
// ----------------------------------------------------------------------------

// "Needs me" is DERIVED, not stored — see src/features/inbox/deriveNeeds.ts and
// docs/design/serve-ui-supplement.md §3. `api.needs()` computes it from the board
// (taskCapsules), gates (taskDetails), and runs via the closed five-signal rule,
// so the panel can never be hand-flagged out of sync with reality. The previous
// hand-authored `needs` array — fabricated clarify/approve/review/provision/failed
// cards — has been removed: a hand-written needs list is exactly the drift the
// supplement forbids.

// ----------------------------------------------------------------------------
// Runs
// ----------------------------------------------------------------------------

export const runs: RunSummary[] = [
  {
    taskId: "AGX-T-0003",
    taskTitle: "Workflow runner lease + liveness protocol",
    projectId: "tusker",
    runner: "codex",
    model: "gpt-5.4-codex",
    lane: "execute",
    leaseState: "held",
    outcome: "running",
    elapsedSec: 512,
    sinceLastEventSec: 7,
    liveness: "fresh",
    tokens: { input: 184320, output: 22140 },
    attemptCount: 1,
  },
  {
    taskId: "CLN-T-0007",
    taskTitle: "Vault CAS write-path guard",
    projectId: "tusker",
    runner: "claude",
    model: "claude-opus-4-8",
    lane: "review",
    leaseState: "held",
    outcome: "running",
    elapsedSec: 143,
    sinceLastEventSec: 96,
    liveness: "stale",
    tokens: { input: 96010, output: 8820 },
    attemptCount: 1,
  },
  {
    taskId: "SRV-T-0004",
    taskTitle: "Embed serve SPA via go:embed",
    projectId: "tusker",
    runner: "codex",
    model: "gpt-5.4-codex",
    lane: "execute",
    leaseState: "expired",
    outcome: "running",
    elapsedSec: 331,
    sinceLastEventSec: 214,
    liveness: "dead",
    tokens: { input: 42110, output: 3010 },
    attemptCount: 2,
  },
  {
    taskId: "RUN-T-0012",
    taskTitle: "Wire ChatGPT Pro handoff credentials",
    projectId: "rzn-browser",
    runner: "claude",
    model: "claude-sonnet-5",
    lane: "execute",
    leaseState: "held",
    outcome: "running",
    elapsedSec: 58,
    sinceLastEventSec: 3,
    liveness: "fresh",
    tokens: { input: 12040, output: 990 },
    attemptCount: 1,
  },
  {
    taskId: "CLN-T-0005",
    taskTitle: "Prune generated events older than N days",
    projectId: "tusker",
    runner: "codex",
    model: "gpt-5.4-codex",
    lane: "execute",
    leaseState: "released",
    outcome: "succeeded",
    elapsedSec: 289,
    sinceLastEventSec: 640,
    liveness: "fresh",
    tokens: { input: 71200, output: 9130 },
    attemptCount: 1,
  },
  {
    taskId: "FBK-T-0004",
    taskTitle: "Feedback digest generator",
    projectId: "tusker",
    runner: "claude",
    model: "claude-opus-4-8",
    lane: "execute",
    leaseState: "released",
    outcome: "failed",
    elapsedSec: 122,
    sinceLastEventSec: 1980,
    liveness: "fresh",
    tokens: { input: 33900, output: 4120 },
    attemptCount: 3,
  },
  {
    taskId: "AGX-T-0001",
    taskTitle: "Daemon registry heartbeat",
    projectId: "tusker",
    runner: "codex",
    model: "gpt-5.4-codex",
    lane: "review",
    leaseState: "released",
    outcome: "interrupted",
    elapsedSec: 47,
    sinceLastEventSec: 5400,
    liveness: "fresh",
    tokens: { input: 8800, output: 610 },
    attemptCount: 1,
  },
];

export const runDetails: Record<string, RunDetail> = {
  "AGX-T-0003": {
    ...runs[0]!,
    workspacePath: "~/.tusker/workspaces/AGX-T-0003-a1c9",
    attempts: [
      {
        n: 1,
        outcome: "running",
        durationSec: 512,
        tokens: { input: 184320, output: 22140 },
        startedAt: "2026-07-06T18:21:28Z",
      },
    ],
    events: [
      { ts: "2026-07-06T18:21:28Z", kind: "lease.acquired", text: "lease held for AGX-T-0003 (ttl 120s)" },
      { ts: "2026-07-06T18:21:30Z", kind: "session.start", text: "codex gpt-5.4-codex · execute lane" },
      { ts: "2026-07-06T18:22:05Z", kind: "tool.call", text: "read internal/agent/lease.go" },
      { ts: "2026-07-06T18:24:41Z", kind: "tool.call", text: "edit internal/agent/lease.go (+38 −6)" },
      { ts: "2026-07-06T18:28:12Z", kind: "tool.call", text: "bun run build → ok" },
      { ts: "2026-07-06T18:29:53Z", kind: "turn.complete", text: "attempt 1 turn 6 · 22.1k out", level: "info" },
    ],
  },
  "SRV-T-0004": {
    ...runs[2]!,
    workspacePath: "~/.tusker/workspaces/SRV-T-0004-77b1",
    attempts: [
      {
        n: 1,
        outcome: "failed",
        durationSec: 96,
        tokens: { input: 21050, output: 1450 },
        startedAt: "2026-07-06T18:19:02Z",
      },
      {
        n: 2,
        outcome: "running",
        durationSec: 331,
        tokens: { input: 42110, output: 3010 },
        startedAt: "2026-07-06T18:22:40Z",
      },
    ],
    events: [
      { ts: "2026-07-06T18:22:40Z", kind: "lease.acquired", text: "lease held for SRV-T-0004 (ttl 120s)" },
      { ts: "2026-07-06T18:23:10Z", kind: "session.start", text: "codex gpt-5.4-codex · execute lane" },
      { ts: "2026-07-06T18:24:30Z", kind: "tool.call", text: "bun add vite → resolving…" },
      { ts: "2026-07-06T18:26:12Z", kind: "lease.warn", text: "no event for 90s — lease renewal at risk", level: "warn" },
      { ts: "2026-07-06T18:26:58Z", kind: "lease.expired", text: "lease expired; process still holding", level: "error" },
    ],
  },
};

/** ISO timestamp `secondsAgo` before the frozen NOW. */
function isoBefore(secondsAgo: number): string {
  return new Date(NOW.getTime() - Math.max(0, secondsAgo) * 1000).toISOString();
}

/**
 * Synthesize a plausible run detail from a run summary, so every run on the
 * board opens (the hand-authored `runDetails` only cover a couple of tasks).
 * TODO(api): GET /api/runs/:taskId returns this for real.
 */
export function synthRunDetail(s: RunSummary): RunDetail {
  const n = Math.max(1, s.attemptCount);
  const perDur = Math.max(1, Math.round(s.elapsedSec / n));
  const attempts: Attempt[] = Array.from({ length: n }, (_, i) => {
    const isLast = i === n - 1;
    return {
      n: i + 1,
      outcome: isLast ? s.outcome : "failed",
      durationSec: perDur,
      tokens: {
        input: Math.round(s.tokens.input / n),
        output: Math.round(s.tokens.output / n),
      },
      startedAt: isoBefore(s.elapsedSec + (n - 1 - i) * perDur),
    };
  });

  const events: RunEvent[] = [
    { ts: isoBefore(s.elapsedSec), kind: "lease.acquired", text: `lease held for ${s.taskId} (ttl 120s)` },
    { ts: isoBefore(s.elapsedSec - 2), kind: "session.start", text: `${s.runner} ${s.model} · ${s.lane} lane` },
    { ts: isoBefore(Math.round(s.elapsedSec * 0.6)), kind: "tool.call", text: "read internal/agent/lease.go" },
    { ts: isoBefore(Math.round(s.elapsedSec * 0.3)), kind: "tool.call", text: "edit internal/agent/lease.go (+18 −4)" },
  ];
  const tail: Record<RunSummary["outcome"], RunEvent> = {
    running: { ts: isoBefore(s.sinceLastEventSec), kind: "turn.complete", text: "turn complete · working" },
    succeeded: { ts: isoBefore(s.sinceLastEventSec), kind: "session.end", text: "attempt succeeded", level: "info" },
    failed: { ts: isoBefore(s.sinceLastEventSec), kind: "attempt.failed", text: "exit 1: run exhausted retries", level: "error" },
    interrupted: { ts: isoBefore(s.sinceLastEventSec), kind: "session.interrupt", text: "interrupted by operator", level: "warn" },
    "retry-queued": { ts: isoBefore(s.sinceLastEventSec), kind: "lease.released", text: "retry queued", level: "warn" },
    "parked-no-progress": { ts: isoBefore(s.sinceLastEventSec), kind: "lease.parked", text: "parked: no progress", level: "warn" },
    "parked-budget": { ts: isoBefore(s.sinceLastEventSec), kind: "lease.parked", text: "parked: budget exceeded", level: "error" },
  };
  events.push(tail[s.outcome]);

  return {
    ...s,
    workspacePath: `~/.tusker/workspaces/${s.taskId}`,
    attempts,
    events,
  };
}

/** Resolve run detail for a task: hand-authored fixture, else synthesized. */
export function runDetailFor(taskId: string): RunDetail | undefined {
  const explicit = runDetails[taskId];
  if (explicit) return explicit;
  const summary = runs.find((r) => r.taskId === taskId);
  return summary ? synthRunDetail(summary) : undefined;
}

// ----------------------------------------------------------------------------
// Work: epics & tasks
// ----------------------------------------------------------------------------

export const epics: EpicSummary[] = [
  {
    id: "AGX",
    title: "Agent execution & orchestration",
    counts: { backlog: 3, ready: 1, in_progress: 1, review: 0, blocked: 1, done: 6 },
  },
  {
    id: "CLN",
    title: "Cleanup & store hygiene",
    counts: { backlog: 2, ready: 2, in_progress: 1, review: 1, blocked: 0, done: 4 },
  },
  {
    id: "SRV",
    title: "Tusker Serve: local control-room UI",
    counts: { backlog: 3, ready: 1, in_progress: 0, review: 0, blocked: 0, done: 0 },
  },
  {
    id: "FBK",
    title: "Feedback loop",
    counts: { backlog: 1, ready: 0, in_progress: 0, review: 0, blocked: 1, done: 2 },
  },
  {
    id: "RUN",
    title: "Runner protocols & handoff",
    counts: { backlog: 2, ready: 1, in_progress: 1, review: 0, blocked: 0, done: 3 },
  },
];

export const taskCapsules: TaskCapsule[] = [
  { id: "AGX-T-0003", title: "Workflow runner lease + liveness protocol", epicId: "AGX", epicTitle: "Agent execution & orchestration", status: "in_progress", readiness: "ready", priority: "p1", risk: "high", hasGate: true, updatedAt: "2026-07-06T18:29:53Z" },
  { id: "AGX-T-0001", title: "Daemon registry heartbeat", epicId: "AGX", epicTitle: "Agent execution & orchestration", status: "done", readiness: "ready", priority: "p1", risk: "medium", hasGate: false, updatedAt: "2026-07-06T11:02:10Z" },
  { id: "CLN-T-0007", title: "Vault CAS write-path guard", epicId: "CLN", epicTitle: "Cleanup & store hygiene", status: "review", readiness: "blocked_gate", priority: "p1", risk: "high", hasGate: true, updatedAt: "2026-07-06T16:55:00Z" },
  { id: "CLN-T-0005", title: "Prune generated events older than N days", epicId: "CLN", epicTitle: "Cleanup & store hygiene", status: "done", readiness: "ready", priority: "p2", risk: "low", hasGate: false, updatedAt: "2026-07-06T12:40:00Z" },
  { id: "SRV-T-0001", title: "Author tusker serve engineering spec", epicId: "SRV", epicTitle: "Tusker Serve: local control-room UI", status: "ready", readiness: "blocked_gate", priority: "p2", risk: "medium", hasGate: true, updatedAt: "2026-07-06T15:40:00Z" },
  { id: "SRV-T-0004", title: "Embed serve SPA via go:embed", epicId: "SRV", epicTitle: "Tusker Serve: local control-room UI", status: "in_progress", readiness: "blocked_dependency", priority: "p2", risk: "medium", hasGate: false, updatedAt: "2026-07-06T18:26:58Z" },
  { id: "FBK-T-0004", title: "Feedback digest generator", epicId: "FBK", epicTitle: "Feedback loop", status: "blocked", readiness: "blocked_dependency", priority: "p3", risk: "medium", hasGate: true, updatedAt: "2026-07-06T13:05:00Z" },
  { id: "RUN-T-0012", title: "Wire ChatGPT Pro handoff credentials", epicId: "RUN", epicTitle: "Runner protocols & handoff", status: "blocked", readiness: "blocked_gate", priority: "p2", risk: "low", hasGate: true, updatedAt: "2026-07-06T14:20:00Z" },
  { id: "RUN-T-0009", title: "Codex app-server session bridge", epicId: "RUN", epicTitle: "Runner protocols & handoff", status: "in_progress", readiness: "ready", priority: "p1", risk: "high", hasGate: false, updatedAt: "2026-07-06T18:10:00Z" },
];

export const taskDetails: Record<string, TaskDetail> = {
  "AGX-T-0003": {
    ...taskCapsules[0]!,
    intent:
      "Give every dispatched runner a **lease** it must renew by emitting protocol events. If the process dies silently, the lease expires and the run is surfaced as stale within one minute — the failure mode `tusker serve` exists to make impossible.",
    acceptance: [
      { id: "a1", text: "Lease acquired before session.start, TTL 120s", proof: "pass" },
      { id: "a2", text: "Renewal piggybacks on any protocol event", proof: "pass" },
      { id: "a3", text: "Expired lease with live process routes to a failed gate", proof: "pending" },
      { id: "a4", text: "Liveness indicator turns amber at 60s, red at 120s", proof: "pending" },
    ],
    nonGoals: [
      "No distributed leasing across machines — single daemon only.",
      "No automatic process kill on expiry (v1 surfaces, human decides).",
    ],
    verification: [
      { id: "v1", command: "go test ./internal/agent/... -run Lease", result: "pass" },
      { id: "v2", command: "go test ./internal/agent/... -run Liveness", result: "pending", detail: "detector wiring incomplete" },
    ],
    evidence: [
      { id: "e1", label: "lease.go diff", kind: "diff", ref: "attempts/1/lease.go.patch" },
      { id: "e2", label: "test run log", kind: "log", ref: "attempts/1/go-test.log" },
    ],
    knowledgeDelta:
      "Introduces [[AGX-D-0002]] — leases renew on event emission, not a separate heartbeat, to avoid a second timer.",
    deps: [{ id: "AGX-T-0001", title: "Daemon registry heartbeat", status: "done" }],
    gates: [{ id: "AGX-G-0002", kind: "clarify", owner: "human:sarav" }],
    runHistory: [runs[0]!],
  },
};

// ----------------------------------------------------------------------------
// Docs
// ----------------------------------------------------------------------------

export const docList: DocListEntry[] = [
  { path: "docs/specs/05-runner-and-session-protocol.md", title: "05 · Runner & Session Protocol", kind: "spec", updatedAt: "2026-07-05T22:14:00Z" },
  { path: "docs/specs/10-tusker-serve.md", title: "10 · Tusker Serve", kind: "spec", updatedAt: "2026-07-06T15:40:00Z" },
  { path: "docs/design/tusker-serve-ux-packet.md", title: "Tusker Serve — UX Packet", kind: "knowledge", updatedAt: "2026-07-06T09:23:00Z" },
  { path: ".tusker/decisions/AGX-D-0002.md", title: "AGX-D-0002 · Leases renew on event", kind: "decision", updatedAt: "2026-07-06T18:24:00Z" },
  { path: ".tusker/work/epics/SRV.md", title: "SRV · Tusker Serve", kind: "epic", updatedAt: "2026-07-06T05:13:44Z" },
];

export const docContents: Record<string, DocContent> = {
  "docs/specs/10-tusker-serve.md": {
    path: "docs/specs/10-tusker-serve.md",
    title: "10 · Tusker Serve",
    kind: "spec",
    updatedAt: "2026-07-06T15:40:00Z",
    rev: "sha256:1f9c…a3",
    frontmatter: [
      { key: "id", value: "SRV-SPEC-10", locked: true },
      { key: "status", value: "draft", locked: true },
      { key: "owner", value: "human:sarav", locked: false },
      { key: "epic", value: "SRV", locked: true },
    ],
    outline: [
      { level: 2, text: "Overview", slug: "overview" },
      { level: 2, text: "JSON API surface", slug: "json-api-surface" },
      { level: 3, text: "Needs-me endpoints", slug: "needs-me-endpoints" },
      { level: 3, text: "Runs & liveness", slug: "runs-liveness" },
      { level: 2, text: "Embedding & serving", slug: "embedding-serving" },
    ],
    markdown: [
      "# 10 · Tusker Serve",
      "",
      "> Status: **draft** — this spec is being authored under SRV-T-0001 and",
      "> awaits approval (gate SRV-G-0001).",
      "",
      "## Overview",
      "",
      "`tusker serve` is the control room the operator opens to answer one",
      "question: *what needs me, and what is the machine doing?* It is served by",
      "the tusker binary on `localhost:7420` as an embedded single-page app over a",
      "JSON API on the SQLite runtime store and vault.",
      "",
      "## JSON API surface",
      "",
      "The SPA reads the store through a read-mostly JSON API. All mutations go",
      "through explicit action endpoints (no raw YAML writes).",
      "",
      "### Needs-me endpoints",
      "",
      "```",
      "GET  /api/needs                → NeedItem[]   (ranked, all projects)",
      "POST /api/needs/:id/resolve    → { ok }       (answer / approve / retry)",
      "```",
      "",
      "### Runs & liveness",
      "",
      "Liveness is derived server-side from `sinceLastEventSec`: **fresh** < 60s,",
      "**stale** 60–120s, **dead** ≥ 120s. The client renders the indicator but",
      "does not own the thresholds.",
      "",
      "## Embedding & serving",
      "",
      "The built SPA (`internal/serve/ui/dist`) is embedded with `go:embed` and",
      "served under `/`. API routes live under `/api`.",
    ].join("\n"),
  },
};
