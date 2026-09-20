import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { WaveOverview } from "../src/features/workbench/overview/WaveOverview";
import {
  filterGroupedWaves,
  groupWaves,
  type Startability,
} from "../src/features/workbench/overview/groupWaves";
import {
  humanActionEntry,
  makeRun,
  makeTask,
  makeWave,
} from "../src/features/workbench/overview/previewFixtures";
import type { TaskCapsule, WaveSummary } from "../src/types/domain";

const START: Record<string, Startability> = {
  "W-NEED": { state: "blocked", reason: "Sample: waiting on a human decision." },
  "W-RUN": { state: "blocked", reason: "Sample: work is underway." },
  "W-READY": { state: "ready", reason: "Sample: readiness permits start." },
  "W-UNKNOWN": { state: "unknown", reason: "Sample: readiness read failed." },
  "W-PAUSED": { state: "blocked", reason: "Sample: paused." },
  "W-DRAINED": { state: "unknown", reason: "Sample: predicate missing." },
  "W-DONE": { state: "unknown", reason: "Sample: terminal wave." },
  "W-CANCELLED": { state: "unknown", reason: "Sample: terminal wave." },
  "W-STALE": { state: "unknown", reason: "Sample: stale read." },
};

function mixedInput() {
  const waves: WaveSummary[] = [
    makeWave({
      id: "W-NEED",
      title: "Needs decision",
      memberIds: ["T-1", "T-2"],
      brief: {
        schema: "tusker.wave-brief/v1",
        waveId: "W-NEED",
        title: "Needs decision",
        waveHref: "",
        sectionOrder: ["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"],
        outcome: { summary: "", fullyDrained: false, counts: {}, tasks: [] },
        seeIt: [],
        landed: [],
        reworkParked: [],
        humanAction: [humanActionEntry("G-1", ["T-1"])],
        documentation: [],
      },
    }),
    makeWave({ id: "W-RUN", title: "In flight", memberIds: ["T-3"] }),
    makeWave({ id: "W-READY", title: "Ready wave", memberIds: ["T-4"] }),
    makeWave({ id: "W-UNKNOWN", title: "Unknown start", memberIds: ["T-5"] }),
    makeWave({
      id: "W-PAUSED",
      title: "Paused wave",
      memberIds: ["T-6"],
      authorization: { state: "paused", stale: false, action: "Sample: paused by owner." },
    }),
    makeWave({
      id: "W-DRAINED",
      title: "Drained but unlanded",
      memberIds: ["T-7"],
      brief: {
        schema: "tusker.wave-brief/v1",
        waveId: "W-DRAINED",
        title: "Drained but unlanded",
        waveHref: "",
        sectionOrder: ["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"],
        outcome: { summary: "", fullyDrained: true, counts: {}, tasks: [] },
        seeIt: [],
        landed: [],
        reworkParked: [],
        humanAction: [],
        documentation: [],
      },
    }),
    makeWave({ id: "W-DONE", title: "Shipped", memberIds: ["T-8"], landedAt: "2026-09-06T10:00:00Z" }),
    makeWave({ id: "W-CANCELLED", title: "Dropped", status: "cancelled", memberIds: ["T-9"] }),
    makeWave({
      id: "W-STALE",
      title: "Stale read",
      memberIds: ["T-10"],
      authorization: { state: "stale", stale: true, action: "" },
    }),
  ];
  const tasks: TaskCapsule[] = [
    makeTask({ id: "T-1", title: "Gate task", hasGate: true }),
    makeTask({ id: "T-2", title: "Second task" }),
    makeTask({ id: "T-3", title: "Running task", status: "in_progress" }),
    makeTask({ id: "T-4", title: "Ready task" }),
    makeTask({ id: "T-5", title: "Planned task" }),
    makeTask({ id: "T-6", title: "Paused task" }),
    makeTask({ id: "T-7", title: "Done task", status: "done" }),
    makeTask({ id: "T-8", title: "Landed task", status: "done" }),
    makeTask({ id: "T-9", title: "Ended task", status: "review" }),
    makeTask({ id: "T-10", title: "Stale task", status: "in_progress" }),
  ];
  const runs = [
    makeRun({ taskId: "T-3" }),
    // Stale execution facts must not read as running.
    makeRun({ taskId: "T-10", liveness: "stale", sinceLastEventSec: 900 }),
  ];
  return { waves, tasks, runs };
}

