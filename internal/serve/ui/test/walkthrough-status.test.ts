import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterContextProvider } from "@tanstack/react-router";
import { readFileSync } from "node:fs";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { WaveAuthorityControls, WaveMemberList, summarizeIssues } from "../src/features/workbench/integration/WaveAuthority";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import { usableWaveReview, waveReviewStage, waveSummaryWithReview } from "../src/features/workbench/integration/integrationModel";
import { buildFlowGraph } from "../src/features/workbench/flow/flowGraph";
import { qk, waveReviewQuery } from "../src/lib/queries";
import { streamKeyToQueryKeys } from "../src/lib/stream";
import type { HumanAction, WaveReview } from "../src/types/domain";
import { makeWave, sampleState } from "../src/features/workbench/overview/previewFixtures";
import { chainFixture } from "../src/features/workbench/flow/fixtures";
import * as fx from "../previews/wux/inspector/fixtures";

const readyRun = fx.readyRun;
const readyTask = { ...fx.readyTask, state: sampleState("planned") };

const review = (overrides: Partial<WaveReview> = {}): WaveReview => ({
  schema: "tusker.wave-review/v1", waveId: "W-1", title: "Alpha", outcome: "Ship it.", state: "Planned", authorization: "inert", materialFingerprint: "fp", members: [], frontiers: [], blockers: [], controls: [], ...overrides,
});

const humanAction: HumanAction = { kind: "decision", rawKind: "decision", title: "Approval", action: "Approve this.", whyAgentCannot: "Human authority is required.", completionCondition: "Approval is recorded.", gateId: "G-1", materialRevision: "rev", blockedTaskIds: ["T-1"], covers: [], acceptance: [] };

// 834e84a4 added a run-detail Link to the inspector; server renders need router context.
function renderWithRouter(element: ReturnType<typeof createElement>) {
  const root = createRootRoute();
  const runRoute = createRoute({ getParentRoute: () => root, path: "/p/$projectId/runs/$taskId" });
  const router = createRouter({ routeTree: root.addChildren([runRoute]), history: createMemoryHistory({ initialEntries: ["/"] }) });
  return renderToStaticMarkup(createElement(RouterContextProvider, { router }, element));
}

