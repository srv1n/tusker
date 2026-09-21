/*
  WUX-T-0005 — WaveFlow focused behavior checks.

  Exercises the real graph model (classification, states, cycles,
  viewport math, topology stability). Every test below must execute;
  zero-match names are failures.
*/

import { describe, expect, test } from "bun:test";
import type { RunSummary, TaskDetail } from "../src/types/domain";
import {
  NODE_WIDTH,
  buildFlowGraph,
  clampViewport,
  displayStateFor,
  essentialFlowEdges,
  externalDependencyState,
  fitViewport,
  initialViewport,
  isLiveRun,
  layoutFlowGraph,
  layoutTopDownFlowGraph,
  modelFor,
  panViewport,
  topologyKey,
  zoomViewport,
  type FlowViewport,
} from "../src/features/workbench/flow/flowGraph";
import { chainFixture, cycleFixture, liveUpdateBase, liveUpdateNext, thirtyFixture } from "../src/features/workbench/flow/fixtures";

function run(taskId: string, overrides: Partial<RunSummary> = {}): RunSummary {
  return {
    taskId,
    taskTitle: taskId,
    projectId: "sample",
    runner: "codex",
    model: "terra-pro-4",
    lane: "execute",
    leaseState: "held",
    leaseStateRaw: "running",
    processRunning: true,
    outcome: "running",
    elapsedSec: 10,
    sinceLastEventSec: 2,
    liveness: "fresh",
    attemptCount: 1,
    terminal: false,
    ...overrides,
  };
}

