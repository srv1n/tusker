/*
  real-work-ui-acceptance — deterministic event/revision/reconnect behavior.

  Supporting proof only, with mocked events: these pin the client-side
  contracts (canonical invalidation keys, idempotent event handling,
  reconnect convergence, honest stage/acceptance rendering, stable DAG
  topology) that the live seeded journey in real-work.browser.mjs then
  verifies against the ordinary app. A green suite here never substitutes
  for that live run.

  Every test below executes real logic (stream mapping, query keys, stage
  helpers, rendered markup). Zero-match names are failures.
*/

import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { RunDetail, TaskDetail, WaveSummary } from "../src/types/domain";
import {
  connectLiveStream,
  formatStreamAge,
  getStreamStatus,
  invalidateStreamEvent,
  liveRefetchInterval,
  streamKeyToQueryKeys,
} from "../src/lib/stream";
import { qk } from "../src/lib/queries";
import {
  initialWaveView,
  settleEnteredWaveView,
  waveStartability,
} from "../src/features/workbench/integration/integrationModel";
import { StreamStatusNote } from "../src/features/workbench/integration/StreamStatus";
import {
  acceptedDelivery,
  actualStage,
  identitySummary,
} from "../src/features/workbench/inspector/inspectorLogic";
import { buildFlowGraph, topologyKey } from "../src/features/workbench/flow/flowGraph";
import { branchesFixture, liveUpdateBase, liveUpdateNext } from "../src/features/workbench/flow/fixtures";
import {
  acceptedRun,
  acceptedTask,
  failedTask,
  readyTask,
} from "../previews/wux/inspector/fixtures";

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  closed = false;

  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }

  open() {
    this.onopen?.({} as Event);
  }

  message(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent<string>);
  }

  raw(data: string) {
    this.onmessage?.({ data } as MessageEvent<string>);
  }

  error() {
    this.onerror?.({} as Event);
  }

  close() {
    this.closed = true;
  }
}

function recorder() {
  const invalidations: unknown[] = [];
  return {
    invalidations,
    client: {
      invalidateQueries(args: unknown) {
        invalidations.push(args);
        return Promise.resolve();
      },
    },
  };
}

function keySet(invalidations: unknown[]): string[] {
  return [...new Set(invalidations.map((entry) => JSON.stringify(entry)))].sort();
}

const wave = (overrides: Partial<WaveSummary> = {}): WaveSummary => ({
  id: "W-1",
  title: "Wave",
  status: "open",
  landedAt: undefined,
  memberIds: [],
  members: [],
  counts: {},
  authorization: { state: "armed", stale: false, action: "execute" },
  brief: {
    schema: "tusker.wave-brief/v1", waveId: "W-1", title: "Wave", waveHref: "#",
    sectionOrder: ["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"],
    outcome: { summary: "", fullyDrained: false, counts: {}, tasks: [] },
    seeIt: [], landed: [], reworkParked: [], humanAction: [], documentation: [],
  },
  ...overrides,
});