function groupOf(result: ReturnType<typeof groupWaves>, waveId: string) {
  for (const group of result.groups) {
    if (group.waves.some((entry) => entry.wave.id === waveId)) return group.id;
  }
  throw new Error(`wave ${waveId} missing from partition`);
}

describe("wave overview grouping", () => {
  test("overview partitions mixed states", () => {
    const result = groupWaves({ ...mixedInput(), startability: START });
    const seen = result.groups.flatMap((group) =>
      group.waves.map((entry) => entry.wave.id),
    );
    expect(new Set(seen).size).toBe(seen.length);
    expect(seen.length).toBe(9);
    expect(groupOf(result, "W-NEED")).toBe("needs-you");
    expect(groupOf(result, "W-RUN")).toBe("running");
    expect(groupOf(result, "W-READY")).toBe("ready");
    expect(groupOf(result, "W-UNKNOWN")).toBe("unavailable");
    expect(groupOf(result, "W-PAUSED")).toBe("planned");
    expect(groupOf(result, "W-DONE")).toBe("completed");
    expect(groupOf(result, "W-CANCELLED")).toBe("completed");
    expect(groupOf(result, "W-STALE")).toBe("unavailable");
  });

  test("overview unknown is not ready", () => {
    const result = groupWaves({ ...mixedInput(), startability: START });
    expect(groupOf(result, "W-UNKNOWN")).not.toBe("ready");
    const unavailable = result.groups.find((group) => group.id === "unavailable");
    const entry = unavailable?.waves.find((item) => item.wave.id === "W-UNKNOWN");
    expect(entry?.stateDetail).toContain("Start status unavailable");
    expect(entry?.row.primaryAction).toBe("open");
    expect(entry?.row.secondary?.detail).toContain("read failed");
    // Unknown startability never leaks into completed either.
    expect(groupOf(result, "W-DRAINED")).not.toBe("completed");
  });

  test("overview unavailable section preserves every wave", () => {
    const { waves, tasks, runs } = mixedInput();
    const staleWaves = waves.map((wave) => ({
      ...wave,
      authorization: { ...wave.authorization, state: "stale" as const, stale: true },
    }));
    const result = groupWaves({ waves: staleWaves, tasks, runs, startability: {} });
    const unavailable = result.groups.find((group) => group.id === "unavailable");
    // Completed waves stay completed history; every other wave is preserved
    // in the exceptional section instead of vanishing or succeeding.
    expect(unavailable?.waves.length).toBe(7);
    const completed = result.groups.find((group) => group.id === "completed");
    expect(completed?.waves.length).toBe(2);
    const total = result.groups.reduce((n, group) => n + group.waves.length, 0);
    expect(total).toBe(waves.length);
  });

  test("overview completed is not drained", () => {
    const result = groupWaves({ ...mixedInput(), startability: START });
    // fullyDrained with no landedAt or terminal status is not completion.
    expect(groupOf(result, "W-DRAINED")).toBe("unavailable");
    // Cancellation is history, never a successful completion label.
    const completed = result.groups.find((group) => group.id === "completed");
    const cancelled = completed?.waves.find((entry) => entry.wave.id === "W-CANCELLED");
    expect(cancelled?.stateLabel).toBe("Cancelled");
    expect(cancelled?.stateLabel).not.toBe("Completed");
    expect(completed?.title).toBe("History");
  });

  test("overview preserves actionable gates, fresh running, and closeout facts", () => {
    const input = mixedInput();
    const result = groupWaves({
      ...input,
      runs: [...input.runs, makeRun({ taskId: "T-1" })],
      startability: START,
    });
    const needsYou = result.groups.find((group) => group.id === "needs-you")?.waves[0];
    expect(needsYou?.stateDetail).toContain("Sample decision for G-1");
    expect(needsYou?.stateDetail).toContain("Running.");
    expect(needsYou?.row).toEqual({
      primaryAction: "open",
      secondary: {
        kind: "human-action",
        text: "Sample decision for G-1 Running.",
        detail: "Sample decision for G-1",
      },
    });

    const gateWave = makeWave({ id: "W-GATE", title: "Choose region", memberIds: ["T-GATE"] });
    const gateTask = makeTask({
      id: "T-GATE",
      title: "Choose staging region",
      hasGate: true,
      openGates: [{
        id: "G-REGION", kind: "clarify", rawKind: "clarify", title: "Choose region",
        status: "open", owner: "human", satisfied: false, blocking: true, blocks: [],
        action: "Choose the staging region.",
      }],
    });
    const gated = groupWaves({
      waves: [gateWave], tasks: [gateTask], runs: [], startability: { "W-GATE": { state: "blocked", reason: "Waiting on gate." } },
    });
    expect(groupOf(gated, "W-GATE")).toBe("needs-you");
    expect(gated.groups[0]?.waves[0]?.row.secondary?.text).toBe("Choose the staging region.");

    const promotion = groupWaves({
      waves: [makeWave({ id: "W-PROMOTE", title: "Promote release", status: "pending_promotion" })],
      tasks: [], runs: [], startability: {},
    });
    const promotionEntry = promotion.groups.find((group) => group.id === "planned")?.waves[0];
    expect(promotionEntry?.stateLabel).toBe("Pending promotion");
    expect(promotionEntry?.row.primaryAction).toBe("open");
    expect(groupOf(promotion, "W-PROMOTE")).not.toBe("completed");
  });

  test("overview distinguishes disarmed, blocked, and stopped from ready or completed", () => {
    const waves = [
      makeWave({ id: "W-DISARMED", title: "New work", authorization: { state: "disarmed", stale: false, action: "" } }),
      makeWave({ id: "W-BLOCKED", title: "Wait on dependency" }),
      makeWave({ id: "W-STOPPED", title: "Stopped work", status: "stopped" }),
    ];
    const result = groupWaves({
      waves, tasks: [], runs: [],
      startability: {
        "W-BLOCKED": { state: "blocked", reason: "Dependency W-01 is incomplete." },
      },
    });
    const disarmed = result.groups.find((group) => group.id === "planned")?.waves.find((wave) => wave.wave.id === "W-DISARMED");
    const blocked = result.groups.find((group) => group.id === "planned")?.waves.find((wave) => wave.wave.id === "W-BLOCKED");
    const stopped = result.groups.find((group) => group.id === "completed")?.waves[0];
    expect(disarmed?.stateLabel).toBe("Not started");
    expect(blocked?.stateLabel).toBe("Blocked");
    expect(blocked?.row.secondary?.detail).toBe("Dependency W-01 is incomplete.");
    expect(stopped?.stateLabel).toBe("Stopped");
    expect(stopped?.stateLabel).not.toBe("Completed");
  });

  test("overview search filters titles and completed hides by default", () => {
    const result = groupWaves({ ...mixedInput(), startability: START });
    const searched = filterGroupedWaves(result.groups, undefined, "ready wave", false);
    expect(searched.flatMap((group) => group.waves).map((entry) => entry.wave.id)).toEqual([
      "W-READY",
    ]);
    const byId = filterGroupedWaves(result.groups, undefined, "w-ready", false);
    expect(byId.flatMap((group) => group.waves).map((entry) => entry.wave.id)).toEqual([
      "W-READY",
    ]);
    const hidden = filterGroupedWaves(result.groups, undefined, "", false);
    expect(hidden.some((group) => group.id === "completed")).toBe(false);
    expect(hidden.flatMap((group) => group.waves).length).toBe(7);
    const shown = filterGroupedWaves(result.groups, undefined, "", true);
    expect(shown.find((group) => group.id === "completed")?.waves.length).toBe(2);
  });

  test("overview counts unassigned tasks without listing them", () => {
    const input = mixedInput();
    input.tasks.push(makeTask({ id: "T-LOOSE", title: "Loose task" }));
    const result = groupWaves({ ...input, startability: START });
    expect(result.unassignedCount).toBe(1);
    expect(result.totalCount).toBe(9);
  });
});

