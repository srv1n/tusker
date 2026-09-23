import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterContextProvider } from "@tanstack/react-router";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { HumanActionCard } from "../src/features/human-action/HumanActionCard";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import { readyTask } from "../previews/wux/inspector/fixtures";

const question = {
  kind: "question", rawKind: "question", title: "Question from WUX-T-0001", action: "Which release window?",
  whyAgentCannot: "", completionCondition: "", gateId: "question-msg-1", materialRevision: "",
  covers: [], acceptance: [], messageId: "msg-1", body: "Which release window?", askedAt: "2026-09-23T12:00:00Z",
  recipientLabel: "operator:operator", taskId: readyTask.id, yieldSender: true,
};

function render(element: ReturnType<typeof createElement>) {
  const root = createRootRoute();
  const runRoute = createRoute({ getParentRoute: () => root, path: "/p/$projectId/runs/$taskId" });
  const router = createRouter({ routeTree: root.addChildren([runRoute]), history: createMemoryHistory({ initialEntries: ["/"] }) });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(RouterContextProvider, { router }, element))));
}

describe("worker questions needing the operator", () => {
  test("card shows the question and a reply control", () => {
    const html = render(createElement(HumanActionCard, { action: question, taskId: readyTask.id, taskTitle: readyTask.title, projectId: readyTask.projectId }));
    expect(html).toContain("Which release window?");
    expect(html).toContain("operator:operator");
    expect(html).toContain("worker waiting");
    expect(html).toContain("question-reply-msg-1");
    expect(html).toContain(">Reply</button>");
  });

  test("inspector selects the open question in its message form", () => {
    const task = { ...readyTask, humanAction: question, humanActions: [question], messages: [{
      id: "msg-1", sender: `task:${readyTask.id}`, recipient: { kind: "operator" as const, id: "operator" },
      kind: "question" as const, body: question.body, replyRequired: true, yieldSender: true,
      state: "queued", transportState: "pending", createdAt: question.askedAt,
    }] };
    const html = render(createElement(TaskInspector, { task, run: null, selectedTaskId: task.id, loading: false, onClose: () => {}, onOpenTask: () => {} }));
    expect(html).toContain("Which release window?");
    expect(html).toContain(`Reply to task:${readyTask.id}`);
    expect(html).toContain("Send answer");
  });
});
