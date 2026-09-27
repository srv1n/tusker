/*
  TSK-T-0013 — wave overview is glanceable.

  Server-renders WaveAuthorityControls and WaveMemberList against seeded
  WaveReview fixtures and asserts the compact contract: the server state
  badge with its reason, one primary action, no boilerplate prose; expected
  dependency and capacity waits are neutral and named; failures stay
  specific to task and lane. Raw fingerprints and codes stay off the wave
  surface entirely; Diagnostics and the JSON review carry them.
*/

import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { qk } from "../src/lib/queries";
import { WaveAuthorityControls, WaveMemberList, capacityConstraint, summarizeIssues } from "../src/features/workbench/integration/WaveAuthority";
import { makeWave, sampleState } from "../src/features/workbench/overview/previewFixtures";
import type { TaskState, WaveReview, WaveReviewMember } from "../src/types/domain";

const PROJECT = "beta";
const WAVE = "W-0003";

function member(overrides: Partial<WaveReviewMember>): WaveReviewMember {
  return { taskId: "BET-T-0001", title: "Task one", state: "ready", ...overrides };
}

function reviewFixture(overrides: Partial<WaveReview>): WaveReview {
  return {
    schema: "tusker.wave-review/v1",
    waveId: WAVE,
    title: "Beta wave",
    outcome: "Deliver the beta outcome.",
    state: "Planned",
    authorization: "inert",
    materialFingerprint: "sha256:deadbeef",
    members: [member({})],
    frontiers: [["BET-T-0001"]],
    blockers: [],
    controls: [],
    ...overrides,
  };
}

function renderControls(review: WaveReview, state?: TaskState): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(qk.waveReview(PROJECT, WAVE), review);
  if (state) client.setQueryData(qk.wave(PROJECT, WAVE), makeWave({ id: WAVE, title: "Beta wave", state }));
  return renderToStaticMarkup(
    createElement(QueryClientProvider, { client }, createElement(WaveAuthorityControls, { projectId: PROJECT, waveId: WAVE })),
  );
}

/** Member rows with server states, keyed by task ID. */
function renderMembers(review: WaveReview, states: Record<string, TaskState> = {}): string {
  const members = Object.entries(states).map(([id, state]) => ({ id, title: id, group: "", status: "", state, proof: "" }));
  return renderToStaticMarkup(createElement(WaveMemberList, { review, projectId: PROJECT, members }));
}

describe("compact wave summary (A1)", () => {
  test("ready wave: state badge, green start, no boilerplate", () => {
    const html = renderControls(reviewFixture({ controls: [{ action: "wave start", enabled: true, scope: WAVE }] }), sampleState("planned", "1 planned"));
    expect(html).toContain('data-task-state="planned"');
    expect(html).toContain("1 planned");
    expect(html).toContain('data-wave-control="wave start"');
    expect(html).toContain("bg-pass");
    expect(html).not.toContain("Ready to start");
    expect(html).not.toContain("Work is prepared");
    expect(html).not.toContain("This starts only the currently eligible tasks");
    expect(html).not.toContain("No current diagnostic records");
  });

  test("executing wave: state reason plus Pause with its one hint", () => {
    const html = renderControls(reviewFixture({
      state: "Running",
      authorization: "authorized",
      members: [member({ state: "running", phase: "executing" }), member({ taskId: "BET-T-0002", title: "Task two" })],
      controls: [{ action: "wave pause", enabled: true, scope: WAVE }],
    }), sampleState("working", "1 working, 1 planned"));
    expect(html).toContain(">Working<");
    expect(html).toContain("1 working, 1 planned");
    expect(html).toContain('data-wave-control="wave pause"');
    expect(html).toContain("Running. Pause stops new tasks; active tasks may finish.");
    expect(html).not.toContain("Agents are actively progressing");
  });

  test("reviewing wave: badge plus review reason", () => {
    const html = renderControls(reviewFixture({
      state: "Waiting",
      authorization: "authorized",
      members: [member({ state: "reviewing", phase: "reviewing", lane: "review" })],
      controls: [{ action: "wave pause", enabled: true, scope: WAVE }],
    }), sampleState("in_review", "1 in review"));
    expect(html).toContain(">In review<");
    expect(html).toContain("1 in review");
  });

  test("paused wave: Resume with resume-semantics hint, never a start", () => {
    const html = renderControls(reviewFixture({
      state: "Paused",
      authorization: "paused",
      controls: [{ action: "wave resume", enabled: true, scope: WAVE }],
    }), sampleState("blocked", "paused by you", { reason_code: "paused" }));
    expect(html).toContain("paused by you");
    expect(html).toContain('data-wave-control="wave resume"');
    expect(html).toContain("does not retry failed tasks");
    expect(html).not.toContain('data-wave-control="wave start"');
  });

  test("blocked wave: grayed Start wave plus the reason it cannot start", () => {
    const html = renderControls(reviewFixture({
      blockers: [{ code: "STRICT_PROOF_MISSING", taskId: "BET-T-0001", reason: "command proof is missing", action: "re-run current command proof" }],
      controls: [{ action: "wave start", enabled: false, scope: WAVE, reason: "resolve global blockers before wave start" }],
    }));
    expect(html).toContain('data-wave-control-refused="wave start"');
    expect(html).toContain("Verification has not been recorded");
    expect(html).not.toContain('data-wave-control="wave start"');
  });

  test("completed wave: state only, no action", () => {
    const html = renderControls(reviewFixture({
      state: "Completed",
      authorization: "authorized",
      members: [member({ state: "completed", phase: "completed" })],
      controls: [{ action: "wave start", enabled: false, scope: WAVE, reason: "wave is already complete" }],
    }), sampleState("done"));
    expect(html).toContain(">Done<");
    expect(html).not.toContain("data-wave-control");
  });
});

