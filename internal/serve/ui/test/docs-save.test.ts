import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";

describe("task prose saves", () => {
  const taskContract = readFileSync("src/features/docs/TaskContract.tsx", "utf8");
  const docReader = readFileSync("src/features/docs/DocReader.tsx", "utf8");

  test("TaskContract cannot claim durable prose persistence without a backend", () => {
    expect(taskContract).toContain("read-only");
    expect(taskContract).toContain("editable={false}");
    expect(taskContract).not.toContain("useDocEditor");
    expect(taskContract).not.toContain("SavedBanner");
    expect(taskContract).not.toContain("Approve spec");
    expect(taskContract).not.toContain(">Edit<");
    expect(taskContract).not.toContain("ed.save");
    expect(taskContract).not.toContain("onCommit={commitFrontmatter}");
  });

  test("fake document save modules are absent from the production bundle", () => {
    expect(existsSync("src/features/docs/editor.ts")).toBe(false);
    expect(existsSync("src/features/docs/banners.tsx")).toBe(true);
    expect(readFileSync("src/features/docs/banners.tsx", "utf8")).not.toContain("SavedBanner");
    expect(readFileSync("src/features/docs/banners.tsx", "utf8")).not.toContain("ConflictBanner");
    const sourceFiles = ["src/features/docs/DocReader.tsx", "src/features/docs/TaskContract.tsx", "src/features/docs/DocumentView.tsx"];
    for (const path of sourceFiles) {
      expect(readFileSync(path, "utf8")).not.toMatch(/features\/docs\/(editor|banners)/);
    }
  });

  test("the raw source escape hatch has no save or edit affordance", () => {
    const sourceView = readFileSync("src/features/docs/DocSourceView.tsx", "utf8");

    expect(sourceView).toContain("read-only");
    expect(sourceView).toContain("{doc.markdown}");
    expect(sourceView).not.toContain("useDocEditor");
    expect(sourceView).not.toContain("Save");
    expect(sourceView).not.toContain("Edit");
  });

  test("the vault document reader is explicitly read-only until a durable save exists", () => {
    expect(docReader).toContain('actions = <Mono className="text-[10.5px] uppercase tracking-[0.08em] text-faint">read-only</Mono>');
    expect(docReader).toContain("readOnly");
    expect(docReader).toContain("editable={false}");
    expect(docReader).not.toContain("useDocEditor");
    expect(docReader).not.toContain("SavedBanner");
    expect(docReader).not.toContain(">Edit<");
    expect(docReader).not.toContain("onChange={(bodyMd)");
    expect(docReader).not.toContain("Approve spec");
  });
});
