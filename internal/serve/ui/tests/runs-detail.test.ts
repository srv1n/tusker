import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { EventTail } from "../src/features/runs/detail/EventTail";
import {
  isLiveHeaderRun,
  runStats,
} from "../src/features/runs/detail/helpers";
import {
  interruptedRunReadbackComplete,
  invalidateRunActionQueries,
  runRefetchInterval,
} from "../src/lib/queries";
import { createRunActionLock } from "../src/features/runs/detail/actionLock";
import type { RunDetail } from "../src/types/domain";

const completedRun = {
  taskId: "TRC-T-0002",
  taskTitle: "Completed trace run",
  projectId: "tusker",
  runner: "codex",
  model: "gpt-5.4-codex",
  lane: "execute",
  leaseState: "unclaimed",
  outcome: "succeeded",
  elapsedSec: 886,
  sinceLastEventSec: 6,
  liveness: "fresh",
  attemptCount: 1,
  terminal: true,
  workspacePath: "/tmp/TRC-T-0002",
  attempts: [
    {
      n: 1,
      outcome: "succeeded",
      durationSec: 886,
      startedAt: "2026-07-08T03:00:00Z",
    },
  ],
  events: [],
} satisfies RunDetail;

test("runs-detail header renders operator state instead of treating silence as death", () => {
  const source = readFileSync("src/features/runs/detail/RunHeader.tsx", "utf8");

  expect(isLiveHeaderRun(completedRun)).toBe(false);
  expect(source).toContain("run.operatorState.state");
  expect(source).not.toContain("<LivenessIndicator");
});

test("runs-detail stats freeze released elapsed without presenting usage totals", () => {
  expect(runStats(completedRun)).toEqual([
    { label: "Elapsed", value: "14m 46s" },
    { label: "Attempts", value: "1" },
    { label: "Liveness", value: "fresh" },
  ]);
});

test("runs-detail header still marks a held running run as live", () => {
  const runningRun = {
    ...completedRun,
    leaseState: "held",
    leaseStateRaw: "running",
    processRunning: true,
    outcome: "running",
  } satisfies RunDetail;

  expect(isLiveHeaderRun(runningRun)).toBe(true);
  // 834e84a4 predated W-0044 server capabilities; interrupt eligibility now comes from the server.
  expect(interruptedRunReadbackComplete(runningRun)).toBe(false);
});

test("runs-detail waits for canonical interrupted lease and stopped process", () => {
  const interrupted = {
    ...completedRun,
    leaseState: "released",
    leaseStateRaw: "interrupted",
    processRunning: false,
    outcome: "interrupted",
  } satisfies RunDetail;

  expect(interruptedRunReadbackComplete({ ...interrupted, processRunning: true })).toBe(false);
  expect(interruptedRunReadbackComplete({ ...interrupted, leaseStateRaw: "running" })).toBe(false);
  expect(interruptedRunReadbackComplete(interrupted)).toBe(true);
});

test("runs-detail polling stops on query error and otherwise follows readback state", () => {
  const waitingForReadback = {
    ...completedRun,
    leaseStateRaw: "interrupted",
    processRunning: true,
  } satisfies RunDetail;
  const readbackComplete = {
    ...waitingForReadback,
    processRunning: false,
  } satisfies RunDetail;

  expect(runRefetchInterval(true, waitingForReadback, false, 45_000)).toBe(400);
  expect(runRefetchInterval(true, readbackComplete, false, 45_000)).toBe(45_000);
  expect(runRefetchInterval(false, waitingForReadback, false, false)).toBe(false);
  expect(runRefetchInterval(true, waitingForReadback, true, 45_000)).toBe(false);
  expect(runRefetchInterval(false, completedRun, false, 45_000, true)).toBe(400);
});

test("run action invalidation waits for every canonical query refetch", async () => {
  const releases: Array<() => void> = [];
  const calls: unknown[] = [];
  const qc = {
    invalidateQueries: (filters: unknown) => {
      calls.push(filters);
      return new Promise<void>((resolve) => releases.push(resolve));
    },
  };
  let settled = false;

  const invalidation = invalidateRunActionQueries(qc, "SRV-T-0021").then(() => {
    settled = true;
  });
  await Promise.resolve();
  expect(calls).toEqual([
    { queryKey: ["run", "all", "SRV-T-0021"] },
    { queryKey: ["runs"] },
    { queryKey: ["tasks"] },
  ]);
  expect(settled).toBe(false);

  releases.forEach((release) => release());
  await invalidation;
  expect(settled).toBe(true);
});

