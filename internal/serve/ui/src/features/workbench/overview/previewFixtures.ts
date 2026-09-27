import type { RunSummary, TaskCapsule, TaskState, TaskStateCode, WaveSummary } from "@/types/domain";

// Fixture builders for the isolated sample-data preview and focused behavior
// checks. Clearly labeled sample data — never production truth. Production
// code must not import this module.

let seq = 0;

const SAMPLE_STATE: Record<TaskStateCode, Pick<TaskState, "label" | "category" | "next_actor">> = {
  backlog: { label: "Backlog", category: "not_started", next_actor: "architect" },
  planned: { label: "Planned", category: "not_started", next_actor: "daemon" },
  working: { label: "Working", category: "active", next_actor: "worker" },
  needs_input: { label: "Needs input", category: "active", next_actor: "you" },
  blocked: { label: "Blocked", category: "active", next_actor: "you" },
  in_review: { label: "In review", category: "active", next_actor: "reviewer" },
  done: { label: "Done", category: "closed", next_actor: "nobody" },
  canceled: { label: "Canceled", category: "closed", next_actor: "nobody" },
};

/** A sample server state record (the real one is computed by the server). */
export function sampleState(state: TaskStateCode, reason = "", extra: Partial<TaskState> = {}): TaskState {
  return { state, ...SAMPLE_STATE[state], reason_code: "", reason, next_action: "", ...extra };
}

export function makeWave(
  overrides: Partial<WaveSummary> & { id: string; title: string },
): WaveSummary {
  return {
    status: "",
    state: sampleState("planned"),
    landedAt: null,
    memberIds: [],
    members: [],
    counts: {},
    authorization: { state: "armed", stale: false, action: "" },
    brief: {
      schema: "tusker.wave-brief/v1",
      waveId: overrides.id,
      title: overrides.title,
      waveHref: "",
      sectionOrder: ["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"],
      outcome: { summary: "", fullyDrained: false, counts: {}, tasks: [] },
      seeIt: [],
      landed: [],
      reworkParked: [],
      humanAction: [],
      documentation: [],
    },
    ...overrides,
  };
}

export function makeTask(
  overrides: Partial<TaskCapsule> & { id: string; title: string },
): TaskCapsule {
  seq += 1;
  return {
    epicId: "WUX",
    epicTitle: "Work experience",
    status: "ready",
    state: sampleState("planned"),
    readiness: "ready",
    priority: "p2",
    risk: "medium",
    hasGate: false,
    updatedAt: `2026-09-0${(seq % 7) + 1}T10:00:00Z`,
    ...overrides,
  };
}

export function makeRun(
  overrides: Partial<RunSummary> & { taskId: string },
): RunSummary {
  return {
    taskTitle: overrides.taskId,
    projectId: "demo",
    runner: "codex",
    model: "sample-model",
    lane: "execute",
    leaseState: "held",
    leaseStateRaw: "running",
    outcome: "running",
    elapsedSec: 42,
    sinceLastEventSec: 3,
    liveness: "fresh",
    attemptCount: 1,
    terminal: false,
    ...overrides,
  };
}

export function humanActionEntry(gateId: string, blockedTaskIds: string[]) {
  return {
    gateId,
    gateHref: "",
    action: `Sample decision for ${gateId}`,
    resumeId: "",
    blockedTaskIds,
  };
}
