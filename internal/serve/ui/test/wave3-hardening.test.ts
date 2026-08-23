import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { isSafeHref } from "../src/features/editor/sanitize";

describe("Wave 3 production UI contracts", () => {
  test("execution settings validate a draft and save explicitly", () => {
    const source = readFileSync("src/features/product/OperationsScreens.tsx", "utf8");
    expect(source).toContain("settings.mutate");
    expect(source).toContain("Number.isFinite");
    expect(source).not.toContain("onChange={(e) => settings.mutate");
  });

  test("markdown uses the href sanitizer and strict external rel", () => {
    const source = readFileSync("src/features/docs/Markdown.tsx", "utf8");
    expect(source).toContain("isSafeHref");
    expect(source).toContain("noopener noreferrer");
    expect(source).toContain("Unsafe link blocked");
  });

  test("href sanitizer rejects browser-normalized network paths and controls", () => {
    expect(isSafeHref("/local/doc")).toBe(true);
    expect(isSafeHref("//evil.example/path")).toBe(false);
    expect(isSafeHref("/\\\\evil.example/path")).toBe(false);
    expect(isSafeHref("/local\nheader")).toBe(false);
    expect(isSafeHref("https://example.com/docs")).toBe(true);
  });

  test("modal surfaces include keyboard containment and focus restoration", () => {
    for (const path of ["src/features/search/TaskSearch.tsx", "src/components/Sidebar.tsx", "src/components/ui/action-feedback.tsx"]) {
      const source = readFileSync(path, "utf8");
      expect(source).toContain("Tab");
      expect(source).toContain("openerRef");
      expect(source).toContain("Escape");
    }
  });
});
