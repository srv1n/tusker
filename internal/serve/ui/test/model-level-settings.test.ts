import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const settings = readFileSync(new URL("../src/features/settings/app/ProfilesSection.tsx", import.meta.url), "utf8");
const api = readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8");
const inspector = readFileSync(new URL("../src/features/workbench/inspector/TaskInspector.tsx", import.meta.url), "utf8");

test("model settings use live three-level configuration with guarded saves", () => {
  for (const value of ["Models", "execute", "review", "Global defaults", "This project"]) {
    expect(settings.toLowerCase()).toContain(value.toLowerCase());
  }
  expect(settings).toContain("levels?.levels.map");
  expect(settings).toContain("api.modelLevels()");
  expect(settings).toContain("api.modelLevelsSet");
  expect(settings).toContain("levels.revision");
  expect(settings).toContain("setError");
  expect(api).toContain('action: "set"');
  expect(api).toContain('action: "reset"');
  expect(api).toContain('action: "profile-set"');
});

test("task inspector explains authored levels and effective routes", () => {
  expect(inspector).toContain("task.authoredWorkLevel");
  expect(inspector).toContain("task.authoredReviewLevel");
  expect(inspector).toContain("task.effectiveExecute.profile");
  expect(inspector).toContain("task.effectiveReview.profile");
  expect(inspector).toContain("compatible default");
});

test("model settings preserve ordered fallbacks and inherited reset", () => {
  expect(settings).toContain('split(",")');
  expect(settings).toContain('join(", ")');
  expect(settings).toContain("primary, fallback");
  expect(settings).toContain("row[lane].overridden");
  expect(settings).toContain("api.modelLevelsReset");
  expect(settings).toContain("No profiles configured");
  expect(settings).toContain("Exact manual values are preserved and marked unverified");
  expect(settings).toContain("api.modelProfileSet");
});
