import { expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { SessionRecoveryControls } from "../src/features/runs/detail/SessionRecoveryControls";
import type { RunDetail } from "../src/types/domain";

const base = {
  taskId: "TSK-T-0066", taskTitle: "Server actions", projectId: "tusker",
  runner: "codex", model: "gpt-6", lane: "execute", leaseState: "released",
  leaseStateRaw: "released", processRunning: false, outcome: "interrupted",
  elapsedSec: 1, sinceLastEventSec: 1, liveness: "stale", attemptCount: 1,
  terminal: false, workspacePath: "/tmp/run", attempts: [], events: [],
} satisfies RunDetail;

test("only declared capabilities enable run actions, even when resume says supported", () => {
  const html = renderToStaticMarkup(createElement(SessionRecoveryControls, {
    run: { ...base, resume: { supported: true }, controls: { capabilities: [
      { action: "continue", available: false, reason: "Owner cannot continue" },
      { action: "reconnect", available: true },
    ] } },
    onReconnect: () => {}, onAction: () => {},
  }));
  expect(html).toContain('aria-label="Continue existing session"');
  expect(html).toContain('title="Owner cannot continue"');
  expect(html).toContain('title="This server did not declare this action."');
  expect((html.match(/ disabled=""/g) ?? []).length).toBe(5);
});

test("server pending Stop survives a fresh render without local state", () => {
  const html = renderToStaticMarkup(createElement(SessionRecoveryControls, {
    run: { ...base, controls: { capabilities: [{ action: "stop", available: true }], pending: { action: "stop", state: "pending" } } },
    onReconnect: () => {}, onAction: () => {},
  }));
  expect(html).toContain("Stop pending · awaiting canonical readback");
  expect(html).toContain("Stop…");
  expect((html.match(/ disabled=""/g) ?? []).length).toBe(6);
});
