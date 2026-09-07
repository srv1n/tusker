/*
  WUX-T-0005 — sample-data fixtures for the WaveFlow preview.

  Clearly labeled sample data (never production truth): a 5-task chain,
  parallel branches plus join, an external prerequisite, a cycle, a
  30-node long-title graph, a 100-node performance graph, a live-update
  pair with identical topology, and a dependency-free set.
*/

import type { RunSummary, TaskDetail } from "@/types/domain";
import type { DependencyFact } from "./flowGraph";

export interface FlowFixture {
  key: string;
  label: string;
  description: string;
  memberIds: string[];
  tasks: TaskDetail[];
  runs: RunSummary[];
  dependencyFacts: Record<string, DependencyFact>;
}

let sequence = 0;

function task(
  id: string,
  title: string,
  status: TaskDetail["status"],
  depIds: string[] = [],
): TaskDetail {
  sequence += 1;
  return {
    id,
    projectId: "sample",
    title,
    epicId: "sample-epic",
    epicTitle: "Sample epic",
    status,
    readiness: status === "blocked" ? "blocked_dependency" : "ready",
    priority: "p2",
    risk: "medium",
    hasGate: false,
    updatedAt: `2026-09-0${(sequence % 7) + 1}T10:00:00Z`,
    intent: `Sample intent for ${id}.`,
    acceptance: [{ id: "A1", text: "Sample acceptance.", proof: "pending" }],
    nonGoals: [],
    verification: [],
    evidence: [],
    deps: depIds.map((dep) => ({ id: dep, title: dep, status: "ready" as const })),
    gates: [],
    runHistory: [],
  };
}

function liveRun(taskId: string, model: string): RunSummary {
  return {
    taskId,
    taskTitle: taskId,
    projectId: "sample",
    runner: "codex",
    model,
    lane: "execute",
    leaseState: "held",
    leaseStateRaw: "running",
    processRunning: true,
    outcome: "running",
    elapsedSec: 42,
    sinceLastEventSec: 3,
    liveness: "fresh",
    attemptCount: 1,
    terminal: false,
  };
}

function terminalRun(taskId: string, outcome: RunSummary["outcome"]): RunSummary {
  return {
    ...liveRun(taskId, "sample-model"),
    leaseState: "released",
    leaseStateRaw: "settled",
    processRunning: false,
    outcome,
    liveness: "stale",
    terminal: true,
  };
}

function fixture(
  key: string,
  label: string,
  description: string,
  memberIds: string[],
  tasks: TaskDetail[],
  runs: RunSummary[] = [],
  dependencyFacts: Record<string, DependencyFact> = {},
): FlowFixture {
  return { key, label, description, memberIds, tasks, runs, dependencyFacts };
}

export function chainFixture(): FlowFixture {
  const tasks = [
    task("WUX-T-0001", "Fetch the wave contract", "done"),
    task("WUX-T-0002", "Derive the display states", "done", ["WUX-T-0001"]),
    task("WUX-T-0003", "Lay out the graph layers", "in_progress", ["WUX-T-0002"]),
    task("WUX-T-0004", "Wire the toolbar controls", "review", ["WUX-T-0003"]),
    task("WUX-T-0005", "Capture the evidence report", "ready", ["WUX-T-0004"]),
  ];
  return fixture(
    "chain",
    "5-task chain",
    "Linear prerequisites with completed, executing, reviewing and queued states.",
    tasks.map((item) => item.id),
    tasks,
    [terminalRun("WUX-T-0001", "succeeded"), terminalRun("WUX-T-0002", "succeeded"), liveRun("WUX-T-0003", "terra-pro-4")],
  );
}

export function branchesFixture(): FlowFixture {
  const tasks = [
    task("WF-B-01", "Open the wave boundary", "done"),
    task("WF-B-02", "Render the left branch", "in_progress", ["WF-B-01"]),
    task("WF-B-03", "Render the right branch", "in_progress", ["WF-B-01"]),
    task("WF-B-04", "Join both branches at review", "ready", ["WF-B-02", "WF-B-03"]),
  ];
  return fixture(
    "branches",
    "Parallel branches plus join",
    "Two live branches converging on one join node.",
    tasks.map((item) => item.id),
    tasks,
    [terminalRun("WF-B-01", "succeeded"), liveRun("WF-B-02", "terra-pro-4"), liveRun("WF-B-03", "luna-flash-2")],
  );
}

export function externalFixture(): FlowFixture {
  const tasks = [
    task("WF-E-01", "Read the upstream schema", "ready", ["EXT-SCHEMA-9"]),
    task("WF-E-02", "Render against the schema", "backlog", ["WF-E-01"]),
  ];
  return fixture(
    "external",
    "External prerequisite",
    "A confirmed outside-wave prerequisite renders as an external node.",
    tasks.map((item) => item.id),
    tasks,
    [],
    { "EXT-SCHEMA-9": { kind: "external", title: "Upstream schema (another wave)" } },
  );
}

export function unresolvedFixture(): FlowFixture {
  const tasks = [task("WF-U-01", "Depend on an unconfirmed task", "ready", ["WF-UNKNOWN-7"])];
  return fixture(
    "unresolved",
    "Unresolved reference",
    "No dependencyFacts supplied: the reference is labeled unresolved, not missing.",
    tasks.map((item) => item.id),
    tasks,
  );
}

export function cycleFixture(): FlowFixture {
  const tasks = [
    task("WF-C-01", "First cyclic task", "ready", ["WF-C-03"]),
    task("WF-C-02", "Second cyclic task", "ready", ["WF-C-01"]),
    task("WF-C-03", "Third cyclic task", "ready", ["WF-C-02"]),
    task("WF-C-04", "Downstream of the cycle", "backlog", ["WF-C-02"]),
  ];
  return fixture(
    "cycle",
    "Dependency cycle",
    "A three-node cycle renders with dashed edges plus an honest warning and list.",
    tasks.map((item) => item.id),
    tasks,
  );
}

