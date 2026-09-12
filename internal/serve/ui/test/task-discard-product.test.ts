import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("../src/features/product/TaskScreens.tsx", import.meta.url), "utf8");

describe("product task discard", () => {
  test("previews dependency impact before exposing the destructive action", () => {
    expect(source).toContain("useDiscardTask");
    expect(source).toContain("Review discard impact");
    expect(source).toContain('aria-label="Resolve downstream dependencies"');
    expect(source).toContain('typeToConfirm: task.id');
    expect(source).toContain('dependents: resolution || undefined');
  });
});
