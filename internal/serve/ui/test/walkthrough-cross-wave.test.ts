import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { WaveFlow } from "../src/features/workbench/flow/WaveFlow";
import { crossWaveWaitSummary, type DependencyFact } from "../src/features/workbench/flow/flowGraph";
import type { TaskDetail } from "../src/types/domain";

const ALPHA: DependencyFact = {
  kind: "external", title: "Assemble alpha report", status: "done", readiness: "done",
  waveId: "W-0002", waveTitle: "Alpha: assemble a small report",
};
const BETA: DependencyFact = {
  kind: "external", title: "Assemble beta report", status: "backlog", readiness: "held",
  waveId: "W-0003", waveTitle: "Beta: assemble an independent report",
};

function member(deps: string[]): TaskDetail {
  return {
    id: "FOL-T-0001",
    projectId: "sample",
    title: "Open combined report",
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
    deps: deps.map((id) => ({ id, title: id, status: "ready" as const })),
    gates: [],
    runHistory: [],
  };
}

function renderFlow(facts: Record<string, DependencyFact>, opts: { authorization?: "inert" | "authorized" | "paused" | "stale"; startEnabled?: boolean } = {}): string {
  return renderToStaticMarkup(createElement(WaveFlow, {
    memberIds: ["FOL-T-0001"],
    tasks: [member(Object.keys(facts))],
    runs: [],
    dependencyFacts: facts,
    authorization: opts.authorization,
    startEnabled: opts.startEnabled,
    onSelectTask: () => {},
    onViewportChange: () => {},
  }));
}

describe("cross-wave wait summary", () => {
  test("mixed completed Alpha and unfinished Beta produce the exact copy", () => {
    const summary = crossWaveWaitSummary([ALPHA, BETA], "inert", true);
    expect(summary?.title).toBe("Waiting for Beta");
    expect(summary?.body).toBe("“Assemble beta report” must finish before this wave can begin. Alpha is complete.");
    expect(summary?.hint).toBe("Start now to have this wave begin automatically after Beta finishes.");
  });

  test("authorized waves promise automatic continuation, never a Start instruction", () => {
    const summary = crossWaveWaitSummary([ALPHA, BETA], "authorized", false);
    expect(summary?.title).toBe("Waiting for Beta");
    expect(summary?.hint).toBe("Tusker will begin automatically when Beta finishes.");
    expect(summary?.hint).not.toContain("Start");
  });

  test("completed prerequisites remove the summary", () => {
    expect(crossWaveWaitSummary([ALPHA, { ...BETA, status: "done", readiness: "done" }], "inert", true)).toBeUndefined();
    expect(crossWaveWaitSummary([], "inert", true)).toBeUndefined();
  });

  test("missing and unavailable facts never invent wait copy", () => {
    expect(crossWaveWaitSummary([{ kind: "missing" }, { kind: "unavailable" }], "inert", true)).toBeUndefined();
    const paused = crossWaveWaitSummary([BETA], "paused", false);
    expect(paused?.title).toBe("Waiting for Beta");
    expect(paused?.hint).toBeUndefined();
  });
});

describe("cross-wave WaveFlow surface", () => {
  test("valid wait names titles, waves, and states with no Unknown or repair copy", () => {
    const html = renderFlow({ "ALP-T-0004": ALPHA, "BET-T-0004": BETA }, { authorization: "inert", startEnabled: true });
    expect(html).toContain("Waiting for Beta");
    expect(html).toContain("“Assemble beta report” must finish before this wave can begin. Alpha is complete.");
    expect(html).toContain("Start now to have this wave begin automatically after Beta finishes.");
    expect(html).toContain("Assemble alpha report");
    expect(html).toContain("Assemble beta report");
    expect(html).toContain("Alpha: assemble a small report");
    expect(html).toContain("Beta: assemble an independent report");
    expect(html).toContain("Completed");
    expect(html).toContain("Backlog");
    expect(html).toContain("ALP-T-0004");
    expect(html).toContain("BET-T-0004");
    expect(html).not.toContain("Unknown");
    expect(html).not.toContain("Unresolved dependency");
    expect(html).not.toContain("econcil");
  });

  test("authorized wait drops the Start instruction", () => {
    const html = renderFlow({ "ALP-T-0004": ALPHA, "BET-T-0004": BETA }, { authorization: "authorized" });
    expect(html).toContain("Tusker will begin automatically when Beta finishes.");
    expect(html).not.toContain("Start now");
  });

  test("missing and unavailable stay explicit and neutral", () => {
    const html = renderFlow({
      "ALP-T-0004": ALPHA,
      "GONE-T-0001": { kind: "missing" },
      "SHUT-T-0001": { kind: "unavailable" },
    });
    expect(html).toContain("Dependency missing");
    expect(html).toContain("Dependency status unavailable");
    expect(html).toContain("GONE-T-0001");
    expect(html).toContain("SHUT-T-0001");
    expect(html).not.toContain("Unresolved dependency");
  });

  test("all-complete prerequisites render no waiting copy", () => {
    const html = renderFlow({
      "ALP-T-0004": ALPHA,
      "BET-T-0004": { ...BETA, status: "done", readiness: "done" },
    });
    expect(html).not.toContain("Waiting for");
    expect(html).not.toContain("must finish before this wave can begin");
  });
});
