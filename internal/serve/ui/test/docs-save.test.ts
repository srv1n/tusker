import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

describe("task prose saves", () => {
  const taskContract = readFileSync("src/features/docs/TaskContract.tsx", "utf8");
  const editorState = readFileSync("src/features/docs/editor.ts", "utf8");

  test("TaskContract delegates persistence to useDocEditor", () => {
    expect(taskContract).toContain("useDocEditor(doc)");
    expect(taskContract).toContain("ed.save");
    expect(taskContract).toContain("ed.cancelEdit");
    expect(taskContract).toContain("ed.reconcile");
    expect(taskContract).toContain("ConflictBanner");
    expect(taskContract).toContain("ValidationStrip");
    expect(taskContract).not.toContain("mockValidate");
  });

  test("the existing save machine keeps CAS before validation before commit", () => {
    const cas = editorState.indexOf("conflictArmed.current");
    const validation = editorState.indexOf("mockValidate(draft)");
    const commit = editorState.indexOf("setContent(draft)");

    expect(cas).toBeGreaterThanOrEqual(0);
    expect(validation).toBeGreaterThan(cas);
    expect(commit).toBeGreaterThan(validation);
  });

  test("the raw source escape hatch has no save or edit affordance", () => {
    const sourceView = readFileSync("src/features/docs/DocSourceView.tsx", "utf8");

    expect(sourceView).toContain("read-only");
    expect(sourceView).toContain("{doc.markdown}");
    expect(sourceView).not.toContain("useDocEditor");
    expect(sourceView).not.toContain("Save");
    expect(sourceView).not.toContain("Edit");
  });
});
