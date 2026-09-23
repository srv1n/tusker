import { expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterContextProvider } from "@tanstack/react-router";
import { RunHeader } from "../src/features/runs/detail/RunHeader";
import { RunSayBox } from "../src/features/runs/detail/RunSayBox";
import type { RunDetail } from "../src/types/domain";

const base: RunDetail = {
  taskId: "TSK-T-1", taskTitle: "Run state", projectId: "tusker", runner: "codex", model: "gpt-6", lane: "execute", leaseState: "held", outcome: "running", elapsedSec: 1, sinceLastEventSec: 1, liveness: "fresh", attemptCount: 1, workspacePath: "/tmp/run", attempts: [], events: [],
};
const retry = { pending: false, result: null };
const interrupt = { confirming: false, pending: false, awaitingReadback: false, result: null };
function renderHeader(run: RunDetail) {
  const root = createRootRoute();
  const docs = createRoute({ getParentRoute: () => root, path: "/p/$projectId/docs" });
  const router = createRouter({ routeTree: root.addChildren([docs]), history: createMemoryHistory({ initialEntries: ["/"] }) });
  return renderToStaticMarkup(createElement(RouterContextProvider, { router }, createElement(RunHeader, { run, onInterrupt: () => {}, onRetry: () => {}, retry, interrupt })));
}

for (const state of ["working", "waiting_on_you", "quiet", "blocked", "failed", "lost", "stopped", "queued", "finished"] as const) {
  test(`${state} renders exactly the server-declared header and message actions`, () => {
    const run: RunDetail = { ...base, operatorState: { state }, sayRoute: { mode: "soft", available: true, note: "Available" }, controls: { capabilities: [
      { action: "interrupt", available: state === "working" },
      { action: "redrive", available: state === "failed" },
      { action: "say", available: state === "quiet" },
      { action: "continue", available: state === "stopped" },
    ] } };
    const header = renderHeader(run);
    const say = renderToStaticMarkup(createElement(RunSayBox, { run, onReadback: async () => {} }));
    expect(header.includes(">Interrupt</button>")).toBe(state === "working");
    expect(header.includes(">Redrive</button>")).toBe(state === "failed");
    expect(say.includes(">Say</button>")).toBe(state === "quiet");
    expect(say.includes(">Continue</button>")).toBe(state === "stopped");
  });
}

test("a working review run cannot interrupt without server permission", () => {
  const run: RunDetail = { ...base, operatorState: { state: "working" }, controls: { capabilities: [{ action: "interrupt", available: false, reason: "Review lane" }] } };
  const html = renderHeader(run);
  expect(html).not.toContain(">Interrupt</button>");
  expect(html).toContain("Redrive is unavailable");
});
