import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { applyFilters, EMPTY_FILTERS, projectLiveExecution } from "../src/features/work/work-utils";
import { deriveNeeds } from "../src/features/inbox/deriveNeeds";

const task = { id:"APP-T-1", title:"One", epicId:"APP", epicTitle:"App", status:"ready", readiness:"ready", priority:"p1", risk:"medium", hasGate:false, updatedAt:"2026-01-01T00:00:00Z" } as const;
const run = { taskId:task.id, taskTitle:task.title, projectId:"app", runner:"codex", model:"", lane:"execute", leaseState:"held", leaseStateRaw:"running", processRunning:true, outcome:"running", elapsedSec:1, sinceLastEventSec:1, liveness:"fresh", attemptCount:1, terminal:false } as const;

test("fresh ownership projects ready work into In progress without changing durable state", () => {
  const [projected] = projectLiveExecution([task], [run]);
  expect(projected.status).toBe("in_progress");
  expect(projected.rawStatus).toBe("ready");
  expect(projected.liveRun).toBe(true);
});

test("stale, interrupted, failed, and submitted runs leave In progress but retain attempt outcome", () => {
  for (const variant of [
    {...run, liveness:"stale" as const},
    {...run, leaseStateRaw:"interrupted", outcome:"interrupted" as const, terminal:true},
    {...run, leaseStateRaw:"failed", outcome:"failed" as const, terminal:true},
    {...run, leaseStateRaw:"submitted", outcome:"succeeded" as const, terminal:true},
  ]) {
    const [projected] = projectLiveExecution([task], [variant]);
    expect(projected.status).toBe("ready");
    expect(projected.liveRun).toBe(false);
    expect(projected.latestAttemptOutcome).toBe(variant.outcome);
  }
});

test("historical failure attempts deduplicate to one need and never imply active work", () => {
  const failed = {...run, leaseStateRaw:"failed", outcome:"failed" as const, terminal:true};
  const needs = deriveNeeds({ capsules:[task], details:{}, runs:[failed, {...failed, attemptCount:2}] });
  expect(needs.filter((n) => n.taskId === task.id && n.kind === "failed")).toHaveLength(1);
  expect(projectLiveExecution([task], [failed])[0].liveRun).toBe(false);
});

test("active work hides discarded tombstones and the explicit history filter reveals them", () => {
  const discarded = { ...task, id: "APP-T-2", rawStatus: "cancelled" };
  expect(applyFilters([task, discarded], EMPTY_FILTERS).map((item) => item.id)).toEqual([task.id]);
  expect(applyFilters([task, discarded], { ...EMPTY_FILTERS, visibility: "discarded" }).map((item) => item.id)).toEqual([discarded.id]);
});

test("the task board promotes fresh runtime work into a Working now lane", () => {
  const screen = readFileSync(new URL("../src/features/product/TaskScreens.tsx", import.meta.url), "utf8");
  expect(screen).toContain("useRuns(projectId)");
  expect(screen).toContain("projectLiveExecution(tasks.data ?? [], runs.data ?? [])");
  expect(screen).toContain('return status === "in_progress" ? "Working now"');
});
