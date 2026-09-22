/*
  FLW-T-0046 — direct-wave authority UI checks.

  Server-renders the shared wave-authority components against seeded QueryClient
  WaveReview fixtures (tusker.wave-review/v1) and asserts the projected control
  is the only one offered. Source-contract checks keep every routed surface on
  the direct review/control/task-start seams and off the legacy delivery-plan,
  wave-execute and task-run endpoints.
*/

import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { qk } from "../src/lib/queries";
import { WaveAuthorityControls, WaveMemberList, WaveReviewDetail, canRetryWave, summarizeIssues, summarizeWave } from "../src/features/workbench/integration/WaveAuthority";
import { taskRunBlocker } from "../src/features/product/TaskScreens";
import { readyTask } from "../previews/wux/inspector/fixtures";
import type { TaskDetail, WaveReview } from "../src/types/domain";

const PROJECT = "wux-preview";
const WAVE = "W-0001";

function reviewFixture(overrides: Partial<WaveReview>): WaveReview {
  return {
    schema: "tusker.wave-review/v1",
    waveId: WAVE,
    title: "Wave one",
    outcome: "Ship the wave outcome.",
    state: "Planned",
    authorization: "inert",
    materialFingerprint: "fp-0001",
    members: [
      {
        taskId: "APP-T-0001",
        title: "First task",
        state: "planned",
        executeRoute: "worker",
        reviewRoute: "reviewer",
        instructions: "Implement the first task contract exactly.",
        acceptance: ["A1 works"],
        verification: ["go test ./x"],
      },
    ],
    frontiers: [["APP-T-0001"]],
    blockers: [],
    controls: [],
    ...overrides,
  };
}

function renderControls(review: WaveReview): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(qk.waveReview(PROJECT, WAVE), review);
  return renderToStaticMarkup(
    createElement(QueryClientProvider, { client }, createElement(WaveAuthorityControls, { projectId: PROJECT, waveId: WAVE })),
  );
}

