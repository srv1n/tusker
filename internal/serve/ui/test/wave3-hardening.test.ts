import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { isSafeHref } from "../src/features/editor/sanitize";

describe("Wave 3 production UI contracts", () => {
  test("execution settings validate a draft and save explicitly", () => {
    const source = readFileSync("src/features/product/OperationsScreens.tsx", "utf8");
    expect(source).toContain("settings.mutate");
    expect(source).toContain("Number.isFinite");
		expect(source).toContain('value="shared">Current checkout');
		expect(source).toContain("including existing uncommitted changes");
		expect(source).toContain('value="worktree">Git worktree');
    expect(source).not.toContain("onChange={(e) => settings.mutate");
  });

  test("global concurrency uses the live daemon limit and existing limits action", () => {
    const source = readFileSync("src/features/settings/app/GeneralSection.tsx", "utf8");
    const mock = readFileSync("src/features/settings/app/mock.ts", "utf8");
    expect(source).toContain("daemonQ.data?.maxActiveRuns");
    expect(source).toContain('action: "limits"');
    expect(mock).not.toContain('{ key: "Global concurrency", value: "8"');
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
    for (const path of ["src/features/search/TaskSearch.tsx", "src/components/ui/action-feedback.tsx"]) {
      const source = readFileSync(path, "utf8");
      expect(source).toContain("Tab");
      expect(source).toContain("openerRef");
      expect(source).toContain("Escape");
    }
  });
});
