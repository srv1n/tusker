import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import { failedRun, readyTask } from "../previews/wux/inspector/fixtures";
import type { WaveReviewMember } from "../src/types/domain";

function render(member: WaveReviewMember) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null,
    createElement(TaskInspector, { task: readyTask, run: failedRun, selectedTaskId: readyTask.id, loading: false, reviewMember: member, onClose: () => {}, onOpenTask: () => {} }),
  )));
}

test("enabled adopt_completed renders the reconcile action with pre-run copy", () => {
  const html = render({ taskId: readyTask.id, title: readyTask.title, state: "waiting", phase: "awaiting_review", waitingReason: "awaiting independent review", recovery: { action: "adopt_completed", enabled: true } });
  expect(html).toContain("Reconcile completed work");
  expect(html).toContain("Verify the work already in this project and record it against this task.");
  expect(html).toContain("Implementation source: Not recorded");
});

test("adopt_completed markup carries no internal jargon", () => {
  const html = render({ taskId: readyTask.id, title: readyTask.title, state: "waiting", phase: "awaiting_review", recovery: { action: "adopt_completed", enabled: true } });
  // Scope to the action button and its detail line — other inspector
  // sections legitimately render unrelated fixture content.
  const button = html.match(/<button[^>]*>Reconcile completed work<\/button>/)?.[0] ?? "";
  const detail = html.match(/<p data-testid="inspector-adopt-detail"[^>]*>.*?<\/p>/)?.[0] ?? "";
  expect(button).not.toBe("");
  expect(detail).not.toBe("");
  for (const region of [button, detail]) {
    const lower = region.toLowerCase();
    expect(lower).not.toContain("provenance");
    expect(lower).not.toContain("fingerprint");
    expect(lower).not.toContain("conversation");
  }
});

test("disabled adopt_completed renders its reason", () => {
  const html = render({ taskId: readyTask.id, title: readyTask.title, state: "waiting", phase: "awaiting_review", recovery: { action: "adopt_completed", enabled: false, reason: "task is already done" } });
  expect(html).toContain("Reconcile completed work");
  expect(html).toContain("disabled");
  expect(html).toContain("task is already done");
});

test("outcome_unknown renders one explicit safe recovery action", () => {
  const html = render({ taskId: readyTask.id, title: readyTask.title, state: "blocked", phase: "outcome_unknown", waitingReason: "contact lost", recovery: { action: "recover_unknown", enabled: true } });
  expect(html).toContain("Verify and continue");
  expect(html).toContain("Inspect and preserve any existing work, verify it, then continue what remains.");
  expect(html).toContain("Verify and preserve existing work, then continue what remains.");
  expect(html).not.toContain("Retry wave");
});