describe("wave authority controls", () => {
	test("ready wave shows counts and offers Run wave without boilerplate", () => {
    const html = renderControls(reviewFixture({ controls: [{ action: "wave start", enabled: true, scope: WAVE }] }));
    expect(html).toContain("0/1 accepted");
    expect(html).toContain("1 waiting");
    expect(html).toContain('data-wave-control="wave start"');
		expect(html).toContain(">Run wave<");
    expect(html).not.toContain("Ready to start");
    expect(html).not.toContain("Work is prepared");
    expect(html).not.toContain("This starts only the currently eligible tasks");
    expect(html).not.toContain(">Pause<");
    expect(html).not.toContain(">Resume<");
  });

  test("authorized waiting wave stays Waiting and offers Pause, never Running", () => {
    const html = renderControls(reviewFixture({
      state: "Waiting",
      authorization: "authorized",
      controls: [{ action: "wave pause", enabled: true, scope: WAVE, reason: "admitted attempts may finish" }],
    }));
    expect(html).toContain("Waiting");
    expect(html).not.toContain(">Running<");
    expect(html).toContain('data-wave-control="wave pause"');
    expect(html).toContain(">Pause<");
    expect(html).not.toContain(">Start<");
  });

  test("failed authorized idle wave exposes Retry wave through the start path", () => {
    const review = reviewFixture({
      state: "Waiting",
      authorization: "authorized",
      members: [{ taskId: "APP-T-0001", title: "First task", state: "blocked", phase: "failed", waitingReason: "worker exited 1", recovery: { action: "retry_task", enabled: true } }],
      blockers: [{ code: "RUNTIME_FAILED", taskId: "APP-T-0001", reason: "worker exited 1", action: "inspect the failed execute attempt" }],
      controls: [{ action: "wave pause", enabled: true, scope: WAVE }],
    });
    expect(canRetryWave(review)).toBe(true);
    const html = renderControls(review);
    expect(html).toContain('data-wave-recovery="retryable"');
    expect(html).toContain('data-wave-control="wave start"');
    expect(html).toContain(">Retry wave<");
    expect(html).toContain("Retry requeues eligible work.");
    expect(html).not.toContain(">Pause<");
  });

  test("uncertain outcome asks for recovery and never offers blind wave retry", () => {
    const review = reviewFixture({
      state: "Waiting",
      authorization: "authorized",
      members: [{ taskId: "APP-T-0001", title: "First task", state: "blocked", phase: "outcome_unknown", waitingReason: "delivery_unknown (write_complete)", recovery: { action: "recover_unknown", enabled: true } }],
      blockers: [{ code: "OUTCOME_UNKNOWN", taskId: "APP-T-0001", reason: "delivery_unknown (write_complete)", action: "inspect retained work before recovery" }],
      controls: [{ action: "wave pause", enabled: true, scope: WAVE }],
    });
    expect(canRetryWave(review)).toBe(false);
    expect(summarizeWave(review).label).toBe("Needs recovery");
    const html = renderControls(review);
    expect(html).toContain("1 needs recovery");
    expect(html).toContain("Verify and continue");
    expect(html).toContain("Some work may already exist");
    expect(html).toContain('data-wave-control="wave pause"');
    expect(html).not.toContain(">Retry wave<");
  });

  test("uncertain recovery honors a disabled capability", () => {
    const html = renderControls(reviewFixture({
      state: "Waiting",
      authorization: "authorized",
      members: [{ taskId: "APP-T-0001", title: "First task", state: "blocked", phase: "outcome_unknown", recovery: { action: "recover_unknown", enabled: false, reason: "Another worker still owns this task" } }],
      blockers: [{ code: "OUTCOME_UNKNOWN", taskId: "APP-T-0001", reason: "lost contact", action: "inspect" }],
      controls: [{ action: "wave pause", enabled: true, scope: WAVE }],
    }));
    expect(html).toContain('data-wave-control="wave pause"');
    expect(html).toContain("Another worker still owns this task");
    expect(html).toMatch(/disabled=""[^>]*>Verify and continue/);
  });

  test("review-only failure does not offer a no-op wave retry", () => {
    const review = reviewFixture({
      state: "Waiting",
      authorization: "authorized",
      members: [{ taskId: "APP-T-0001", title: "First task", state: "blocked", phase: "failed", lane: "review", recovery: { action: "retry_review", enabled: true } }],
      blockers: [{ code: "RUNTIME_FAILED", taskId: "APP-T-0001", reason: "review failed", action: "retry review" }],
      controls: [{ action: "wave pause", enabled: true, scope: WAVE }],
    });
    expect(canRetryWave(review)).toBe(false);
    const html = renderControls(review);
    expect(html).toContain('data-wave-control="wave pause"');
    expect(html).not.toContain(">Retry wave<");
  });

  test("executing wave offers Pause", () => {
    const html = renderControls(reviewFixture({
      state: "Running",
      authorization: "authorized",
      members: [{ taskId: "APP-T-0001", title: "First task", state: "running" }],
      controls: [{ action: "wave pause", enabled: true, scope: WAVE }],
    }));
    expect(html).toContain("Executing");
    expect(html).toContain('data-wave-control="wave pause"');
    expect(canRetryWave(reviewFixture({
      state: "Running",
      authorization: "authorized",
      members: [{ taskId: "APP-T-0001", title: "First task", state: "running", phase: "executing" }],
    }))).toBe(false);
  });

  test("paused wave offers Resume only", () => {
    const html = renderControls(reviewFixture({
      state: "Paused",
      authorization: "paused",
      controls: [{ action: "wave resume", enabled: true, scope: WAVE, reason: "wave is paused" }],
    }));
    expect(html).toContain("Paused");
    expect(html).toContain('data-wave-control="wave resume"');
    expect(html).toContain(">Resume<");
    expect(html).not.toContain('data-wave-control="wave start"');
  });

  test("completed wave shows no enabled control", () => {
    const html = renderControls(reviewFixture({
      state: "Completed",
      authorization: "authorized",
      controls: [{ action: "wave start", enabled: false, scope: WAVE, reason: "wave is already complete" }],
    }));
    expect(html).toContain("Completed");
    expect(html).not.toContain("data-wave-control=");
  });

  test("blocked wave groups a setup cause in human language with a task link", () => {
    const html = renderControls(reviewFixture({
      blockers: [
        { code: "ROUTE_INVALID", taskId: "APP-T-0002", reason: "execute route has no configured profile", action: "choose a worker profile" },
        { code: "HUMAN_GATE_OPEN", reason: "an open human gate blocks APP-T-0003", action: "complete the human gate" },
      ],
      controls: [{ action: "wave start", enabled: false, scope: WAVE, reason: "resolve global blockers before wave start" }],
    }));
    expect(html).not.toContain("data-wave-control=");
    expect(html).toContain("Execution setup needs attention");
    expect(html).toContain(`/p/${PROJECT}/tasks/APP-T-0002`);
    expect(html).not.toContain("Technical details");
    expect(html).not.toContain("ROUTE_INVALID");
    expect(html).not.toContain("HUMAN_GATE_OPEN");
  });

  test("raw internals never reach the wave surface", () => {
    const html = renderControls(reviewFixture({ controls: [{ action: "wave start", enabled: true, scope: WAVE }] }));
    const lower = html.toLowerCase();
    expect(lower).not.toContain("plan path");
    expect(lower).not.toContain("preflight");
    expect(lower).not.toContain("wave arm");
    expect(lower).not.toContain("execute wave");
    expect(html).not.toContain("Technical details");
    expect(html).not.toContain("Material fingerprint");
    expect(html).not.toContain("fp-0001");
  });

  test("maps setup, verification, records, paused, and completed states without exposing codes", () => {
    expect(summarizeWave(reviewFixture({ controls: [{ action: "wave start", enabled: true, scope: WAVE }] })).label).toBe("Ready");
    expect(summarizeWave(reviewFixture({ state: "Running", authorization: "authorized", members: [{ taskId: "APP-T-1", title: "Active", state: "running" }] })).label).toBe("Executing");
    expect(summarizeWave(reviewFixture({ state: "Paused", authorization: "paused" })).label).toBe("Paused");
    expect(summarizeWave(reviewFixture({ state: "Completed" })).label).toBe("Completed");
    expect(summarizeWave(reviewFixture({ blockers: [{ code: "RUNTIME_UNAVAILABLE", reason: "offline", action: "restore" }] })).label).toBe("Waiting for setup");
    expect(summarizeIssues([{ code: "STRICT_PROOF_STALE", reason: "verification receipt is missing", action: "run" }])[0].title).toBe("Previous verification no longer applies");
    expect(summarizeIssues([{ code: "STRICT_PROOF_STALE", reason: "verification receipt is stale", action: "run" }])[0].title).toBe("Previous verification no longer applies");
    expect(summarizeIssues([{ code: "CONTRACT_FINGERPRINT_STALE", reason: "drift", action: "reconcile" }])[0].title).toBe("Work records need reconciling");
  });

  test("does not turn a reported completion into acceptance", () => {
    const html = renderToStaticMarkup(createElement(WaveMemberList, { review: reviewFixture({ members: [{ taskId: "APP-T-0001", title: "Reported", state: "waiting", completionReported: true }] }) }));
    expect(html).toContain("Implementation reported complete; acceptance is not yet recorded.");
    expect(html).not.toContain(">Accepted<");
  });
});