describe("expected waits stay neutral and named (A2)", () => {
  test("dependency wait shows the server reason, not a raw code", () => {
    const review = reviewFixture({
      members: [
        member({ taskId: "BET-T-0001", title: "Base module", state: "running", phase: "executing" }),
        member({ taskId: "BET-T-0002", title: "Dependent module", state: "waiting", waitingReason: "waiting for dependency BET-T-0001" }),
      ],
      frontiers: [["BET-T-0001"], ["BET-T-0002"]],
    });
    const html = renderMembers(review, { "BET-T-0001": sampleState("working"), "BET-T-0002": sampleState("planned", "waiting on Base module") });
    expect(html).toContain("waiting on Base module");
    expect(html).not.toContain("DEPENDENCY_WAITING");
    const controls = renderControls(review, sampleState("working", "1 working, 1 planned"));
    expect(controls).not.toContain("DEPENDENCY_WAITING");
    expect(controls).toContain("1 working, 1 planned");
  });

  test("raw fingerprint and codes never reach the wave surface", () => {
    const html = renderControls(reviewFixture({
      blockers: [{ code: "STRICT_PROOF_MISSING", taskId: "BET-T-0001", reason: "command proof is missing", action: "re-run current command proof" }],
      controls: [{ action: "wave start", enabled: false, scope: WAVE }],
    }));
    expect(html).not.toContain("Technical details");
    expect(html).not.toContain("Material fingerprint");
    expect(html).not.toContain("STRICT_PROOF_MISSING");
    expect(html).not.toContain("sha256:deadbeef");
    expect(html).toContain("Verification has not been recorded");
    expect(html).toContain(`/p/${PROJECT}/tasks/BET-T-0001`);
  });
});

describe("one-slot capacity (A3)", () => {
  const oneSlot = reviewFixture({
    state: "Waiting",
    authorization: "authorized",
    members: [
      member({ taskId: "BET-T-0001", title: "Running sibling", state: "running", phase: "executing" }),
      member({ taskId: "BET-T-0002", title: "Slot-bound sibling", state: "waiting", phase: "capacity_wait", waitingReason: "queued for dispatch; waiting for an execution slot — project capacity 1/1 in use", responsible: "daemon" }),
    ],
    controls: [{ action: "wave pause", enabled: true, scope: WAVE }],
  });

  test("capacity wait reads waiting-for-a-slot and the wave shows the limit with Settings", () => {
    const controls = renderControls(oneSlot);
    expect(controls).toContain("1 task at a time");
    expect(controls).toContain(`/p/${PROJECT}/settings`);
    const members = renderMembers(oneSlot, { "BET-T-0002": sampleState("planned", "no free slot") });
    expect(members).toContain("no free slot");
    expect(members).not.toContain("queued for dispatch");
  });

  test("released slot stops naming capacity and never promises parallelism", () => {
    const released = reviewFixture({
      state: "Waiting",
      authorization: "authorized",
      members: [
        member({ taskId: "BET-T-0001", title: "Running sibling", state: "running", phase: "executing" }),
        member({ taskId: "BET-T-0002", title: "Queued sibling", state: "waiting", phase: "queued", waitingReason: "queued for dispatch", responsible: "daemon" }),
      ],
      controls: [{ action: "wave pause", enabled: true, scope: WAVE }],
    });
    expect(capacityConstraint(released)).toBeUndefined();
    expect(renderControls(released)).not.toContain("task at a time");
    expect(renderControls(oneSlot)).not.toContain("parallel");
  });
});

describe("distinct failures with the responsible actor (A4)", () => {
  test("implementation, review, verification and setup failures stay distinct", () => {
    const issues = summarizeIssues(
      [
        { code: "RUNTIME_FAILED", taskId: "BET-T-0001", reason: "worker exited 1", action: "inspect the failed execute attempt for BET-T-0001" },
        { code: "RUNTIME_FAILED", taskId: "BET-T-0002", reason: "reviewer crashed", action: "inspect the failed review attempt for BET-T-0002" },
        { code: "STRICT_PROOF_FAILED", taskId: "BET-T-0003", reason: "check failed", action: "re-run current command proof" },
        { code: "ROUTE_INVALID", taskId: "BET-T-0004", reason: "no route", action: "repair the route configuration" },
      ],
      [
        member({ taskId: "BET-T-0001", title: "Builder task", lane: "execute" }),
        member({ taskId: "BET-T-0002", title: "Reviewed task", lane: "review" }),
      ],
    );
    const titles = issues.map((issue) => issue.title);
    expect(titles).toContain("Implementation failed — Builder task");
    expect(titles).toContain("Review failed — Reviewed task");
    expect(titles).toContain("Verification failed");
    expect(titles).toContain("Execution setup needs attention");
    expect(titles).not.toContain("Wave setup needs attention");
    const failed = issues.find((issue) => issue.title.startsWith("Implementation failed"));
    expect(failed?.next).toBe("inspect the failed execute attempt for BET-T-0001");
  });

  test("member list keeps the actor and reason on the failed row", () => {
    const html = renderMembers(reviewFixture({
      members: [member({ state: "blocked", phase: "failed", lane: "review", waitingReason: "reviewer crashed" })],
    }), { "BET-T-0001": sampleState("blocked", "reviewer crashed", { reason_code: "crashed" }) });
    expect(html).toContain(">Blocked<");
    expect(html).toContain("reviewer crashed");
  });
});
