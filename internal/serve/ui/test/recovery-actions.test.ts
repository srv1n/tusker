import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import { failedRun, readyTask as fixtureTask } from "../previews/wux/inspector/fixtures";
import { sampleState } from "../src/features/workbench/overview/previewFixtures";
import type { TaskState, WaveReviewMember } from "../src/types/domain";

const readyTask = fixtureTask;

function render(member: WaveReviewMember, state: TaskState = sampleState("blocked")) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null,
    createElement(TaskInspector, { task: { ...readyTask, state }, run: failedRun, selectedTaskId: readyTask.id, loading: false, reviewMember: member, onClose: () => {}, onOpenTask: () => {} }),
  )));
}

test("failed reviewer renders the reviewer-only recovery action", () => {
  const html = render({ taskId: readyTask.id, title: readyTask.title, state: sampleState("blocked", "review proposal snapshot drifted"), phase: "failed", lane: "review", waitingReason: "review proposal snapshot drifted" }, sampleState("blocked", "review proposal snapshot drifted"));
  expect(html).toContain("Retry review");
  expect(html).toContain("review proposal snapshot drifted");
  expect(html).not.toContain("Review the implementation, then record the outcome");
});

test("invalid and unavailable proof render truthful check recovery", () => {
  const base = { taskId: readyTask.id, title: readyTask.title, state: sampleState("in_review"), phase: "proof_blocked" as const };
  const changed = render({ ...base, proofInvalidation: { kind: "changed", dimension: "source", previous: "a", current: "b", nextActor: "command_executor", recovery: "rerun_checks", explanation: "accepted verification no longer matches the current source" } });
  expect(changed).toContain("Rerun checks");
  expect(changed).toContain("accepted verification no longer matches the current source");
  expect(changed).toContain("Previous verified material:");
  expect(changed).toContain("Latest attempted material:");
  expect(changed).toContain("complete receipt history remains under Exact verification");
  const unavailable = render({ ...base, proofInvalidation: { kind: "unavailable", dimension: "material", previous: "a", nextActor: "command_executor", recovery: "rerun_checks", explanation: "current scoped material could not be read; this does not prove that work changed" } });
  expect(unavailable).toContain("disabled");
  expect(unavailable).toContain("does not prove that work changed");
});
