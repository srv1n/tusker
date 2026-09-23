import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { WaveFlow } from "../src/features/workbench/flow/WaveFlow";
import { crossWaveWaitSummary, type DependencyFact } from "../src/features/workbench/flow/flowGraph";
import type { TaskDetail, WaveReview } from "../src/types/domain";

const ALPHA_ID = "ALP-T-0004";
const BETA_ID = "BET-T-0004";
const C1 = "FOL-T-0001";
const MEMBERS = [C1, "FOL-T-0002", "FOL-T-0003", "FOL-T-0004"];

function review(overrides: Partial<WaveReview>): WaveReview {
  return {
    schema: "tusker.wave-review/v1",
    waveId: "W-0004",
    title: "Follow-up: combine the wave reports",
    outcome: "Ship the combined report.",
    state: "Planned",
    authorization: "inert",
    materialFingerprint: "sha256:test",
    members: MEMBERS.map((taskId) => ({
      taskId,
      title: taskId === C1 ? "Open combined report" : `Follow-up task ${taskId}`,
      state: taskId === C1 ? "waiting" : "backlog",
      waitingReason: taskId === C1 ? `waiting for dependency ${BETA_ID}` : undefined,
      dependencies: taskId === C1 ? [ALPHA_ID, BETA_ID] : [],
    })),
    externalDependencies: [
      {
        taskId: ALPHA_ID,
        title: "Assemble alpha report",
        status: "done",
        readiness: "done",
        waveId: "W-0002",
        waveTitle: "Alpha: assemble a small report",
        classification: "external",
      },
      {
        taskId: BETA_ID,
        title: "Assemble beta report",
        status: "backlog",
        readiness: "held",
        waveId: "W-0003",
        waveTitle: "Beta: assemble an independent report",
        classification: "external",
      },
    ],
    frontiers: [[C1]],
    blockers: [],
    controls: [{ action: "wave start", scope: "W-0004", enabled: true }],
    ...overrides,
  };
}

function taskDetail(id: string, deps: string[]): TaskDetail {
  return {
    id,
    projectId: "sample",
    title: id === C1 ? "Open combined report" : `Follow-up task ${id}`,
    epicId: "FOL",
    epicTitle: "Combined report",
    status: "backlog",
    readiness: "blocked_dependency",
    priority: "p2",
    risk: "medium",
    hasGate: false,
    updatedAt: "2026-09-20T10:00:00Z",
    intent: "Combine the wave reports.",
    acceptance: [],
    nonGoals: [],
    verification: [],
    evidence: [],
    deps: deps.map((dep) => ({ id: dep, title: dep, status: "ready" as const })),
    gates: [],
    runHistory: [],
  };
}

function renderReview(wire: WaveReview): string {
  const dependencyFacts: Record<string, DependencyFact> = Object.fromEntries(
    wire.externalDependencies.map((fact) => [fact.taskId, {
      kind: fact.classification,
      title: fact.title,
      status: fact.status,
      readiness: fact.readiness,
      waveId: fact.waveId,
      waveTitle: fact.waveTitle,
    } satisfies DependencyFact]),
  );
  const startEnabled = wire.controls.some((control) => control.action === "wave start" && control.enabled);
  // The wave page shows the wait summary in its callout beside the graph.
  const summary = crossWaveWaitSummary(Object.values(dependencyFacts), wire.authorization, startEnabled);
  return [summary?.title, summary?.body, summary?.hint].filter(Boolean).join(" ") + renderToStaticMarkup(createElement(WaveFlow, {
    memberIds: MEMBERS,
    tasks: [taskDetail(C1, [ALPHA_ID, BETA_ID]), taskDetail("FOL-T-0002", [C1]), taskDetail("FOL-T-0003", [C1]), taskDetail("FOL-T-0004", ["FOL-T-0002", "FOL-T-0003"])],
    runs: [],
    reviewMembers: wire.members,
    dependencyFacts,
    onSelectTask: () => {},
    onViewportChange: () => {},
  }));
}

describe("cross-wave integrated snapshots", () => {
  test("inert wave waiting on Beta names both upstreams and offers Start", () => {
    const html = renderReview(review({}));
    expect(html).toContain("Waiting for Beta");
    expect(html).toContain("“Assemble beta report” must finish before this wave can begin. Alpha is complete.");
    expect(html).toContain("Start now to have this wave begin automatically after Beta finishes.");
    expect(html).toContain("Assemble alpha report");
    expect(html).toContain("Assemble beta report");
    expect(html).toContain("Done");
    expect(html).toContain("Planned");
    expect(html).toContain(ALPHA_ID);
    expect(html).toContain(BETA_ID);
    expect(html).not.toContain("Unknown");
    expect(html).not.toContain("Unresolved dependency");
    expect(html).not.toContain("econcil");
  });

  test("authorized wave promises automatic continuation with no Start copy", () => {
    const html = renderReview(review({
      authorization: "authorized",
      controls: [{ action: "wave start", scope: "W-0004", enabled: false }],
    }));
    expect(html).toContain("Waiting for Beta");
    expect(html).toContain("Tusker will begin automatically when Beta finishes.");
    expect(html).not.toContain("Start now");
  });

  test("all-complete prerequisites drop the wait summary and show both upstreams completed", () => {
    const html = renderReview(review({
      authorization: "authorized",
      state: "Waiting",
      externalDependencies: [
        { taskId: ALPHA_ID, title: "Assemble alpha report", status: "done", readiness: "done", waveId: "W-0002", waveTitle: "Alpha: assemble a small report", classification: "external" },
        { taskId: BETA_ID, title: "Assemble beta report", status: "done", readiness: "done", waveId: "W-0003", waveTitle: "Beta: assemble an independent report", classification: "external" },
      ],
      controls: [],
    }));
    expect(html).not.toContain("Waiting for");
    expect(html).not.toContain("must finish before this wave can begin");
    const completed = html.match(/Done/g) ?? [];
    expect(completed.length).toBeGreaterThanOrEqual(2);
    expect(html).not.toContain("Unresolved dependency");
  });

  test("queued admission renders one queued member and no second Start", () => {
    const html = renderReview(review({
      authorization: "authorized",
      state: "Waiting",
      members: MEMBERS.map((taskId) => ({
        taskId,
        title: taskId === C1 ? "Open combined report" : `Follow-up task ${taskId}`,
        state: taskId === C1 ? "waiting" : "backlog",
        phase: taskId === C1 ? "queued" : undefined,
        responsible: taskId === C1 ? "daemon" : undefined,
        waitingReason: taskId === C1 ? "queued for dispatch" : undefined,
        dependencies: taskId === C1 ? [ALPHA_ID, BETA_ID] : [],
      })),
      externalDependencies: [
        { taskId: ALPHA_ID, title: "Assemble alpha report", status: "done", readiness: "done", waveId: "W-0002", waveTitle: "Alpha: assemble a small report", classification: "external" },
        { taskId: BETA_ID, title: "Assemble beta report", status: "done", readiness: "done", waveId: "W-0003", waveTitle: "Beta: assemble an independent report", classification: "external" },
      ],
      controls: [],
    }));
    expect(html).toContain(`aria-label="Open combined report (${C1}), Task, Waiting"`);
    const queued = html.match(/aria-label="[^"]*, Waiting/g) ?? [];
    expect(queued.length).toBe(1);
    expect(html).not.toContain("Start now");
    expect(html).not.toContain("Start");
    expect(html).not.toContain("Waiting for");
  });
});
