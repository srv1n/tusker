import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { projectVisibleInNavigation, type ProjectSummary } from "../src/types/domain";

const readSource = (path: string) => readFileSync(path, "utf8");

test("normal navigation excludes auxiliary and unavailable registrations", () => {
  const project = (overrides: Partial<ProjectSummary> = {}) => ({
    id: "project", name: "project", repoRoot: "/repo", vaultRoot: "/repo/.tusker",
    automationEnabled: false, health: "healthy", needsCount: 0, activeRuns: 0,
    worstLiveness: null, daemonConnected: true, ...overrides,
  }) satisfies ProjectSummary;

  expect(projectVisibleInNavigation(project())).toBe(true);
  expect(projectVisibleInNavigation(project({ auxiliary: true }))).toBe(false);
  expect(projectVisibleInNavigation(project({ checkouts: [{ id: "missing", label: "Primary checkout", repoRoot: "/missing", vaultRoot: "/missing/.tusker", git: false, detached: false, available: false, activity: "unavailable", activeRuns: 0, health: "error" }] }))).toBe(false);
});

test("detail and mutation transports carry project identity", () => {
  const api = readSource("src/lib/api.ts");
  const queries = readSource("src/lib/queries.ts");

  expect(api).toContain("function withProject(path: string, projectId?: string)");
  for (const endpoint of [
    "`/runs/${taskId}`",
    "`/tasks/${id}`",
    "`/docs/${encodeURI(path)}`",
    "`/tasks/${taskId}/status`",
    "`/gates/${gateId}/${action}`",
  ]) {
    expect(api).toContain(`withProject(${endpoint}`);
  }
  expect(queries).toContain('["task", projectId ?? "all", id]');
  expect(queries).toContain('["run", projectId ?? "all", taskId]');
});

test("visited project queries remain warm while SSE owns freshness", () => {
  const main = readSource("src/main.tsx");
  expect(main).toContain("staleTime: 30_000");
  expect(main).toContain("connectLiveStream(queryClient");
});
