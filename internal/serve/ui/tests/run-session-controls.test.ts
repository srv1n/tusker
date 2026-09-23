import { expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { AttemptTimeline } from "../src/features/runs/detail/AttemptTimeline";
import { EventTail } from "../src/features/runs/detail/EventTail";
import { SessionRecoveryControls } from "../src/features/runs/detail/SessionRecoveryControls";
import type { RunDetail } from "../src/types/domain";

const run = {
  taskId: "TSK-T-0053",
  taskTitle: "Recovery UI",
  projectId: "tusker",
  runner: "codex",
  model: "gpt-5",
  lane: "execute",
  leaseState: "released",
  leaseStateRaw: "released",
  processRunning: false,
  outcome: "interrupted",
  elapsedSec: 30,
  sinceLastEventSec: 90,
  liveness: "stale",
  attemptCount: 2,
  activeAttemptId: "attempt-2",
  terminal: false,
  workspacePath: "/tmp/recovery-ui",
  attempts: [
    { id: "attempt-1", n: 1, outcome: "failed", durationSec: 12, startedAt: "2026-09-22T10:00:00Z" },
    { id: "attempt-2", n: 2, outcome: "interrupted", durationSec: 18, startedAt: "2026-09-22T10:01:00Z" },
  ],
  events: [],
  activity: {
    captureState: "partial",
    captureReason: "tool result capture was unavailable",
    heartbeatAgeSec: 4,
    messageAgeSec: 41,
    toolAgeSec: null,
  },
  controls: {
    capabilities: [
      { action: "reconnect", available: true },
      { action: "continue", available: true, provider: "codex" },
      { action: "recover_context", available: true },
      { action: "pause", available: false, reason: "provider does not acknowledge durable pause" },
      { action: "stop", available: false, reason: "no matching owner" },
      { action: "start_fresh", available: false, reason: "settlement required" },
    ],
    checkpoint: { completedAction: "apply patch", unresolvedAction: "provider acknowledgement", operation: "verify" },
  },
} satisfies RunDetail;

test("run recovery controls show capability reasons and fresh-session semantics", () => {
  const html = renderToStaticMarkup(createElement(SessionRecoveryControls, {
    run,
    onReconnect: () => {},
    onAction: () => {},
  }));
  expect(html).toContain("Reconnect");
  expect(html).toContain("Continue existing session");
  expect(html).toContain("provider does not acknowledge durable pause");
  expect(html).toContain("Start fresh creates a different native session");
  expect(html).toContain("unresolved provider acknowledgement");
});

test("attempt selection is keyboard-addressable and activity keeps multiline text readable", () => {
  const timeline = renderToStaticMarkup(createElement(AttemptTimeline, {
    attempts: run.attempts,
    selectedAttemptId: "attempt-1",
    onSelect: () => {},
  }));
  expect(timeline).toContain("Show activity for attempt 1");
  expect(timeline).toContain('aria-current="true"');

  const tail = renderToStaticMarkup(createElement(EventTail, {
    events: [{ id: "message-1", ts: "", kind: "agent_message", text: "line one\nline two", activity: true }],
    liveness: "stale",
    sinceLastEventSec: 90,
    activity: run.activity,
    historicalAttempt: true,
  }));
  expect(tail).toContain("heartbeat");
  expect(tail).toContain("message");
  expect(tail).toContain("tool progress");
  expect(tail).toContain("Activity capture partial");
  expect(tail).toContain("line one");
  expect(tail).toContain("line two");
  expect(tail).toContain("historical attempt");
});

test("a pending action is rendered until canonical readback", () => {
  const html = renderToStaticMarkup(createElement(SessionRecoveryControls, {
    run,
    pendingAction: "continue",
    onReconnect: () => {},
    onAction: () => {},
  }));
  expect(html).toContain("Continue existing session…");
  expect(html).toContain("awaiting canonical readback");
});
