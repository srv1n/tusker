import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import { AgentCoordinationSummary, reviewOverrideNeedsReason, routeBlockers, routeSummary, TaskContractDisclosure } from "../src/features/product/TaskScreens";
import { TaskInspector } from "../src/features/workbench/inspector/TaskInspector";
import { readyRun, readyTask } from "../previews/wux/inspector/fixtures";

describe("task authoring and execution experience", () => {
  test("uses one route summary for effective and actual identity", () => {
    const worker = { work_level: "standard", profile: "worker", harness: "codex_exec", model: "gpt-worker", effort: "medium", source: "model levels", reason: "tier mapping", fallbacks: ["default"], blockers: [] };
    const reviewer = { ...worker, profile: "reviewer", model: "gpt-review", effort: "high", source: "review override", fallbacks: [], blockers: [] };
    expect(routeSummary(worker)).toBe("worker · gpt-worker · medium · codex_exec");
    expect(routeBlockers({ ...readyTask, effectiveExecute: worker, effectiveReview: reviewer })).toEqual([]);

    const html = renderToStaticMarkup(createElement(AgentCoordinationSummary, {
      task: { ...readyTask, status: "ready", humanAction: undefined, humanActions: [], architect: "codex:architect", origin: "codex:origin", authoringProvenance: { source: "codex", conversation_id: "thread-1", host: "local", captured_at: "now", binding_state: "unbound" }, contactBindings: [{ contact: { projectId: "wux-preview", taskId: readyTask.id, role: "architect" as const, address: { kind: "task" as const, id: "ARCH-T-1" }, generation: 1 }, state: "bound" as const, reason: "task continuation route" }], effectiveExecute: worker, effectiveReview: reviewer },
      run: { ...readyRun, runnerProfile: "actual-worker", runnerHarness: "codex_exec", runnerEffort: "medium", model: "gpt-actual", identity: { repo_root: "/repo", workspace_path: "/repo/.work", workspace_mode: "worktree", runner: "codex", branch: "feature" } },
    }));
    expect(html).toContain("codex:architect");
    expect(html).toContain("thread-1");
    expect(html).toContain("actual-worker · gpt-actual · medium · codex_exec");
    expect(html).toContain("worktree · /repo/.work · branch feature");
    expect(html).toContain("task · ARCH-T-1 · bound · task continuation route");
    expect(html).toContain("tusker work start");
    expect(html).toContain("--current-workspace");
    expect(html).not.toContain("unbound binding");
  });

  test("names every route blocker and keeps missing routes fail-closed", () => {
    const blockers = routeBlockers({
      ...readyTask,
      effectiveExecute: { profile: "worker", model: "gpt-worker", effort: "medium", harness: "codex_exec", blockers: ["profile disabled"] },
      effectiveReview: undefined,
    });
    expect(blockers).toEqual(["Worker: profile disabled", "Reviewer: route preview unavailable"]);
  });

  test("requires reasons for new review-tier overrides but preserves legacy blanks", () => {
    const base = { workLevel: "standard", effectiveWorkLevel: "standard", reviewLevel: "light", initialWorkLevel: "standard", initialReviewLevel: "", reviewReason: "" };
    expect(reviewOverrideNeedsReason(base)).toBe(true);
    expect(reviewOverrideNeedsReason({ ...base, reviewReason: "Security review needs more scrutiny." })).toBe(false);
    expect(reviewOverrideNeedsReason({ ...base, initialReviewLevel: "light" })).toBe(false);
    expect(reviewOverrideNeedsReason({ ...base, authoredReviewReason: "Legacy reason" })).toBe(true);
  });

  test("keeps every open human action visible in the inspector", () => {
    const first = readyTask.humanAction!;
    const second = { ...first, gateId: "WUX-G-0102", title: "Confirm the copy", action: "Confirm the wording." };
    const task = { ...readyTask, humanAction: first, humanActions: [first, second], gates: [{ id: first.gateId, kind: "review" as const, owner: "human:reviewer" }, { id: second.gateId, kind: "review" as const, owner: "human:copy" }] };
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(TaskInspector, { task, run: readyRun, selectedTaskId: task.id, loading: false, onClose: () => {}, onOpenTask: () => {} }))));
    expect(html).toContain("Owner: human:reviewer");
    expect(html).toContain("Owner: human:copy");
    expect(html).toContain("Confirm the wording.");
  });

  test("renders the canonical task body in an explicit disclosure", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const body = "# Implementation notes\n\nUse the existing route.\n\n## Non-goals\n\nNo new store.";
    const render = (open: boolean) => renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(TaskContractDisclosure, { body, projectId: "wux-preview", open })));
    const expanded = render(true);
    const collapsed = render(false);
    expect(expanded).toContain("Full task contract");
    expect(expanded).toContain("Implementation notes");
    expect(expanded).toContain("No new store.");
    expect(expanded).toContain("<details open");
    expect(collapsed).toContain("<details");
    expect(collapsed).not.toContain("<details open");
  });

  test("keeps the three surfaces on the same authoring contract", () => {
    const task = readFileSync("src/features/product/TaskScreens.tsx", "utf8");
    const wave = readFileSync("src/features/product/DeliveryScreens.tsx", "utf8");
    const inspector = readFileSync("src/features/workbench/inspector/TaskInspector.tsx", "utf8");
    const report = readFileSync("../../../docs/reports/task-authoring/experience.md", "utf8");
    expect(task).toContain("taskRunBlocker(detail)");
    expect(task).toContain("HumanActionCard");
    expect(task).toContain('aria-label="Task reviewer profile"');
    expect(task).not.toContain("<option value=\"\">Unclassified</option>");
    expect(task).toContain("TaskContractDisclosure");
    expect(task).not.toContain("(default)");
    expect(wave).toContain("Execution routes");
    expect(wave).toContain("Read the full wave brief");
    expect(wave).toContain("WaveAuthorityControls");
    expect(inspector).toContain("Pause sender until this question is answered");
    expect(inspector).toContain("TaskContractDisclosure");
    expect(report).toContain("The contract an agent must be able to read");
  });
});