export function mixedFixture(): FlowFixture {
  const tasks = [
    task("WF-M-01", "Blocked behind a gate", "blocked"),
    task("WF-M-02", "Failed and awaiting retry", "ready", ["WF-M-01"]),
    task("WF-M-03", "Member detail never loaded", "ready"),
    task("WF-M-04", "Confirmed-missing reference", "ready", ["WF-GONE-1"]),
  ];
  const members = ["WF-M-01", "WF-M-02", "WF-M-03", "WF-M-04", "WF-M-05"];
  return fixture(
    "mixed",
    "Blocked, failed and unavailable",
    "Blocked/failed states, an unloaded member, and a confirmed-missing reference.",
    members,
    tasks,
    [terminalRun("WF-M-02", "failed")],
    { "WF-GONE-1": { kind: "missing", title: "Deleted upstream task" } },
  );
}

const LONG_TITLES = [
  "Reconcile the cross-project dependency ledger before the authorization boundary closes",
  "Render thirty nodes with unusually long human-readable titles without clipping meaning",
  "Preserve keyboard traversal order across every layered branch of the growing graph",
  "Keep the selected task anchored while background live updates refresh branch states",
];

export function thirtyFixture(): FlowFixture {
  const tasks: TaskDetail[] = [];
  const runs: RunSummary[] = [];
  const memberIds: string[] = [];
  for (let i = 1; i <= 30; i += 1) {
    const id = `WF-L-${String(i).padStart(2, "0")}`;
    memberIds.push(id);
    const title = `${LONG_TITLES[(i - 1) % LONG_TITLES.length]} (${i}/30)`;
    const deps: string[] = [];
    if (i > 1) deps.push(`WF-L-${String(i - 1).padStart(2, "0")}`);
    if (i > 3 && i % 3 === 0) deps.push(`WF-L-${String(i - 3).padStart(2, "0")}`);
    const status = i <= 8 ? "done" : i <= 12 ? "review" : i <= 16 ? "in_progress" : i % 7 === 0 ? "blocked" : "ready";
    tasks.push(task(id, title, status, deps));
    if (status === "in_progress") runs.push(liveRun(id, "terra-pro-4"));
    if (status === "done") runs.push(terminalRun(id, "succeeded"));
  }
  return fixture(
    "thirty",
    "Thirty long-title tasks",
    "Thirty nodes with long titles, branches, and mixed states for legibility checks.",
    memberIds,
    tasks,
    runs,
  );
}

export function hundredFixture(): FlowFixture {
  const tasks: TaskDetail[] = [];
  const runs: RunSummary[] = [];
  const memberIds: string[] = [];
  for (let i = 1; i <= 100; i += 1) {
    const id = `WF-P-${String(i).padStart(3, "0")}`;
    memberIds.push(id);
    const deps: string[] = [];
    if (i > 1) deps.push(`WF-P-${String(i - 1).padStart(3, "0")}`);
    if (i % 4 === 0 && i > 4) deps.push(`WF-P-${String(i - 4).padStart(3, "0")}`);
    const status = i <= 20 ? "done" : i % 9 === 0 ? "blocked" : i % 5 === 0 ? "review" : "ready";
    tasks.push(task(id, `Performance node ${i} of one hundred`, status, deps));
    if (status === "done") runs.push(terminalRun(id, "succeeded"));
  }
  return fixture(
    "hundred",
    "One hundred nodes",
    "Performance probe: render measurements identify host and browser.",
    memberIds,
    tasks,
    runs,
  );
}

export function liveUpdateBase(): FlowFixture {
  const base = chainFixture();
  return { ...base, key: "live", label: "Live update", description: " advancing states keeps topology and viewport." };
}

export function liveUpdateNext(): FlowFixture {
  const tasks = [
    task("WUX-T-0001", "Fetch the wave contract", "done"),
    task("WUX-T-0002", "Derive the display states", "done", ["WUX-T-0001"]),
    task("WUX-T-0003", "Lay out the graph layers", "done", ["WUX-T-0002"]),
    task("WUX-T-0004", "Wire the toolbar controls", "in_progress", ["WUX-T-0003"]),
    task("WUX-T-0005", "Capture the evidence report", "ready", ["WUX-T-0004"]),
  ];
  return fixture(
    "live-next",
    "Live update (next)",
    "Same topology, advanced states: viewport must not move.",
    tasks.map((item) => item.id),
    tasks,
    [terminalRun("WUX-T-0001", "succeeded"), terminalRun("WUX-T-0002", "succeeded"), terminalRun("WUX-T-0003", "succeeded"), liveRun("WUX-T-0004", "terra-pro-4")],
  );
}

export function noDepsFixture(): FlowFixture {
  const tasks = [
    task("WF-N-01", "First independent task", "ready"),
    task("WF-N-02", "Second independent task", "ready"),
    task("WF-N-03", "Third independent task", "backlog"),
  ];
  return fixture(
    "nodeps",
    "No dependencies",
    "Members without edges render as nodes plus an honest empty-edges note.",
    tasks.map((item) => item.id),
    tasks,
  );
}

export const ALL_FIXTURES: FlowFixture[] = [
  chainFixture(),
  branchesFixture(),
  externalFixture(),
  unresolvedFixture(),
  cycleFixture(),
  mixedFixture(),
  thirtyFixture(),
  hundredFixture(),
  noDepsFixture(),
];
