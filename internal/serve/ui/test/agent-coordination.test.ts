import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterContextProvider } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import { readyRun, readyTask } from "../previews/wux/inspector/fixtures";

// 834e84a4 added a run-detail Link to the inspector; server renders need router context.
function renderWithRouter(element: ReturnType<typeof createElement>) {
  const root = createRootRoute();
  const runRoute = createRoute({ getParentRoute: () => root, path: "/p/$projectId/runs/$taskId" });
  const router = createRouter({ routeTree: root.addChildren([runRoute]), history: createMemoryHistory({ initialEntries: ["/"] }) });
  return renderToStaticMarkup(createElement(RouterContextProvider, { router }, element));
}

describe("agent coordination", () => {
  test("shows contacts separately from prerequisites and truthful receipts", () => {
    const task = { ...readyTask, contacts: [{ projectId: "app", taskId: readyTask.id, role: "architect" as const, address: { kind: "task" as const, id: "ARCH-T-1" }, generation: 1 }, { projectId: "app", taskId: readyTask.id, role: "peer" as const, name: "schema", address: { kind: "task" as const, id: "PEER-T-1" }, generation: 1 }], messages: [{ id: "m1", sender: "task:ARCH-T-1", recipient: { kind: "task" as const, id: readyTask.id }, kind: "question" as const, body: "Which revision?", replyRequired: true, yieldSender: true, state: "queued", transportState: "unsupported", createdAt: "now" }] };
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const html = renderWithRouter(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(TaskInspector, { task, run: readyRun, selectedTaskId: task.id, loading: false, onClose: () => {}, onOpenTask: () => {} }))));
    expect(html).toContain("Agent coordination"); expect(html).toContain("ARCH-T-1"); expect(html).toContain("Which revision?"); expect(html).toContain("unsupported"); expect(html).toContain("Message recipient"); expect(html).toContain("Reply");
  });
});