describe("wave review detail", () => {
  test("members render state, routes, instructions and task links", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(qk.waveReview(PROJECT, WAVE), reviewFixture({ controls: [{ action: "wave start", enabled: true, scope: WAVE }] }));
    const html = renderToStaticMarkup(
      createElement(QueryClientProvider, { client }, createElement(WaveReviewDetail, { projectId: PROJECT, waveId: WAVE })),
    );
    expect(html).toContain(">Tasks</h2>");
    expect(html).toContain("Ship the wave outcome.");
    expect(html).toContain("APP-T-0001");
    expect(html).toContain("Implement the first task contract exactly.");
    expect(html).toContain("go test ./x");
    expect(html).toContain(`/p/${PROJECT}/tasks/APP-T-0001`);
    expect(html).toContain("worker");
    expect(html).toContain("reviewer");
  });

  test("showControls=false renders review content without authority buttons", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(qk.waveReview(PROJECT, WAVE), reviewFixture({ controls: [{ action: "wave start", enabled: true, scope: WAVE }] }));
    const html = renderToStaticMarkup(
      createElement(QueryClientProvider, { client }, createElement(WaveReviewDetail, { projectId: PROJECT, waveId: WAVE, showControls: false })),
    );
    expect(html).toContain(`data-wave-review="${WAVE}"`);
    expect(html).toContain(`data-wave-instructions="APP-T-0001"`);
    expect(html).toContain("Implement the first task contract exactly.");
    expect(html).toContain(`/p/${PROJECT}/tasks/APP-T-0001`);
    expect(html).not.toContain("data-wave-control=");
    expect(html).not.toContain("data-wave-authority");
  });
});

