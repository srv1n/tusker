import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const profiles = readFileSync(new URL("../src/features/settings/app/ProfilesSection.tsx", import.meta.url), "utf8");
const api = readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8");
const types = readFileSync(new URL("../src/types/domain.ts", import.meta.url), "utf8");

test("runner conformance is a first-class settings action", () => {
  expect(profiles).toContain("Test an installed harness");
  expect(profiles).toContain("Run local checks");
  expect(profiles).toContain("Run live canary");
  expect(profiles).toContain("Print token");
  expect(profiles).toContain("One-second timer");
  expect(profiles).toContain("api.runnerConformance");
  expect(api).toContain("/runner/conformance");
  expect(types).toContain('schema: "tusker.runner-conformance/v1"');
});
