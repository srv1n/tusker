import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterContextProvider } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import type { RunDetail, TaskDetail, WaveReviewMember } from "../src/types/domain";
import {
  acceptedDelivery,
  actualStage,
  currentAttemptRecord,
  historicalAttemptRecords,
  latestRunEvent,
  resolveVisibleRun,
  visibleTaskIntent,
} from "../src/features/workbench/inspector/inspectorLogic";
import { acceptedRun, acceptedTask, failedRun, failedTask, readyRun, readyTask, unavailableTask } from "../previews/wux/inspector/fixtures";

// 834e84a4 added a run-detail Link to the inspector; server renders need router context.
function renderWithRouter(element: ReturnType<typeof createElement>) {
  const root = createRootRoute();
  const runRoute = createRoute({ getParentRoute: () => root, path: "/p/$projectId/runs/$taskId" });
  const router = createRouter({ routeTree: root.addChildren([runRoute]), history: createMemoryHistory({ initialEntries: ["/"] }) });
  return renderToStaticMarkup(createElement(RouterContextProvider, { router }, element));
}

function render(task: TaskDetail = readyTask, run: RunDetail | null = readyRun, reviewMember?: WaveReviewMember) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithRouter(
    createElement(
      QueryClientProvider,
      { client },
      createElement(
        ConfirmProvider,
        null,
        createElement(TaskInspector, {
          task,
          run,
          selectedTaskId: task.id,
          loading: false,
          reviewMember,
          onClose: () => {},
          onOpenTask: () => {},
        }),
      ),
    ),
  );
}

test("walkthrough inspector separates implementation, review and delivery stages", () => {
  expect(actualStage({ ...readyTask, status: "in_progress" }, readyRun).label).toBe("Building now");
  expect(actualStage(acceptedTask, acceptedRun).label).toContain("waiting for independent review");

  const reviewing = { ...readyRun, lane: "review" as const, outcome: "running" as const, runnerProfile: "reviewer", runnerHarness: "codex_exec" };
  expect(actualStage({ ...readyTask, status: "review" }, reviewing).label).toBe("Reviewing now");

  const failedReviewer = { ...reviewing, outcome: "failed" as const, liveness: "dead" as const };
  expect(actualStage({ ...readyTask, status: "review" }, failedReviewer).label).toBe("Review failed — action needed");
  expect(actualStage({ ...acceptedTask, status: "done" }, failedReviewer).label).toBe("Delivered");
});

test("walkthrough inspector exposes activity truth and protects selection races", () => {
  const outOfOrder = {
    ...readyRun,
    events: [...readyRun.events].reverse(),
  };
  expect(latestRunEvent(outOfOrder)?.text).toBe("Rendered thirty nodes; measuring pan latency.");
  expect(resolveVisibleRun(readyRun, failedTask.id)).toBeNull();
  expect(render(unavailableTask, null)).toContain("Unavailable — no activity recorded");
  expect(render()).toContain("Open run logs and details");
  expect(render()).toContain(readyRun.events[1]!.ts);
});

test("walkthrough inspector never inherits proof from a failed current attempt", () => {
  const failedWithOldPass = {
    ...failedTask,
    verification: [{ id: "V1", command: "bun test", result: "pass" as const }],
  };
  const failedCurrent = { ...failedRun, events: [], delivery: { summary: "old delivery", proofStatus: "accepted" } };
  const html = render(failedWithOldPass, failedCurrent);
  expect(html).toContain("Previous proof is not current");
  expect(html).toContain("not current");
  expect(acceptedDelivery(failedCurrent)).toBeNull();

  const staleCurrent = { ...readyRun, liveness: "stale" as const };
  expect(render({ ...failedWithOldPass, id: readyTask.id }, staleCurrent)).toContain("not current");
});

test("walkthrough inspector preserves authored intent and labels a missing one honestly", () => {
  expect(visibleTaskIntent(readyTask)).toEqual({ markdown: readyTask.intent, fromTitle: false });
  expect(visibleTaskIntent({ ...readyTask, intent: "No intent recorded." })).toEqual({
    markdown: `Task title: ${readyTask.title}`,
    fromTitle: true,
  });
  expect(render({ ...readyTask, intent: "" }, readyRun)).toContain("Authored intent is unavailable; this uses the task title.");
});

test("walkthrough inspector keeps the completed worker separate from the current reviewer", () => {
  const run = {
    ...readyRun,
    lane: "review" as const,
    outcome: "running" as const,
    activeAttemptId: "attempt-review",
    attempts: [
      { id: "attempt-worker", n: 1, lane: "execute" as const, runner: "codex", outcome: "succeeded" as const, durationSec: 120, startedAt: "2026-09-16T06:00:00Z", finishedAt: "2026-09-16T06:02:00Z" },
      { id: "attempt-review", n: 2, lane: "review" as const, runner: "claude", outcome: "running" as const, durationSec: 0, startedAt: "2026-09-16T06:03:00Z" },
    ],
  };
  expect(currentAttemptRecord(run)?.id).toBe("attempt-review");
  expect(historicalAttemptRecords(run).map((attempt) => attempt.id)).toEqual(["attempt-worker"]);
  const html = render(readyTask, run);
  expect(html).toContain("Independent review active · attempt 2");
  expect(html).toContain("Implementation · attempt 1 · succeeded · attempt-worker");
  expect(html).not.toContain("Implementation · attempt 2");
});

test("walkthrough inspector lets authoritative member phases beat stale task and run reads", () => {
  const staleTask = { ...readyTask, status: "in_progress" as const };
  const staleRun = { ...readyRun, outcome: "succeeded" as const, liveness: "stale" as const };
  const proofBlocked: WaveReviewMember = {
    taskId: staleTask.id,
    title: staleTask.title,
    state: "blocked",
    phase: "proof_blocked",
    waitingReason: "verification is required",
  };
  expect(actualStage(staleTask, staleRun, proofBlocked)).toEqual({ label: "Verification required", state: "proof_blocked", tone: "warn", live: false });

  const unknown = { ...proofBlocked, phase: "outcome_unknown" as const };
  expect(actualStage(staleTask, staleRun, unknown)).toEqual({ label: "Needs recovery", state: "unknown", tone: "warn", live: false });
  expect(render(staleTask, staleRun, proofBlocked)).toContain("Verification required");

  const failed: WaveReviewMember = { ...proofBlocked, phase: "failed", waitingReason: "review rejected the attempt" };
  expect(actualStage(staleTask, staleRun, failed)).toEqual({ label: "Failed", state: "failed", tone: "fail", live: false });
  expect(render(staleTask, staleRun, failed)).toContain("<span data-testid=\"inspector-stage\" class=\"inline-flex items-center rounded-full border px-2.5 py-0.5 text-[11px] font-medium whitespace-nowrap border-fail/30 bg-fail-soft text-fail\">Failed</span>");
});

test("walkthrough inspector wraps long task ids and titles in the header", () => {
  const longId = "APP-T-0001-very-long-imported-task-identifier-that-must-remain-visible";
  const longTitle = "A task title with enough words to prove the inspector header wraps instead of truncating";
  const html = render({ ...readyTask, id: longId, title: longTitle });
  expect(html).toContain(`data-testid="inspector-header-id"`);
  expect(html).toContain(longId);
  expect(html).toContain(`data-testid="inspector-header-title"`);
  expect(html).toContain(longTitle);
  expect(html).not.toMatch(/data-testid="inspector-header-id"[^>]*truncate/);
  expect(html).not.toMatch(/data-testid="inspector-header-title"[^>]*truncate/);
});