describe("surface source contracts", () => {
  const src = (path: string) => readFileSync(`src/${path}`, "utf8");

  test("work experience and wave detail use WaveAuthorityControls", () => {
    const work = src("features/workbench/integration/WorkExperience.tsx");
    const delivery = src("features/product/DeliveryScreens.tsx");
    expect(work).toContain("WaveAuthorityControls");
    expect(work).toContain("WaveReviewDetail");
    expect(work).toContain("showControls={false}");
    expect(work).not.toContain("useWaveExecute");
    expect(delivery).toContain("WaveAuthorityControls");
    expect(delivery).toContain("WaveReviewDetail");
    expect(delivery).toContain("showControls={false}");
    expect(delivery).not.toContain("useWaveExecute");
    expect(delivery).not.toContain("waveStartBlockers");
    expect(delivery).not.toContain("Execute wave");
  });

  test("task surfaces use the direct task start hook", () => {
    for (const [path, marker] of [
      ["features/product/TaskScreens.tsx", "useTaskStart(taskId, projectId)"],
      ["features/workbench/inspector/TaskInspector.tsx", "useTaskStart(task.id, task.projectId)"],
      ["features/docs/TaskContract.tsx", "useTaskStart(task.id, projectId)"],
    ] as const) {
      const source = src(path);
      expect(source).toContain(marker);
      expect(source).not.toContain("useRunTask");
      expect(source).not.toContain("Execute once");
		expect(source).toContain("Run task");
    }
  });

  test("navigation and router expose no plan destination", () => {
    expect(src("components/Sidebar.tsx")).not.toContain("/p/$projectId/plan");
    expect(src("features/workbench/navigation/ProjectStrip.tsx")).not.toContain("/p/$projectId/plan");
    const router = src("router.tsx");
    expect(router).not.toContain("planRoute");
    expect(router).not.toContain('/p/$projectId/plan');
    expect(src("features/workbench/overview/WaveOverview.tsx")).not.toMatch(/\bplan\b/i);
  });
});

describe("task start eligibility", () => {
  const routes = {
    effectiveExecute: { profile: "worker", model: "gpt-worker", effort: "medium", harness: "codex_exec", blockers: [] },
    effectiveReview: { profile: "reviewer", model: "gpt-review", effort: "high", harness: "codex_exec", blockers: [] },
  };
  const task = (overrides: Partial<TaskDetail>): TaskDetail => ({ ...readyTask, ...routes, status: "ready", rawStatus: "ready", readiness: "ready", hasGate: false, humanAction: undefined, humanActions: [], deps: [], ...overrides });

  test("allows planned backlog and held tasks", () => {
    expect(taskRunBlocker(task({ status: "backlog", rawStatus: "backlog", readiness: "held" }))).toBeUndefined();
    expect(taskRunBlocker(task({}))).toBeUndefined();
    expect(taskRunBlocker(task({ rawStatus: "rework" }))).toBeUndefined();
  });

  test("blocks review, in-progress and terminal states", () => {
    expect(taskRunBlocker(task({ status: "review", rawStatus: "review" }))).toContain("review");
    expect(taskRunBlocker(task({ rawStatus: "in_progress" }))).toContain("in progress");
    expect(taskRunBlocker(task({ rawStatus: "done" }))).toContain("done");
    expect(taskRunBlocker(task({ rawStatus: "superseded" }))).toContain("superseded");
    expect(taskRunBlocker(task({ rawStatus: "blocked", status: "blocked" }))).toContain("backlog, ready, or rework");
    expect(taskRunBlocker(task({ rawStatus: "idea", status: "idea" }))).toContain("backlog, ready, or rework");
  });

  test("blocks human gates, unfinished dependencies and route blockers", () => {
    expect(taskRunBlocker(task({ humanAction: readyTask.humanAction }))).toContain("human action");
    expect(taskRunBlocker(task({ deps: [{ id: "APP-T-0009", title: "Dep", status: "ready" }] }))).toContain("APP-T-0009");
    expect(taskRunBlocker(task({
      effectiveExecute: { profile: "", model: "", effort: "", harness: "", blockers: ["profile disabled"], work_level: "standard" },
      effectiveReview: { profile: "reviewer", model: "gpt-review", effort: "high", harness: "codex_exec", blockers: [] },
    }))).toContain("worker and reviewer model");
  });
});