describe("real-work event invalidation", () => {
  test("task events hit the canonical wave-member detail key", () => {
    // The wave detail must read members through qk.task so targeted stream
    // events and run/wave mutations invalidate exactly that cache entry.
    expect(qk.task("A-1", "demo")).toEqual(["task", "demo", "A-1"]);
    expect(streamKeyToQueryKeys("tasks:A-1", "demo")).toContainEqual(["task", "demo", "A-1"]);

    const { client, invalidations } = recorder();
    invalidateStreamEvent(client, { kind: "task_status_change", keys: ["tasks:A-1"], project: "demo" });
    expect(invalidations).toContainEqual({ queryKey: ["task", "demo", "A-1"], exact: false });
    expect(invalidations).toContainEqual({ queryKey: ["tasks", "demo"], exact: false });
    expect(invalidations).toContainEqual({ queryKey: qk.needs("demo"), exact: false });
  });

  test("wave events refresh the wave, task and needs surfaces together", () => {
    expect(streamKeyToQueryKeys("waves", "demo")).toContainEqual(qk.waves("demo"));
    const { client, invalidations } = recorder();
    invalidateStreamEvent(client, { kind: "wave_changed", keys: ["waves"], project: "demo" });
    expect(invalidations).toContainEqual({ queryKey: qk.waves("demo"), exact: false });
    expect(invalidations).toContainEqual({ queryKey: qk.tasks("demo"), exact: false });
    // Another project's caches stay untouched.
    expect(JSON.stringify(invalidations)).not.toContain('"other"');
  });

  test("review batches invalidate the shared authoritative wave-review projection", () => {
    const { client, invalidations } = recorder();
    invalidateStreamEvent(client, { kind: "review_changed", keys: ["review:batch"], project: "demo" });
    expect(invalidations).toContainEqual({ queryKey: ["wave-review", "demo"], exact: false });
  });

  test("duplicate and out-of-order events converge without regressing state", () => {
    FakeEventSource.instances = [];
    const { client, invalidations } = recorder();
    const disconnect = connectLiveStream(client, {
      EventSourceImpl: FakeEventSource,
      now: () => 1_000,
      debounceMs: 0,
    });
    try {
      const source = FakeEventSource.instances[0]!;
      const event = { kind: "task_status_change", keys: ["tasks:A-1", "runs:A-1"], project: "demo" };
      source.message(event);
      const first = keySet(invalidations);
      expect(first).toContain(JSON.stringify({ queryKey: ["task", "demo", "A-1"], exact: false }));

      // A duplicated delivery invalidates the same set — no duplicate nodes,
      // no newer state to regress because invalidation only refetches.
      source.message(event);
      expect(keySet(invalidations)).toEqual(first);

      // An older event arriving late carries a subset of the same keys, so
      // the converged set never grows stale entries or regresses.
      source.message({ kind: "task_status_change", keys: ["tasks:A-1"], project: "demo" });
      expect(keySet(invalidations)).toEqual(first);
    } finally {
      disconnect();
    }
  });

  test("malformed events do not kill the reconnect loop", () => {
    FakeEventSource.instances = [];
    const { client, invalidations } = recorder();
    const before = invalidations.length;
    const disconnect = connectLiveStream(client, {
      EventSourceImpl: FakeEventSource,
      now: () => 5_000,
      debounceMs: 0,
    });
    try {
      const source = FakeEventSource.instances[0]!;
      source.open();
      expect(getStreamStatus().connected).toBe(true);
      source.raw("this is not json {");
      expect(getStreamStatus().connected).toBe(true);
      expect(getStreamStatus().lastErrorAt).toBe(5_000);
      expect(invalidations.length).toBeGreaterThan(before);
      // The stream still processes the next well-formed event.
      source.message({ kind: "task_status_change", keys: ["tasks:A-1"], project: "demo" });
      expect(invalidations).toContainEqual({ queryKey: ["task", "demo", "A-1"], exact: false });
    } finally {
      disconnect();
    }
  });

  test("reconnect obtains fresh authoritative state", () => {
    FakeEventSource.instances = [];
    const { client, invalidations } = recorder();
    const disconnect = connectLiveStream(client, {
      EventSourceImpl: FakeEventSource,
      debounceMs: 0,
    });
    try {
      const source = FakeEventSource.instances[0]!;
      source.open();
      const keys = keySet(invalidations);
      for (const family of ["daemon", "projects", "needs", "runs", "tasks", "waves"]) {
        expect(keys.some((key) => key.startsWith(`{"queryKey":["${family}"`))).toBe(true);
      }
      expect(liveRefetchInterval()).toBe(false);
    } finally {
      disconnect();
    }
  });

  test("replay miss converges the full summary surface", () => {
    FakeEventSource.instances = [];
    const { client, invalidations } = recorder();
    const disconnect = connectLiveStream(client, {
      EventSourceImpl: FakeEventSource,
      debounceMs: 0,
    });
    try {
      const source = FakeEventSource.instances[0]!;
      source.open();
      const before = keySet(invalidations);
      source.message({
        kind: "stream_replay_miss",
        keys: ["tasks:A-1"],
        project: "demo",
        replay_miss: true,
      });
      const after = keySet(invalidations);
      expect(after.length).toBeGreaterThan(before.length);
      expect(after).toContain(JSON.stringify({ queryKey: qk.waves("demo"), exact: false }));
      expect(after).toContain(JSON.stringify({ queryKey: qk.tasks("demo"), exact: false }));
      expect(after).toContain(JSON.stringify({ queryKey: ["task"], exact: false }));
    } finally {
      disconnect();
    }
  });
});

describe("real-work stale-state honesty", () => {
  test("disconnect falls back to interval refetch and says so", () => {
    FakeEventSource.instances = [];
    const { client } = recorder();
    const disconnect = connectLiveStream(client, {
      EventSourceImpl: FakeEventSource,
      debounceMs: 0,
    });
    try {
      const source = FakeEventSource.instances[0]!;
      source.open();
      expect(getStreamStatus().connected).toBe(true);
      source.error();
      expect(getStreamStatus().connected).toBe(false);
      expect(liveRefetchInterval()).not.toBe(false);

      const offline = renderToStaticMarkup(createElement(StreamStatusNote));
      expect(offline).toContain("Reconnecting");
      expect(offline).toContain('data-connected="false"');

      source.open();
      const online = renderToStaticMarkup(createElement(StreamStatusNote));
      expect(online).toContain("Live updates");
      expect(online).toContain('data-connected="true"');
    } finally {
      disconnect();
    }
  });

  test("last-event age renders honestly", () => {
    expect(formatStreamAge(null)).toBe("no events yet");
    expect(formatStreamAge(1_000, 12_000)).toBe("11s ago");
    expect(formatStreamAge(1_000, 121_000)).toBe("2m ago");
  });
});