describe("WaveFlow graph model", () => {
  test("flow preserves selected viewport", () => {
    // A viewport the user settled on must survive selection events and
    // unrelated prop churn: viewport math is pure and never derived from
    // selection, and re-committing the same value is a no-op.
    const settled: FlowViewport = { x: -120, y: 40, scale: 1.5 };
    const recommitted = clampViewport({ ...settled });
    expect(recommitted).toEqual(settled);

    // Panning then zooming back out returns to the settled value.
    const moved = panViewport(settled, 30, -10);
    expect(moved).not.toEqual(settled);
    const back = panViewport(moved, -30, 10);
    expect(back).toEqual(settled);

    // Initial view is always the readable origin, independent of selection.
    expect(initialViewport()).toEqual({ x: 0, y: 0, scale: 1 });
  });

  test("flow external and cyclic dependencies", () => {
    // Confirmed external references render as external nodes with an edge.
    const chain = chainFixture();
    const external = buildFlowGraph({
      memberIds: [...chain.memberIds, "WUX-T-010"],
      tasks: [
        ...chain.tasks,
        {
          ...chain.tasks[0]!,
          id: "WUX-T-010",
          title: "Downstream of another wave",
          status: "ready",
          deps: [{ id: "EXT-1", title: "EXT-1", status: "done" }],
        } satisfies TaskDetail,
      ],
      runs: chain.runs,
      dependencyFacts: { "EXT-1": { kind: "external", title: "Upstream wave task" } },
    });
    const externalNode = external.nodes.find((node) => node.id === "EXT-1");
    expect(externalNode?.kind).toBe("external");
    expect(externalNode?.title).toBe("Upstream wave task");
    expect(external.edges.some((edge) => edge.from === "EXT-1" && edge.to === "WUX-T-010")).toBe(true);

    // Unconfirmed references are unresolved — never missing, never external.
    const unresolved = buildFlowGraph({
      memberIds: ["A"],
      tasks: [
        {
          ...chain.tasks[0]!,
          id: "A",
          title: "A",
          status: "ready",
          deps: [{ id: "GHOST", title: "GHOST", status: "ready" }],
        } satisfies TaskDetail,
      ],
    });
    const ghost = unresolved.nodes.find((node) => node.id === "GHOST");
    expect(ghost?.kind).toBe("unresolved");
    expect(unresolved.warnings.some((warning) => warning.kind === "unresolved")).toBe(true);
    expect(unresolved.warnings.some((warning) => warning.kind === "missing")).toBe(false);

    // Canonical facts give the external node its real title, state, and wave
    // context; a valid external fact produces no dependency warning.
    const factual = buildFlowGraph({
      memberIds: ["WUX-T-010"],
      tasks: [
        {
          ...chain.tasks[0]!,
          id: "WUX-T-010",
          title: "Downstream of another wave",
          status: "ready",
          deps: [
            { id: "EXT-DONE", title: "EXT-DONE", status: "done" },
            { id: "EXT-OPEN", title: "EXT-OPEN", status: "ready" },
            { id: "EXT-GONE", title: "EXT-GONE", status: "ready" },
            { id: "EXT-SHUT", title: "EXT-SHUT", status: "ready" },
          ],
        } satisfies TaskDetail,
      ],
      dependencyFacts: {
        "EXT-DONE": { kind: "external", title: "Assemble alpha report", status: "done", readiness: "done", waveId: "W-0002", waveTitle: "Alpha: assemble a small report" },
        "EXT-OPEN": { kind: "external", title: "Assemble beta report", status: "backlog", readiness: "held", waveId: "W-0003", waveTitle: "Beta: assemble an independent report" },
        "EXT-GONE": { kind: "missing" },
        "EXT-SHUT": { kind: "unavailable" },
      },
    });
    const done = factual.nodes.find((node) => node.id === "EXT-DONE");
    const open = factual.nodes.find((node) => node.id === "EXT-OPEN");
    const gone = factual.nodes.find((node) => node.id === "EXT-GONE");
    const shut = factual.nodes.find((node) => node.id === "EXT-SHUT");
    expect(done?.kind).toBe("external");
    expect(done?.title).toBe("Assemble alpha report");
    expect(done?.state).toBe("completed");
    expect(done?.context).toBe("Alpha: assemble a small report");
    expect(open?.state).toBe("backlog");
    expect(open?.context).toBe("Beta: assemble an independent report");
    // Missing/unavailable keep the durable ID as title and carry explicit
    // neutral state labels instead of invented facts.
    expect(gone?.kind).toBe("missing");
    expect(gone?.title).toBe("EXT-GONE");
    expect(gone?.state).toBe("unknown");
    expect(gone?.stateLabel).toBe("Dependency missing");
    expect(shut?.kind).toBe("unavailable");
    expect(shut?.title).toBe("EXT-SHUT");
    expect(shut?.stateLabel).toBe("Dependency status unavailable");
    const unavailableWarning = factual.warnings.find((warning) => warning.kind === "unavailable");
    expect(unavailableWarning?.text).toContain("Dependency status unavailable");
    // Valid external facts never surface as warnings.
    expect(factual.warnings.some((warning) => warning.taskIds.includes("EXT-DONE") || warning.taskIds.includes("EXT-OPEN"))).toBe(false);
    // Canonical state mapping honors status first, then readiness.
    expect(externalDependencyState({ kind: "external", status: "done" })).toBe("completed");
    expect(externalDependencyState({ kind: "external", status: "review" })).toBe("reviewing");
    expect(externalDependencyState({ kind: "external", status: "ready" })).toBe("ready");
    expect(externalDependencyState({ kind: "external", status: "backlog" })).toBe("backlog");
    expect(externalDependencyState({ kind: "external", readiness: "held" })).toBe("backlog");
    expect(externalDependencyState({ kind: "external", status: "mystery", readiness: "mystery" })).toBe("unknown");

    // Cycles stay inspectable: nodes render, cyclic edges flag, warning lists.
    const cycle = cycleFixture();
    const cyclic = buildFlowGraph(cycle);
    expect(cyclic.cycles.length).toBeGreaterThan(0);
    expect(cyclic.edges.filter((edge) => edge.cyclic).length).toBeGreaterThan(0);
    const warning = cyclic.warnings.find((item) => item.kind === "cycle");
    expect(warning).toBeDefined();
    expect(warning!.taskIds).toContain("WF-C-01");
    for (const id of cycle.memberIds) {
      expect(cyclic.nodes.some((node) => node.id === id)).toBe(true);
    }
    // Cyclic layout still terminates with every node positioned.
    const layout = layoutFlowGraph(cyclic, cycle.memberIds);
    for (const id of cycle.memberIds) {
      expect(layout.positions[id]).toBeDefined();
    }
  });

  test("flow live update preserves topology", () => {
    // Same topology with advanced states: identical topology key, identical
    // node positions — a live refresh must not relayout or move the viewport.
    const before = liveUpdateBase();
    const after = liveUpdateNext();
    const graphBefore = buildFlowGraph(before);
    const graphAfter = buildFlowGraph(after);
    expect(topologyKey(graphAfter)).toBe(topologyKey(graphBefore));

    const layoutBefore = layoutFlowGraph(graphBefore, before.memberIds);
    const layoutAfter = layoutFlowGraph(graphAfter, after.memberIds);
    expect(layoutAfter.positions).toEqual(layoutBefore.positions);

    // But the states genuinely advanced: executing moved downstream.
    const stateOf = (graph: typeof graphBefore, id: string): string | undefined =>
      graph.nodes.find((node) => node.id === id)?.state;
    expect(stateOf(graphBefore, "WUX-T-0003")).toBe("executing");
    expect(stateOf(graphAfter, "WUX-T-0003")).toBe("completed");
    expect(stateOf(graphAfter, "WUX-T-0004")).toBe("executing");
  });

  test("flow display states are truthful", () => {
    expect(displayStateFor("done", run("A"))).toBe("completed");
    // Worker success is not completion: done alone decides completed.
    // A live run proves current activity, so it wins; the lane keeps
    // the worker stage distinct from the reviewer stage.
    expect(displayStateFor("review", run("A"))).toBe("executing");
    expect(displayStateFor("review", run("A", { lane: "review" }))).toBe("reviewing");
    expect(displayStateFor("review", undefined)).toBe("reviewing");
    expect(displayStateFor("blocked", undefined)).toBe("blocked");
    expect(displayStateFor("ready", undefined)).toBe("ready");
    expect(displayStateFor("backlog", undefined)).toBe("backlog");
    // A stale run behind in_progress is unknown, not executing.
    expect(
      displayStateFor("in_progress", run("A", { liveness: "stale", leaseStateRaw: "running" })),
    ).toBe("unknown");
    // A terminal failed run surfaces failure.
    expect(
      displayStateFor("ready", run("A", { terminal: true, outcome: "failed", liveness: "stale", leaseStateRaw: "settled" })),
    ).toBe("failed");
    // Live runs earn executing even when the durable status lags.
    expect(displayStateFor("ready", run("A"))).toBe("executing");
    expect(isLiveRun(run("A", { liveness: "dead" }))).toBe(false);
  });

  test("flow model labels use verified identity only", () => {
    expect(modelFor(run("A"))).toBe("terra-pro-4");
    // Stale, terminal, or model-less runs never label a node.
    expect(modelFor(run("A", { liveness: "stale" }))).toBeUndefined();
    expect(modelFor(run("A", { terminal: true, outcome: "succeeded", leaseStateRaw: "settled" }))).toBeUndefined();
    expect(modelFor(run("A", { model: "  " }))).toBeUndefined();
    expect(modelFor(undefined)).toBeUndefined();
  });

  test("flow viewport math stays readable", () => {
    // Zoom clamps to the legible band and anchors the cursor point.
    const zoomed = zoomViewport({ x: 0, y: 0, scale: 1 }, 99, { x: 100, y: 100 });
    expect(zoomed.scale).toBe(2.5);
    expect(zoomed.x).toBeLessThan(0);
    // Zooming out clamps at the floor.
    expect(zoomViewport({ x: 0, y: 0, scale: 1 }, 0.001).scale).toBe(0.25);
    // Fit shows the whole graph inside the container with padding.
    const fitted = fitViewport({ width: 2000, height: 1000 }, { width: 800, height: 480 });
    expect(fitted.scale).toBeLessThan(1);
    expect(fitted.x).toBeGreaterThanOrEqual(0);
    expect(fitted.y).toBeGreaterThanOrEqual(0);
  });

  test("flow thirty-node graph lays out left to right", () => {
    const fixture = thirtyFixture();
    const graph = buildFlowGraph(fixture);
    expect(graph.nodes.length).toBe(30);
    const layout = layoutFlowGraph(graph, fixture.memberIds);
    const xOf = (id: string): number => layout.positions[id]?.x ?? NaN;
    // Chain order is preserved along x.
    expect(xOf("WF-L-02")).toBeGreaterThan(xOf("WF-L-01"));
    expect(xOf("WF-L-30")).toBeGreaterThan(xOf("WF-L-15"));
    // Long titles survive intact for accessible labels.
    const long = graph.nodes.find((node) => node.id === "WF-L-01");
    expect(long?.title.length ?? 0).toBeGreaterThan(60);
    // Node text widths honor the 220-280px implementation default.
    expect(NODE_WIDTH).toBeGreaterThanOrEqual(220);
    expect(NODE_WIDTH).toBeLessThanOrEqual(280);
  });

  test("flow dependencies point down while parallel tasks share a row", () => {
    const fixture = thirtyFixture();
    const graph = buildFlowGraph(fixture);
    const layout = layoutTopDownFlowGraph(graph, fixture.memberIds);
    for (const edge of graph.edges) {
      expect(layout.positions[edge.from]!.y).toBeLessThan(layout.positions[edge.to]!.y);
    }
    for (const layer of layout.layers) {
      expect(new Set(layer.map((id) => layout.positions[id]!.y)).size).toBe(1);
    }
  });

  test("flow hides transitively redundant arrows without losing reachability", () => {
    const graph = buildFlowGraph(thirtyFixture());
    const essential = essentialFlowEdges(graph);
    expect(essential.length).toBeLessThan(graph.edges.length);
    const reachable = (edges: typeof graph.edges, from: string, to: string): boolean => {
      const pending = [from];
      const seen = new Set<string>();
      while (pending.length > 0) {
        const id = pending.pop()!;
        if (id === to) return true;
        if (seen.has(id)) continue;
        seen.add(id);
        pending.push(...edges.filter((edge) => edge.from === id).map((edge) => edge.to));
      }
      return false;
    };
    for (const edge of graph.edges) expect(reachable(essential, edge.from, edge.to)).toBe(true);
  });

  test("flow unloaded members stay inspectable", () => {
    const chain = chainFixture();
    const graph = buildFlowGraph({
      memberIds: [...chain.memberIds, "WUX-T-999"],
      tasks: chain.tasks,
      runs: chain.runs,
    });
    const missing = graph.nodes.find((node) => node.id === "WUX-T-999");
    expect(missing?.missingDetail).toBe(true);
    expect(missing?.state).toBe("unknown");
    expect(graph.warnings.some((warning) => warning.kind === "unavailable")).toBe(true);
  });
});
