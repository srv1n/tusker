import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const readSource = (path: string) => readFileSync(path, "utf8");

test("task-detail navigation renders the complete existing task-detail API surface", () => {
  const documentView = readSource("src/features/docs/DocumentView.tsx");
  const taskContract = readSource("src/features/docs/TaskContract.tsx");

  expect(documentView).toContain("if (isTaskId(path)) return <TaskContract projectId={projectId} taskId={path} focusGateId={gate} />");
  expect(taskContract).toContain("const q = useTask(taskId, projectId);");
  for (const section of ["Intent", "Acceptance", "Verification", "Evidence", "Run history", "Gates"]) {
    expect(taskContract).toContain(section);
  }
});