describe("walkthrough status", () => {
  test("uses the authoritative review for the wave start stage", () => {
    expect(waveReviewStage(review({ controls: [{ action: "wave start", enabled: true, scope: "W-1" }] }))).toBe("ready");
    const proofBlocked = review({ members: [{ taskId: "T-1", title: "Proof", state: "blocked", phase: "proof_blocked" }] });
    expect(waveReviewStage(proofBlocked)).toBe("blocked");
  });

  test("a review error or nonterminal review overrides a stale completed summary", () => {
    const wave = makeWave({ id: "W-1", status: "completed", landedAt: "2026-09-16T00:00:00Z" });
    const staleReview = review({ state: "Completed", members: [{ taskId: "T-1", title: "Old execution", state: "running", phase: "executing" }] });
    expect(usableWaveReview(staleReview, new Error("review endpoint offline"))).toBeUndefined();
    expect(waveSummaryWithReview(wave, staleReview, new Error("review endpoint offline"))).toMatchObject({ status: "unknown", landedAt: null });

    const executing = review({ state: "Running", members: [{ taskId: "T-1", title: "Current execution", state: "running", phase: "executing" }] });
    const displayed = waveSummaryWithReview(wave, executing);
    expect(displayed).toMatchObject({ status: "open", landedAt: null });
  });

  test("member rows show the server state and its reason", () => {
    const html = renderToStaticMarkup(createElement(WaveMemberList, {
      review: review({ members: [{ taskId: "T-2", title: "Follow-up", state: "waiting", waitingReason: "waiting for dependency T-1" }] }),
      members: [{ id: "T-2", title: "Follow-up", group: "", status: "ready", state: sampleState("planned", "waiting on T-1"), proof: "" }],
    }));
    expect(html).toContain('data-task-state="planned"');
    expect(html).toContain("waiting on T-1");
  });

  test("review reads share a cache key and refresh on review-batch events", () => {
    expect(waveReviewQuery("W-1", "demo").queryKey).toEqual(waveReviewQuery("W-1", "demo").queryKey);
    expect(streamKeyToQueryKeys("review:batch", "demo")).toContainEqual(["wave-review", "demo"]);
    expect(streamKeyToQueryKeys("review:batch", "demo")).toContainEqual(["tasks", "demo"]);
  });

  test("graph member nodes use the wave summary state while task details load", () => {
    const loadingGraph = buildFlowGraph({ memberIds: ["T-proof"], tasks: [], runs: [], members: [{ id: "T-proof", title: "Proof", group: "", status: "review", state: sampleState("in_review", "reviewing"), proof: "" }] });
    expect(loadingGraph.nodes[0]).toMatchObject({ title: "Proof", missingDetail: true });
    expect(loadingGraph.nodes[0]?.state?.state).toBe("in_review");
    expect(readFileSync("src/features/workbench/integration/WorkExperience.tsx", "utf8")).toContain("loading={review.isPending || details.some((query) => query.isPending)}");
  });

  test("one authoritative observation overrides stale task and run data on every wave surface", () => {
    const staleTask = { ...readyTask, id: "T-1", status: "ready" as const };
    const staleRun = { ...readyRun, taskId: "T-1", outcome: "running" as const, liveness: "fresh" as const, lane: "execute" as const };
    for (const [phase, expected] of [
      ["proof_blocked", { reason: "verification required" }],
      ["failed", { reason: "crashed" }],
    ] as const) {
      const state = sampleState("blocked", expected.reason);
      const observation = review({
        state: "Running", authorization: "authorized",
        members: [{ taskId: "T-1", title: staleTask.title, state: "blocked", phase, waitingReason: "authoritative review state" }],
        blockers: [{ code: phase === "failed" ? "STRICT_PROOF_FAILED" : "STRICT_PROOF_STALE", taskId: "T-1", reason: "authoritative review state", action: "repair" }],
      });
      const members = [{ id: "T-1", title: staleTask.title, group: "", status: "ready", state, proof: "" }];
      const row = renderToStaticMarkup(createElement(WaveMemberList, { review: observation, members }));
      expect(row).toContain('data-task-state="blocked"');
      expect(row).toContain(expected.reason);
      expect(buildFlowGraph({ memberIds: ["T-1"], tasks: [{ ...staleTask, state }], runs: [staleRun] }).nodes[0]?.state?.state).toBe("blocked");

      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      client.setQueryData(waveReviewQuery("W-1", "demo").queryKey, observation);
      client.setQueryData(qk.wave("demo", "W-1"), makeWave({ id: "W-1", title: "Alpha", state: sampleState("blocked", "1 blocked") }));
      const header = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(WaveAuthorityControls, { projectId: "demo", waveId: "W-1" }))));
      expect(header).toContain('data-task-state="blocked"');
      expect(header).toContain("1 blocked");
      const drawer = renderWithRouter(createElement(QueryClientProvider, { client: new QueryClient({ defaultOptions: { queries: { retry: false } } }) }, createElement(ConfirmProvider, null, createElement(TaskInspector, { task: { ...staleTask, state }, run: staleRun, selectedTaskId: "T-1", loading: false, reviewMember: observation.members[0], onClose: () => {}, onOpenTask: () => {} }))));
      expect(drawer).toContain('data-task-state="blocked"');
      expect(drawer).toContain(expected.reason);
      expect(drawer).not.toContain('data-task-state="working"');
    }
  });

  test("active progress remains visible alongside failed work and every independent blocker", () => {
    const mixed = review({
      state: "Running", authorization: "authorized",
      members: [{ taskId: "T-1", title: "Still executing", state: "running", phase: "executing" }, { taskId: "T-2", title: "Failed", state: "blocked", phase: "failed" }],
      humanActions: [{ taskId: "T-1", taskTitle: "Still executing", action: humanAction }],
      blockers: [{ code: "STRICT_PROOF_MISSING", taskId: "T-2", reason: "missing proof", action: "run it" }, { code: "STRICT_PROOF_FAILED", taskId: "T-2", reason: "failed proof", action: "repair it" }, { code: "STRICT_PROOF_STALE", taskId: "T-2", reason: "stale proof", action: "rerun it" }, { code: "ROUTE_INVALID", taskId: "T-2", reason: "route missing", action: "repair route" }, { code: "CONTRACT_FINGERPRINT_STALE", taskId: "T-2", reason: "contract changed", action: "reconcile" }],
    });
    expect(summarizeIssues(mixed.blockers).map((issue) => issue.title)).toEqual(["Verification has not been recorded", "Verification failed", "Previous verification no longer applies", "Execution setup needs attention", "Work records need reconciling"]);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(waveReviewQuery("W-1", "demo").queryKey, mixed);
    client.setQueryData(qk.wave("demo", "W-1"), makeWave({ id: "W-1", title: "Alpha", state: sampleState("blocked", "1 working, 1 blocked") }));
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(WaveAuthorityControls, { projectId: "demo", waveId: "W-1" }))));
    expect(html).toContain("1 working, 1 blocked");
    expect(html).toContain("Verification has not been recorded");
    expect(html).toContain("Execution setup needs attention");
    expect(html).toContain("Work records need reconciling");
    expect(html).toContain("Approve this.");
  });
});