test("running message feed refreshes even with a connected event stream", () => {
  const running = { ...completedRun, terminal: false, leaseState: "held" } satisfies RunDetail;
  expect(runRefetchInterval(false, running, false, false)).toBe(2_000);
  expect(runRefetchInterval(false, running, true, false)).toBe(false);
});

test("message feed shows readable escaped content instead of transport noise", () => {
  const html = renderToStaticMarkup(createElement(EventTail, {
    events: [
      { ts: "2026-09-22T12:00:00Z", kind: "acp_session_update_observed", text: "transport noise" },
      { ts: "2026-09-22T12:00:01Z", kind: "agent_message", text: "Running fixture tests\n<script>alert(1)</script>", activity: true },
      { ts: "", kind: "tool_call", text: "cargo test\nin_progress", activity: true },
    ],
    liveness: "fresh",
    sinceLastEventSec: 2,
  }));
  expect(html).toContain("Recent messages");
  expect(html).toContain("Running fixture tests");
  expect(html).toContain("cargo test");
  expect(html).toContain("Time unavailable");
  expect(html).toContain("&lt;script&gt;");
  expect(html).not.toContain("<script>");
  expect(html).not.toContain("transport noise");
});

test("legacy message capture does not pretend a fresh heartbeat is progress", () => {
  const html = renderToStaticMarkup(createElement(EventTail, { events: [], liveness: "fresh", sinceLastEventSec: 1 }));
  expect(html).toContain("No messages captured for this attempt");
  expect(html).toContain("Older ACP runs");
});

test("runs-detail interrupt confirms, guards double fire, and polls canonical readback", () => {
  const detail = readFileSync("src/features/runs/RunDetail.tsx", "utf8");
  const header = readFileSync("src/features/runs/detail/RunHeader.tsx", "utf8");
  const queries = readFileSync("src/lib/queries.ts", "utf8");

  expect(detail).toContain("await confirm({");
  expect(detail).toContain('!runActionLock.tryAcquire("interrupt")');
  expect(detail).toContain("interrupt.mutate(undefined, {");
  expect(detail).toContain("<TaskRunDetail key={taskId}");
  expect(detail).toContain('!runActionLock.tryAcquire("redrive")');
  expect(detail).toContain("redrive.mutate(undefined, {");
  // 834e84a4 used local run state; W-0044 makes interrupt presence server declared.
  expect(header).toContain('interruptCapability?.available &&');
  expect(header).toContain('disabled={actionBusy}');
  expect(header).toContain("interrupt.result.reason");
  expect(header).toContain("interrupt.error");
  expect(queries).toContain('mutationKey: ["interrupt", taskId]');
  expect(queries).toContain('mutationKey: ["redrive", taskId]');
});

test("runs-detail action lock prevents interrupt and redrive from overlapping", () => {
  const lock = createRunActionLock();

  expect(lock.tryAcquire("redrive")).toBe(true);
  expect(lock.active()).toBe("redrive");
  expect(lock.tryAcquire("interrupt")).toBe(false);
  lock.release("interrupt");
  expect(lock.active()).toBe("redrive");
  lock.release("redrive");

  expect(lock.tryAcquire("interrupt")).toBe(true);
  expect(lock.tryAcquire("redrive")).toBe(false);
  lock.release("interrupt");
  expect(lock.active()).toBe(null);
});

test("run inspector separates ownership, resume, delivery, and bounded failure", () => {
  const detail = readFileSync("src/features/runs/RunDetail.tsx", "utf8");
  expect(detail).toContain("data-run-operator-facts");
  expect(detail).toContain("run.authorization.source");
  expect(detail).toContain("relativeTime(run.authorization.created_at)");
  expect(detail).toContain("run.identity?.repo_root");
  expect(detail).toContain("run.session?.session_ref");
  expect(detail).toContain("Copy resume command");
  expect(detail).toContain("Resume unavailable:");
  expect(detail).toContain("data-run-delivery");
  expect(detail).toContain("No deliverable recorded.");
  expect(detail).toContain("acceptance verification");
  expect(detail).toContain("line-clamp-3");
  expect(detail).not.toContain("token");
});

test("run inspector links to the current task contract", () => {
  const header = readFileSync("src/features/runs/detail/RunHeader.tsx", "utf8");
  const docShell = readFileSync("src/features/docs/DocShell.tsx", "utf8");

  expect(header).toContain('to="/p/$projectId/docs"');
  expect(header).toContain("params={{ projectId: run.projectId }}");
  expect(header).toContain("search={{ path: run.taskId }}");
  expect(header).toContain("View ticket");
  expect(docShell).toContain('to="/p/$projectId"');
  expect(docShell).toContain("<ArrowLeft");
});
