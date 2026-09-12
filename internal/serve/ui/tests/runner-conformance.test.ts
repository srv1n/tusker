import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const profiles = readFileSync(new URL("../src/features/settings/app/ProfilesSection.tsx", import.meta.url), "utf8");
const api = readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8");
const types = readFileSync(new URL("../src/types/domain.ts", import.meta.url), "utf8");

test("runner conformance is a first-class settings action", () => {
  expect(profiles).toContain('"Running test…" : "Run test"');
  expect(profiles).toContain("does not save the draft");
  expect(profiles).toContain("Check setup");
  expect(profiles).toContain("a live test may consume model usage");
  expect(profiles).toContain("api.runnerConformance");
  expect(api).toContain("/runner/conformance");
  expect(api).toContain("responseFailureMessage");
  expect(api).toContain("cases?: Array");
  expect(types).toContain('schema: "tusker.runner-conformance/v1"');
});

test("full access profiles remain saveable and testable", () => {
  expect(profiles).not.toContain("canLiveTest");
  expect(profiles).not.toContain("Full access cannot be safely verified from Settings");
});
