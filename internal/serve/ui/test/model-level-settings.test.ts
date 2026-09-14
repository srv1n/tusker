import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const tiers = readFileSync(new URL("../src/features/settings/app/TiersSection.tsx", import.meta.url), "utf8");
const api = readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8");
const tasks = readFileSync(new URL("../src/features/product/TaskScreens.tsx", import.meta.url), "utf8");

test("tier settings use live three-level configuration with guarded saves", () => {
  for (const value of ["Tier 1", "Tier 2", "Tier 3", "Worker", "Reviewer", "Save changes"]) {
    expect(tiers.toLowerCase()).toContain(value.toLowerCase());
  }
  expect(tiers).toContain("api.modelLevels(projectId)");
  expect(tiers).toContain("api.modelLevelsSet");
  expect(tiers).toContain("setLevels(next)");
  expect(tiers).toContain("profile.eligible_tiers.includes(tier)");
  expect(tiers).toContain("setError");
  expect(api).toContain('action: "set"');
  expect(api).toContain('action: "reset"');
  expect(api).toContain('action: "profile-set"');
});

test("task routing is three immediate choices with compact missing-model copy", () => {
  expect(tasks).toContain("Tier 2 · Standard");
  expect(tasks).toContain('aria-label="Task worker profile"');
  expect(tasks).toContain('aria-label="Task reviewer profile"');
  expect(tasks).toContain("detail.authoredWorkLevel");
  expect(tasks).toContain("actualRouteLabel");
  expect(tasks).toContain("No model is configured for");
  expect(tasks).toContain("update({ executeProfile: value || null })");
  expect(tasks).not.toContain("Advanced overrides");
});

test("tiers preserve explicit fallbacks and project inheritance", () => {
  expect(tiers).toContain("Fallbacks");
  expect(tiers).toContain("row[lane].overridden");
  expect(tiers).toContain("api.modelLevelsReset");
  expect(tiers).toContain("Choose profile");
  expect(tiers).toContain("unavailable");
});