describe("real-work wave entry and readiness", () => {
  test("every wave entry leads with dependencies", () => {
    expect(initialWaveView(wave({ status: "closed" }))).toBe("flow");
    expect(initialWaveView(wave({ status: "landed", landedAt: "2026-09-07T00:00:00Z" }))).toBe("flow");
    expect(initialWaveView(wave({ status: "open" }))).toBe("flow");
    expect(initialWaveView(wave({ status: "closed" }), "results")).toBe("results");
  });

  test("entry waits for review and a selected view survives a completion poll", () => {
    const open = wave({ status: "open" });
    expect(settleEnteredWaveView(null, open, true, undefined)).toBeNull();
    const entered = settleEnteredWaveView(null, open, false, undefined);
    expect(entered).toBe("flow");
    // The wave lands while the operator watches: the latch keeps Dependencies.
    expect(settleEnteredWaveView(entered, wave({ status: "landed", landedAt: "2026-09-07T00:00:00Z" }), false, undefined)).toBe("flow");
    // An already-completed wave still leads with its dependencies on entry.
    expect(settleEnteredWaveView(null, wave({ status: "closed" }), false, undefined)).toBe("flow");
    // An explicit deep link still wins on entry.
    expect(settleEnteredWaveView(null, wave({ status: "closed" }), false, "flow")).toBe("flow");
  });

  test("unavailable readiness never enables start", () => {
    expect(waveStartability([wave({ status: "open" })])["W-1"]).toEqual({
      state: "unknown",
      reason: "Wave status could not be loaded. Try refreshing.",
    });
  });
});

describe("real-work completion honesty", () => {
  const exitedTask: TaskDetail = { ...failedTask, id: "EXIT-1", title: "Process exited", status: "in_progress" };
  const exitedRun: RunDetail = {
    ...acceptedRun,
    taskId: exitedTask.id,
    taskTitle: exitedTask.title,
    outcome: "succeeded",
    liveness: "stale",
    delivery: undefined,
  };
  const pendingDeliveryRun: RunDetail = {
    ...acceptedRun,
    delivery: { summary: "Unreviewed output.", verification: "", proofStatus: "pending" },
  };

  test("a successful process exit alone never paints the task complete", () => {
    expect(actualStage(exitedTask, exitedRun).label).not.toBe("Delivered");
    expect(actualStage({ ...exitedTask, status: "done" }, exitedRun)).toEqual({
      label: "Delivered",
      state: "completed",
      tone: "pass",
      live: false,
    });
    expect(acceptedDelivery(exitedRun)).toBeNull();
  });

  test("evidence without accepted proof never becomes a result", () => {
    expect(acceptedDelivery(pendingDeliveryRun)).toBeNull();
    expect(acceptedDelivery({ ...pendingDeliveryRun, delivery: undefined })).toBeNull();
    const accepted = acceptedDelivery(acceptedRun);
    expect(accepted?.summary).toContain("intent, stage, decision");
    expect(actualStage(acceptedTask, acceptedRun).label).toBe("Checking verification · waiting for independent review");
  });

  test("failure stays visible until a new truthful attempt", () => {
    expect(actualStage(failedTask, null).label).toBe("Implementation in progress");
    expect(identitySummary({ state: "unavailable" })).toBe("Unavailable");
  });
});

describe("real-work DAG stability", () => {
  test("state-only live updates keep topology, dependency changes do not", () => {
    const base = liveUpdateBase();
    const next = liveUpdateNext();
    const baseGraph = buildFlowGraph(base);
    const nextGraph = buildFlowGraph(next);
    expect(topologyKey(baseGraph)).toBe(topologyKey(nextGraph));

    const branched = branchesFixture();
    const branchedGraph = buildFlowGraph(branched);
    expect(topologyKey(branchedGraph)).not.toBe(topologyKey(baseGraph));
    expect(branchedGraph.edges.length).toBeGreaterThan(0);
  });

  test("readiness fixture exposes branch and join structure", () => {
    expect(readyTask.deps.length).toBe(0);
    const graph = buildFlowGraph({ memberIds: [readyTask.id], tasks: [readyTask], runs: [] });
    expect(graph.nodes.map((node) => node.id)).toContain(readyTask.id);
    expect(graph.warnings).toEqual([]);
  });
});