function renderOverview(overrides: {
  query?: string;
  waves?: WaveSummary[];
  tasks?: TaskCapsule[];
  loading?: boolean;
  error?: string;
  descriptions?: Record<string, string>;
  category?: "all" | "needs-you" | "running" | "ready" | "planned" | "completed" | "unavailable";
} = {}) {
  const input = mixedInput();
  return renderToStaticMarkup(
    createElement(WaveOverview, {
      waves: overrides.waves ?? input.waves,
      tasks: overrides.tasks ?? input.tasks,
      runs: input.runs,
      startability: START,
      descriptions: overrides.descriptions,
      query: overrides.query ?? "",
      category: overrides.category ?? "all",
      onQueryChange: () => {},
      onCategoryChange: () => {},
      onOpenWave: () => {},
      onOpenUnassigned: () => {},
      loading: overrides.loading,
      error: overrides.error,
    }),
  );
}

describe("wave overview rendering", () => {
  test("overview renders every wave once with linked titles and no open column", () => {
    const html = renderOverview();
    for (const section of ["Needs you", "Running", "Ready to start", "Planned", "Status unavailable"]) {
      expect(html).toContain(section);
    }
    expect(html).not.toContain("Shipped");
    // Each visible wave title renders exactly once inside its full-card button.
    for (const title of ["Needs decision", "In flight", "Ready wave", "Stale read"]) {
      expect(html.split(`>${title}</span>`).length - 1).toBe(1);
      expect(html).toMatch(new RegExp(`<button[^>]*wux-ov-card[^>]*>[\\s\\S]*>${title}</span>[\\s\\S]*</button>`));
    }
    expect(html).toContain("Open wave Needs decision");
    // No separate Open column and no repeated all-task ticket inventory.
    expect(html).not.toContain(">Open<");
    expect(html).not.toContain("Running task");
  });

  test("overview rows show supplied descriptions and compact progress", () => {
    const html = renderOverview({
      descriptions: { "W-RUN": "Sample: editor refresh in flight." },
    });
    expect(html).toContain("Sample: editor refresh in flight.");
    expect(html).toContain("moving");
    // Waves without a supplied description leave no filler text.
    expect(html).not.toContain("execution summary");
  });

  test("overview search and status filter controls render", () => {
    const searched = renderOverview({ query: "ready wave" });
    expect(searched).toContain("Ready wave");
    expect(searched).not.toContain("In flight");
    // Controlled filter state is reflected, so callbacks retain selection.
    expect(searched).toContain('value="ready wave"');
    expect(searched).toContain('aria-label="Filter waves by status"');
    expect(searched).toContain("All (1)");
    const history = renderOverview({ category: "completed" });
    expect(history).toContain("Shipped");
    expect(history).not.toContain("In flight");
    const input = mixedInput();
    input.tasks.push(makeTask({ id: "T-LOOSE", title: "Loose task" }));
    expect(renderOverview({ tasks: input.tasks })).toContain("Unassigned tasks");
  });

  test("overview demonstrates empty, loading and error states", () => {
    expect(renderOverview({ waves: [], tasks: [] })).toContain("No active waves match.");
    expect(renderOverview({ loading: true })).toContain("Loading wave overview");
    expect(renderOverview({ error: "Sample read failed." })).toContain(
      "Wave overview unavailable.",
    );
  });
});
