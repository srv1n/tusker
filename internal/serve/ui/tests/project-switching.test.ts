import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const readSource = (path: string) => readFileSync(path, "utf8");

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
