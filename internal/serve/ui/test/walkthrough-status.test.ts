import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { readFileSync } from "node:fs";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { WaveAuthorityControls, WaveMemberList, summarizeIssues, summarizeWave } from "../src/features/workbench/integration/WaveAuthority";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import { usableWaveReview, waveReviewStage, waveStartability, waveSummaryWithReview } from "../src/features/workbench/integration/integrationModel";
import { groupWaves } from "../src/features/workbench/overview/groupWaves";
import { buildFlowGraph, reviewDisplayStateFor } from "../src/features/workbench/flow/flowGraph";
import { waveReviewQuery } from "../src/lib/queries";
import { streamKeyToQueryKeys } from "../src/lib/stream";
import type { HumanAction, WaveReview } from "../src/types/domain";
import { makeWave } from "../src/features/workbench/overview/previewFixtures";
import { chainFixture } from "../src/features/workbench/flow/fixtures";
import { readyRun, readyTask } from "../previews/wux/inspector/fixtures";

const review = (overrides: Partial<WaveReview> = {}): WaveReview => ({
  schema: "tusker.wave-review/v1", waveId: "W-1", title: "Alpha", outcome: "Ship it.", state: "Planned", authorization: "inert", materialFingerprint: "fp", members: [], frontiers: [], blockers: [], controls: [], ...overrides,
});

const humanAction: HumanAction = { kind: "decision", rawKind: "decision", title: "Approval", action: "Approve this.", whyAgentCannot: "Human authority is required.", completionCondition: "Approval is recorded.", gateId: "G-1", materialRevision: "rev", blockedTaskIds: ["T-1"], covers: [], acceptance: [] };

