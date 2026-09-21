/*
  WUX-T-0006 — TaskInspector focused behavior checks.

  These exercise real behavior: the race guard, the identity rule and the
  evidence/acceptance distinction are asserted through the pure helpers AND
  through server-rendered panel markup (renderToStaticMarkup), so a late
  payload, a missing identity or a mislabeled artifact fails the suite.

  Written with createElement (no JSX) so the file stays a plain .ts test.
*/

import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import { taskRunBlocker } from "../src/features/product/TaskScreens";
import {
  acceptedDelivery,
  actualStage,
  identityDisplay,
  resolveVisibleTask,
} from "../src/features/workbench/inspector/inspectorLogic";
import { acceptedRun, acceptedTask, failedTask, readyRun, readyTask } from "../previews/wux/inspector/fixtures";

type InspectorProps = Parameters<typeof TaskInspector>[0];

function render(overrides: Partial<InspectorProps> = {}): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const props: InspectorProps = {
    task: readyTask,
    run: readyRun,
    selectedTaskId: readyTask.id,
    loading: false,
    executionIdentity: {
      provider: "preview-provider",
      model: "preview-model",
      transport: "ACP",
      stage: "execute",
      observed: true,
    },
    onClose: () => {},
    onOpenTask: () => {},
    ...overrides,
  };
  return renderToStaticMarkup(
    createElement(
      QueryClientProvider,
      { client },
      createElement(ConfirmProvider, null, createElement(TaskInspector, props)),
    ),
  );
}

describe("inspector selection race", () => {
  test("inspector late task response rejected", () => {
    // Logic level: a payload for A is invisible while B is selected.
    expect(resolveVisibleTask(readyTask, failedTask.id)).toBeNull();
    expect(resolveVisibleTask(readyTask, readyTask.id)).not.toBeNull();

    // Rendered level: the panel shows loading, never the other task's data.
    const html = render({ task: readyTask, selectedTaskId: failedTask.id, loading: false });
    expect(html).not.toContain(readyTask.title);
    expect(html).toContain('data-testid="inspector-loading"');

    // The matching selection renders the task.
    const matched = render({ task: readyTask, selectedTaskId: readyTask.id });
    expect(matched).toContain(readyTask.title);
    expect(matched).toContain(`data-task-id="${readyTask.id}"`);
  });
});

describe("inspector execution identity", () => {
  test("inspector missing identity is unavailable", () => {
    expect(identityDisplay(undefined)).toEqual({ state: "unavailable" });
    expect(identityDisplay({ observed: false })).toEqual({ state: "unavailable" });
    expect(
      identityDisplay({ provider: "p", model: "m", transport: "ACP", observed: true }),
    ).toEqual({ state: "unavailable" });

    const missing = render({ executionIdentity: undefined });
    expect(missing).toContain("Unavailable");
    expect(missing).not.toContain("preview-model");

    const unobserved = render({ executionIdentity: { observed: false } });
    expect(unobserved).toContain("Unavailable");
    expect(unobserved).not.toContain("preview-model");

    // A past worker's model never appears as the current reviewer's model:
    // an unobserved stage keeps the panel at Unavailable.
    const reviewHtml = render({
      task: { ...readyTask, humanAction: undefined },
      executionIdentity: {
        provider: "preview-provider",
        model: "stale-worker-model",
        transport: "ACP",
        stage: "execute",
        observed: false,
      },
    });
    expect(reviewHtml).not.toContain("stale-worker-model");
  });
});

describe("inspector task routing", () => {
  test("a planned backlog task is startable without wave-first choreography", () => {
    const planned = { ...readyTask, status: "backlog" as const, rawStatus: "backlog", readiness: "held" as const, hasGate: false, humanAction: undefined, humanActions: [], effectiveExecute: { profile: "worker", model: "gpt-worker", effort: "medium", harness: "codex_exec", blockers: [] }, effectiveReview: { profile: "reviewer", model: "gpt-review", effort: "high", harness: "codex_exec", blockers: [] } };
    expect(taskRunBlocker(planned)).toBeUndefined();
    const html = render({ task: planned, run: null });
		expect(html).toContain(`aria-label="Run task ${planned.id}"`);
  });

  test("shows the default tier, predicted routes, and recorded current identity", () => {
    const task = {
      ...readyTask,
      effectiveExecute: { work_level: "standard", profile: "worker", model: "gpt-worker", effort: "medium", source: "model levels", blockers: [] },
      effectiveReview: { work_level: "standard", profile: "reviewer", model: "gpt-review", effort: "high", source: "model levels", blockers: [] },
    };
    const run = { ...readyRun, runnerProfile: "actual-reviewer", runnerHarness: "codex_exec", model: "gpt-actual", lane: "review" as const };
    const html = render({ task, run, executionIdentity: undefined });
    expect(html).toContain("Tier 2 · Standard");
    expect(html).toContain(">Worker<");
    expect(html).toContain("worker · gpt-worker · medium");
    expect(html).toContain("Current reviewer:");
    expect(html).toContain("actual-reviewer · gpt-actual · Codex");
    expect(html).not.toContain("codex_exec");
  });
});

describe("inspector evidence and acceptance", () => {
  test("inspector evidence keeps acceptance", () => {
    // Accepted delivery surfaces once, as the run's accepted result…
    expect(acceptedDelivery(acceptedRun)).not.toBeNull();
    // …while an unreviewed or pending delivery never becomes one.
    expect(acceptedDelivery(readyRun)).toBeNull();
    expect(acceptedDelivery(null)).toBeNull();

    const html = render({ task: acceptedTask, run: acceptedRun, selectedTaskId: acceptedTask.id });
    expect(html).toContain('data-testid="inspector-accepted"');
    expect(html).toContain(acceptedRun.delivery!.summary!);

    // Evidence items render as available artifacts with no accepted stamp…
    expect(html).toContain('data-testid="inspector-evidence-item"');
    expect(html).toContain("Appearing here is not acceptance");
    // …and the acceptance proofs render alongside, not merged into evidence.
    expect(html).toContain('data-testid="inspector-acceptance"');
    expect(html).toContain("pass");

    // Worker success is not accepted completion: a succeeded run on an
    // unreviewed task still reads as checking, not delivered.
    const stage = actualStage({ ...acceptedTask, status: "review" }, acceptedRun);
    expect(stage.label.toLowerCase()).toContain("check");
  });
});

describe("inspector parked attempts", () => {
  test("does not present a stopped review as active checking", () => {
    const parked = { ...acceptedRun, outcome: "parked-no-progress" as const, liveness: "dead" as const };
    expect(actualStage({ ...acceptedTask, status: "review" }, parked)).toEqual({
      label: "Stopped — no progress", tone: "warn", live: false,
    });
    const task = { ...acceptedTask, status: "review" as const };
    expect(render({ task, run: parked, selectedTaskId: task.id })).toContain("Latest attempt");
  });
});
