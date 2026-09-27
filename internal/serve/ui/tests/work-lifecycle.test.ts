import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { applyFilters, EMPTY_FILTERS } from "../src/features/work/work-utils";
import { deriveNeeds } from "../src/features/inbox/deriveNeeds";

const task = { id:"APP-T-1", title:"One", epicId:"APP", epicTitle:"App", status:"ready", readiness:"ready", priority:"p1", risk:"medium", hasGate:false, updatedAt:"2026-01-01T00:00:00Z" } as const;
const run = { taskId:task.id, taskTitle:task.title, projectId:"app", runner:"codex", model:"", lane:"execute", leaseState:"held", leaseStateRaw:"running", processRunning:true, outcome:"running", elapsedSec:1, sinceLastEventSec:1, liveness:"fresh", attemptCount:1, terminal:false } as const;

test("historical failure attempts deduplicate to one need", () => {
  const failed = {...run, leaseStateRaw:"failed", outcome:"failed" as const, terminal:true};
  const needs = deriveNeeds({ capsules:[task], details:{}, runs:[failed, {...failed, attemptCount:2}] });
  expect(needs.filter((n) => n.taskId === task.id && n.kind === "failed")).toHaveLength(1);
});

test("active work hides discarded tombstones and the explicit history filter reveals them", () => {
  const discarded = { ...task, id: "APP-T-2", rawStatus: "cancelled" };
  expect(applyFilters([task, discarded], EMPTY_FILTERS).map((item) => item.id)).toEqual([task.id]);
  expect(applyFilters([task, discarded], { ...EMPTY_FILTERS, visibility: "discarded" }).map((item) => item.id)).toEqual([discarded.id]);
});

test("the task board columns follow the server state", () => {
  const screen = readFileSync(new URL("../src/features/product/TaskScreens.tsx", import.meta.url), "utf8");
  expect(screen).not.toContain("useRuns(projectId)");
  expect(screen).toContain("boardGroups");
  expect(screen).toContain("state.state");
});
