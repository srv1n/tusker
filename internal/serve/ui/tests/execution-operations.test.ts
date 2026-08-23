import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const screen = readFileSync(new URL("../src/features/executions/ExecutionOperations.tsx", import.meta.url), "utf8");
const api = readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8");
const queries = readFileSync(new URL("../src/lib/queries.ts", import.meta.url), "utf8");
const types = readFileSync(new URL("../src/types/domain.ts", import.meta.url), "utf8");
const router = readFileSync(new URL("../src/router.tsx", import.meta.url), "utf8");
const sidebar = readFileSync(new URL("../src/components/Sidebar.tsx", import.meta.url), "utf8");
const operations = readFileSync(new URL("../src/features/product/OperationsScreens.tsx", import.meta.url), "utf8");

test("execution operations keeps graph, inbox, authority boundary and timeline seams explicit", () => {
  for (const text of ["Execution tree", "Unbound direct work", "Guarded binding", "Earlier unbound history remains", "Convergent timeline", "Partial provider visibility", "provider-owned", "Tusker-managed", "Reset", "Older", "Newer"]) expect(screen).toContain(text);
  for (const filter of ["task", "wave", "provider_id", "agent_type", "source", "binding", "lifecycle"]) expect(screen).toContain(filter);
  expect(screen).toContain("useExecutionRename");
  expect(screen).toContain("useExecutionBindingPreview");
  expect(screen).toContain("useExecutionBind");
  expect(screen).toContain("inbox.data?.executions.find");
});

test("API contracts separate provider observation facts from executable controls", () => {
  expect(types).toContain("interface ProviderCapabilityFact");
  expect(types).toContain("interface ExecutionCapability");
  expect(api).toContain("executionBindingPreview");
  expect(api).toContain("executionRename");
  expect(api).toContain("executionBind");
  expect(queries).toContain("useExecutionTimeline");
  expect(router).toContain('path: "diagnostics/executions"');
  expect(operations).toContain('"/p/$projectId/diagnostics/executions"');
  expect(sidebar).not.toContain('label: "Executions"');
});
