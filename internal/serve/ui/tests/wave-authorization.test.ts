import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const panel = readFileSync(new URL("../src/features/ops/ProjectOps.tsx", import.meta.url), "utf8");
const domain = readFileSync(new URL("../src/types/domain.ts", import.meta.url), "utf8");

test("wave cards separate derived completion from execution authorization", () => {
  expect(panel).toContain("wave.status");
  expect(panel).toContain("wave.authorization.state");
  expect(panel).toContain("wave.authorization.action");
  expect(domain).toContain('state: "disarmed" | "armed" | "paused" | "stale"');
});