describe("walkthrough status", () => {
  test("uses the authoritative review for ready, execution, review, failure and completion", () => {
    expect(waveReviewStage(review({ controls: [{ action: "wave start", enabled: true, scope: "W-1" }] }))).toBe("ready");
    expect(summarizeWave(review({ state: "Running", authorization: "authorized", members: [{ taskId: "T-1", title: "Build", state: "running", phase: "executing" }] })).label).toBe("Executing");
    expect(summarizeWave(review({ members: [{ taskId: "T-1", title: "Review", state: "waiting", phase: "awaiting_review" }] })).label).toBe("Awaiting review");
    expect(summarizeWave(review({ members: [{ taskId: "T-1", title: "Review", state: "reviewing", phase: "reviewing" }] })).label).toBe("Reviewing");
    expect(summarizeWave(review({ members: [{ taskId: "T-1", title: "Fail", state: "blocked", phase: "failed", waitingReason: "exit 1" }] })).label).toBe("Failed");
    const proofBlocked = review({ members: [{ taskId: "T-1", title: "Proof", state: "blocked", phase: "proof_blocked" }] });
    expect(waveReviewStage(proofBlocked)).toBe("blocked");
    expect(summarizeWave(proofBlocked).label).toBe("Blocked");
    expect(summarizeWave(review({ state: "Completed" })).label).toBe("Completed");
    expect(summarizeWave(review({ state: "Cancelled" })).label).toBe("Cancelled");
  });

  test("completion reaches history while failed reads and stale observations stay honest", () => {
    const wave = makeWave({ id: "W-1", status: "open" });
    const completed = waveStartability([wave], [review({ state: "Completed" })]);
    const completeEntry = groupWaves({ waves: [wave], tasks: [], runs: [], startability: completed }).groups.flatMap((group) => group.waves)[0];
    expect(completeEntry?.group).toBe("completed");
    expect(completeEntry?.stateLabel).toBe("Completed");

    const stale = waveStartability([wave], [review({ authorization: "stale", state: "Running", members: [{ taskId: "T-1", title: "Old", state: "running", phase: "executing" }] })]);
    const staleEntry = groupWaves({ waves: [wave], tasks: [], runs: [], startability: stale }).groups.flatMap((group) => group.waves)[0];
    expect(staleEntry?.group).toBe("unavailable");
    expect(staleEntry?.stateLabel).toBe("Status unavailable");

    expect(waveStartability([wave], [], { "W-1": new Error("review endpoint offline") })["W-1"]).toEqual({ state: "unknown", stage: "unknown", reason: "review endpoint offline" });
  });

  test("a review error or nonterminal review overrides a stale completed summary everywhere", () => {
    const wave = makeWave({ id: "W-1", status: "completed", landedAt: "2026-09-16T00:00:00Z" });
    const staleReview = review({ state: "Completed", members: [{ taskId: "T-1", title: "Old execution", state: "running", phase: "executing" }] });
    const startability = waveStartability([wave], [staleReview], { "W-1": new Error("review endpoint offline") });
    const entry = groupWaves({ waves: [wave], tasks: [], runs: [], startability }).groups.flatMap((group) => group.waves)[0];
    expect(usableWaveReview(staleReview, new Error("review endpoint offline"))).toBeUndefined();
    expect(waveSummaryWithReview(wave, staleReview, new Error("review endpoint offline"))).toMatchObject({ status: "unknown", landedAt: null });
    expect(entry?.stateLabel).toBe("Status unavailable");
    expect(entry?.group).toBe("unavailable");

    const executing = review({ state: "Running", members: [{ taskId: "T-1", title: "Current execution", state: "running", phase: "executing" }] });
    const displayed = waveSummaryWithReview(wave, executing);
    const current = groupWaves({ waves: [displayed], tasks: [], runs: [], startability: waveStartability([displayed], [executing]) }).groups.flatMap((group) => group.waves)[0];
    expect(displayed).toMatchObject({ status: "open", landedAt: null });
    expect(current?.stateLabel).toBe("Executing");
  });

  test("dependency rows name the blocker and the next owner", () => {
    const html = renderToStaticMarkup(createElement(WaveMemberList, { review: review({ members: [{ taskId: "T-2", title: "Follow-up", state: "waiting", waitingReason: "waiting for dependency T-1" }] }) }));
    expect(html).toContain("Waiting for T-1 to complete; that task’s owner acts next.");
  });

  test("review reads share a cache key and refresh on review-batch events", () => {
    expect(waveReviewQuery("W-1", "demo").queryKey).toEqual(waveReviewQuery("W-1", "demo").queryKey);
    expect(streamKeyToQueryKeys("review:batch", "demo")).toContainEqual(["wave-review", "demo"]);
    expect(streamKeyToQueryKeys("review:batch", "demo")).toContainEqual(["tasks", "demo"]);
  });

  test("graph member nodes use every known authoritative review phase, including while task details load", () => {
    const fixture = chainFixture();
    const graph = buildFlowGraph({ ...fixture, reviewMembers: [{ taskId: "WUX-T-0003", title: "Lay out the graph layers", state: "waiting", phase: "awaiting_review" }] });
    expect(graph.nodes.find((node) => node.id === "WUX-T-0003")?.state).toBe("awaiting_review");
    expect(reviewDisplayStateFor({ taskId: "T-proof", title: "Proof", state: "blocked", phase: "proof_blocked" })).toBe("proof_blocked");
    expect(reviewDisplayStateFor({ taskId: "T-cancel", title: "Cancelled", state: "cancelled" })).toBe("cancelled");
    const loadingGraph = buildFlowGraph({ memberIds: ["T-proof"], tasks: [], runs: [], reviewMembers: [{ taskId: "T-proof", title: "Proof", state: "blocked", phase: "proof_blocked" }] });
    expect(loadingGraph.nodes[0]).toMatchObject({ title: "Proof", state: "proof_blocked", missingDetail: true });
    expect(readFileSync("src/features/workbench/integration/WorkExperience.tsx", "utf8")).toContain("loading={review.isPending || details.some((query) => query.isPending)}");
  });

  test("one authoritative observation overrides stale task and run data on every wave surface", () => {
    const staleTask = { ...readyTask, id: "T-1", status: "ready" as const };
    const staleRun = { ...readyRun, taskId: "T-1", outcome: "running" as const, liveness: "fresh" as const, lane: "execute" as const };
    for (const [phase, expected] of [
      ["proof_blocked", { header: "Blocked", row: "Verification required", overview: "Blocked", drawer: "Verification required" }],
      ["failed", { header: "Failed", row: "Failed", overview: "Failed", drawer: "Failed" }],
    ] as const) {
      const observation = review({
        state: "Running", authorization: "authorized",
        members: [{ taskId: "T-1", title: staleTask.title, state: "blocked", phase, waitingReason: "authoritative review state" }],
        blockers: [{ code: phase === "failed" ? "STRICT_PROOF_FAILED" : "STRICT_PROOF_STALE", taskId: "T-1", reason: "authoritative review state", action: "repair" }],
      });
      const wave = makeWave({ id: "W-1", status: "open" });
      const overview = groupWaves({ waves: [wave], tasks: [], runs: [], startability: waveStartability([wave], [observation]) }).groups.flatMap((group) => group.waves)[0];
      expect(overview?.stateLabel).toBe(expected.overview);
      expect(renderToStaticMarkup(createElement(WaveMemberList, { review: observation }))).toContain(expected.row);
      expect(buildFlowGraph({ memberIds: ["T-1"], tasks: [staleTask], runs: [staleRun], reviewMembers: observation.members }).nodes[0]?.state).toBe(phase);

      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      client.setQueryData(waveReviewQuery("W-1", "demo").queryKey, observation);
      const header = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(WaveAuthorityControls, { projectId: "demo", waveId: "W-1" }))));
      expect(header).toContain(expected.header);
      const drawer = renderToStaticMarkup(createElement(QueryClientProvider, { client: new QueryClient({ defaultOptions: { queries: { retry: false } } }) }, createElement(ConfirmProvider, null, createElement(TaskInspector, { task: staleTask, run: staleRun, selectedTaskId: "T-1", loading: false, reviewMember: observation.members[0], onClose: () => {}, onOpenTask: () => {} }))));
      expect(drawer).toContain(expected.drawer);
      expect(drawer).not.toContain("Building now");
    }
  });

  test("active progress remains visible alongside failed work and every independent blocker", () => {
    const mixed = review({
      state: "Running", authorization: "authorized",
      members: [{ taskId: "T-1", title: "Still executing", state: "running", phase: "executing" }, { taskId: "T-2", title: "Failed", state: "blocked", phase: "failed" }],
      humanActions: [{ taskId: "T-1", taskTitle: "Still executing", action: humanAction }],
      blockers: [{ code: "STRICT_PROOF_MISSING", taskId: "T-2", reason: "missing proof", action: "run it" }, { code: "STRICT_PROOF_FAILED", taskId: "T-2", reason: "failed proof", action: "repair it" }, { code: "STRICT_PROOF_STALE", taskId: "T-2", reason: "stale proof", action: "rerun it" }, { code: "ROUTE_INVALID", taskId: "T-2", reason: "route missing", action: "repair route" }, { code: "CONTRACT_FINGERPRINT_STALE", taskId: "T-2", reason: "contract changed", action: "reconcile" }],
    });
    expect(summarizeWave(mixed).label).toBe("Failed");
    expect(summarizeIssues(mixed.blockers).map((issue) => issue.title)).toEqual(["Verification has not been recorded", "Verification failed", "Previous verification no longer applies", "Execution setup needs attention", "Work records need reconciling"]);
    const mixedWithoutHuman = { ...mixed, humanActions: [] };
    const overview = groupWaves({ waves: [makeWave({ id: "W-1" })], tasks: [], runs: [], startability: waveStartability([makeWave({ id: "W-1" })], [mixedWithoutHuman]) }).groups.flatMap((group) => group.waves)[0];
    expect(overview).toMatchObject({ stateLabel: "Failed", runningAlso: true, stateDetail: "missing proof" });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(waveReviewQuery("W-1", "demo").queryKey, mixed);
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(WaveAuthorityControls, { projectId: "demo", waveId: "W-1" }))));
    expect(html).toContain("1 running");
    expect(html).toContain("Verification has not been recorded");
    expect(html).toContain("Execution setup needs attention");
    expect(html).toContain("Work records need reconciling");
    expect(html).toContain("Approve this.");
  });

  test("overview needs-you comes from review human actions, without claiming a blocked wave is ready", () => {
    const wave = makeWave({ id: "W-1", status: "open" });
    const blocked = review({
      state: "Running", authorization: "authorized",
      members: [{ taskId: "T-1", title: "Still executing", state: "running", phase: "executing" }],
      controls: [{ action: "wave start", enabled: true, scope: "W-1" }],
      humanActions: [{ taskId: "T-2", taskTitle: "Decision", action: humanAction }],
      blockers: [{ code: "ROUTE_INVALID", taskId: "T-2", reason: "route missing", action: "repair route" }],
    });
    const startability = waveStartability([wave], [blocked]);
    const entry = groupWaves({ waves: [wave], tasks: [], runs: [], startability }).groups.flatMap((group) => group.waves)[0];
    expect(startability["W-1"]).toMatchObject({ state: "blocked", stage: "executing", humanAction: "Approve this." });
    expect(entry).toMatchObject({ group: "needs-you", stateLabel: "Needs you", runningAlso: true, stateDetail: "Approve this. route missing Work is also active." });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(waveReviewQuery("W-1", "demo").queryKey, blocked);
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(WaveAuthorityControls, { projectId: "demo", waveId: "W-1" }))));
    expect(html).toContain("Approve this.");
    expect(html).not.toContain("Start wave");
  });
});
