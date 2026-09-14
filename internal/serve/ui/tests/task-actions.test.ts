import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const task = readFileSync(new URL("../src/features/docs/TaskContract.tsx", import.meta.url), "utf8");

test("task status changes use one staged transition instead of a button wall", () => {
  expect(task).toContain('aria-label="Move task to another state"');
  expect(task).toContain('<option value="">Move to...</option>');
  expect(task).toContain("{selectedStatus && (");
  expect(task).toContain("Move to {selectedStatusLabel}");
  expect(task).not.toContain('["cancelled", "Cancel"]');
});

test("secondary task workflows are progressively disclosed one at a time", () => {
  expect(task).toContain("aria-expanded={moreOpen}");
  expect(task).toContain('aria-label="Choose another task action"');

  for (const action of ["close", "land", "gate", "evidence", "feedback"]) {
    expect(task).toContain(`activeAction === "${action}"`);
  }

  expect(task).toContain('setActiveAction("")');
});

test("startable tasks expose direct task start with visible directive state", () => {
  expect(task).toContain('const runnable = !runBlocker && currentStatus !== "in_progress" && currentStatus !== "blocked"');
  expect(task).toContain("taskStart.mutate()");
  expect(task).not.toContain("human:serve");
  expect(task).toContain('directiveQueued ? "Authorized — waiting for runtime" : taskStart.isPending ? "Starting…" : "Start task"');
  for (const state of ["queued", "lapsed", "consumed"]) {
    expect(task).toContain(`task.runDirective.state === "${state}"`);
  }
  expect(task).toContain("<ActionResultLine pending={taskStart.isPending} error={taskStart.error} result={taskStart.data} />");
});

test("Serve evidence starts pending review without free-text acceptance authority", () => {
  const taskContract = readFileSync(new URL("../src/features/docs/TaskContract.tsx", import.meta.url), "utf8");
  const projectOps = readFileSync(new URL("../src/features/ops/ProjectOps.tsx", import.meta.url), "utf8");
  expect(taskContract).toContain('status: "pending_review"');
  expect(projectOps).toContain('useState("pending_review")');
  expect(projectOps).not.toContain("acceptedBy");
  expect(projectOps).not.toContain("accepted by");
  expect(projectOps).not.toContain('<option value="accepted">accepted</option>');
});
