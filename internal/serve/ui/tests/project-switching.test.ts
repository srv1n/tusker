import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const readSource = (path: string) => readFileSync(path, "utf8");

test("project overview starts project reads without a projects-first boundary", () => {
  const overview = readSource("src/features/overview/ProjectOverview.tsx");
  const parentStart = overview.indexOf("export function ProjectOverview()");
  const contentMount = overview.indexOf("<OverviewContent project={immediateProject}", parentStart);
  const legacyBoundary = overview.indexOf("<QueryBoundary q={projectsQ}", parentStart);

  expect(contentMount).toBeGreaterThan(parentStart);
  expect(legacyBoundary).toBe(-1);
  expect(overview).toContain("const runsQ = useRuns(projectId)");
  expect(overview).toContain("const needsQ = useNeeds(projectId)");
  expect(overview).toContain("const epicsQ = useEpics(projectId)");
  expect(overview).toContain("const tasksQ = useTasks(projectId)");
});

test("overview task cards open task contracts while active runs open run detail", () => {
  const overview = readSource("src/features/overview/ProjectOverview.tsx");
  const taskCard = overview.slice(
    overview.indexOf("function TaskMiniCard("),
    overview.indexOf("function BlockerCard("),
  );
  const activeRun = readSource("src/features/runs/board/rows.tsx");

  expect(taskCard).toContain('to="/p/$projectId/docs"');
  expect(taskCard).toContain("params={{ projectId }}");
  expect(taskCard).toContain("search={{ path: task.id }}");
  expect(taskCard).not.toContain('to="/p/$projectId/runs/$taskId"');

  expect(activeRun).toContain('to="/p/$projectId/runs/$taskId"');
  expect(activeRun).toContain("taskId: run.taskId");
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
