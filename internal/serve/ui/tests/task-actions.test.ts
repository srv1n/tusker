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
