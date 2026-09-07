import type { RunSummary, TaskCapsule, WaveSummary } from "@/types/domain";

// Fixture builders for the isolated sample-data preview and focused behavior
// checks. Clearly labeled sample data — never production truth. Production
// code must not import this module.

let seq = 0;

export function makeWave(
  overrides: Partial<WaveSummary> & { id: string; title: string },
): WaveSummary {
  return {
    status: "",
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
