/*
  Task states (docs/system/proposals/task-states.md, S3/S4): every surface
  shows the server's one state record. Internal lease, outcome, readiness, or
  run-directive facts never change the label.
*/

import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterContextProvider } from "@tanstack/react-router";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import { boardGroupFor } from "../src/features/workbench/board/boardModel";
import { WaveList } from "../src/features/workbench/overview/WaveList";
import { makeTask, sampleState } from "../src/features/workbench/overview/previewFixtures";
import type { RunDetail, TaskDetail, WaveListItem } from "../src/types/domain";
import { readyRun, readyTask } from "../previews/wux/inspector/fixtures";

const QUESTION = "Which port should the dev server bind to?";

// Internals that used to read as "Queued": a queued run directive, a ready
// status, and a retry-queued lease. The state record says the worker asked.
const pausedQuestionTask: TaskDetail = {
  ...readyTask,
  status: "ready",
  readiness: "ready",
  hasGate: false,
  humanAction: undefined,
  runDirective: { state: "queued", actor: "daemon", expiresAt: "2026-09-27T12:00:00Z" } as TaskDetail["runDirective"],
  humanActions: [{
    kind: "question", rawKind: "question", title: "Worker question", action: QUESTION, whyAgentCannot: "", completionCondition: "",
    gateId: "msg-1", materialRevision: "", covers: [], acceptance: [], messageId: "msg-1", body: QUESTION, taskId: readyTask.id,
  }],
  state: sampleState("needs_input", QUESTION, { reason_code: "question" }),
};
const retryQueuedRun: RunDetail = { ...readyRun, leaseState: "retry_queued", leaseStateRaw: "retry_queued", outcome: "retry-queued", liveness: "stale" };

function renderInspector(task: TaskDetail, run: RunDetail | null): string {
  const root = createRootRoute();
  const runRoute = createRoute({ getParentRoute: () => root, path: "/p/$projectId/runs/$taskId" });
  const router = createRouter({ routeTree: root.addChildren([runRoute]), history: createMemoryHistory({ initialEntries: ["/"] }) });
  return renderToStaticMarkup(createElement(QueryClientProvider, { client: new QueryClient() },
    createElement(ConfirmProvider, null, createElement(RouterContextProvider, { router },
      createElement(TaskInspector, { task, run, selectedTaskId: task.id, loading: false, onClose: () => {}, onOpenTask: () => {} })))));
}

describe("task state display", () => {
  test("a paused worker question shows Needs input, the question, and a reply box, never Queued", () => {
    const html = renderInspector(pausedQuestionTask, retryQueuedRun);
    expect(html).toContain('data-task-state="needs_input"');
    expect(html).toContain("Needs input");
    expect(html).toContain(QUESTION);
    expect(html).toContain(`id="question-reply-msg-1"`);
    expect(html).not.toContain("Queued");
    expect(html).not.toContain("Run task");
  });

  test("a blocked task shows its reason and next action", () => {
    const task = { ...pausedQuestionTask, humanActions: [], state: sampleState("blocked", "The harness failed on all 6 attempts.", { reason_code: "crashed", next_action: "Open the log, then Retry" }) };
    const html = renderInspector(task, retryQueuedRun);
    expect(html).toContain('data-task-state="blocked"');
    expect(html).toContain("The harness failed on all 6 attempts.");
    expect(html).toContain("Next: Open the log, then Retry");
  });

  test("board grouping reads only the state record", () => {
    const task = makeTask({ id: "T-1", title: "Build", status: "in_progress", liveRun: true, latestAttemptOutcome: "retry-queued", state: sampleState("needs_input", QUESTION) });
    expect(boardGroupFor(task)).toBe("needs_input");
  });

  test("the wave list groups and labels waves by their state", () => {
    const wave = (id: string, state: WaveListItem["state"]): WaveListItem => ({ id, title: `Wave ${id}`, status: "open", authorization: "armed", memberCount: 3, doneCount: 1, liveRun: true, state });
    const html = renderToStaticMarkup(createElement(WaveList, {
      waves: [wave("W-1", sampleState("working", "2 working, 1 done")), wave("W-2", sampleState("blocked", "1 blocked, 2 planned"))],
      query: "", onOpenWave: () => {}, loading: false,
    }));
    expect(html.indexOf('aria-label="Blocked"')).toBeLessThan(html.indexOf('aria-label="Working"'));
    expect(html).toContain("2 working, 1 done");
    expect(html).not.toContain("Needs you");
    expect(html).not.toContain("Auto-run on");
  });
});
