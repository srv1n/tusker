import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterContextProvider } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import type { RunDetail, TaskDetail, WaveReviewMember } from "../src/types/domain";
import {
  acceptedDelivery,
  currentAttemptRecord,
  historicalAttemptRecords,
  latestRunEvent,
  resolveVisibleRun,
  visibleTaskIntent,
} from "../src/features/workbench/inspector/inspectorLogic";
import { sampleState } from "../src/features/workbench/overview/previewFixtures";
import { failedRun, failedTask, readyRun, readyTask, unavailableTask } from "../previews/wux/inspector/fixtures";

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
          // Preview fixtures predate the state record; default them to Planned.
          task: { ...task, state: task.state ?? sampleState("planned") },
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

test("walkthrough inspector renders only the server state record", () => {
  const html = render({ ...readyTask, status: "in_progress", state: sampleState("blocked", "verification is required", { next_action: "Run the current verification commands" }) }, readyRun);
  expect(html).toContain('data-task-state="blocked"');
  expect(html).toContain("verification is required");
  expect(html).toContain("Next: Run the current verification commands");
  expect(html).not.toContain("Building now");
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
