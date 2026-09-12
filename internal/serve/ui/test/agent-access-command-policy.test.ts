import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const component = readFileSync(new URL("../src/features/settings/app/CommandPolicyDetails.tsx", import.meta.url), "utf8");
const domain = readFileSync(new URL("../src/types/domain.ts", import.meta.url), "utf8");
const profiles = readFileSync(new URL("../src/features/settings/app/ProfilesSection.tsx", import.meta.url), "utf8");

test("agent adapter details consume the shared policy and show all outcomes", () => {
  for (const value of ["CommandPolicy", "Agent adapter", "Automatic", "Ask each time", "Block", "Provider coverage", "Allow-once approval cannot override"]) {
    expect(component).toContain(value);
  }
  expect(component).not.toContain("Trusted-agent boundary");
  expect(component).not.toContain("Python script");
  expect(domain).toContain("command_policy?: CommandPolicy");
  expect(profiles).toContain("<CommandPolicyDetails");
  expect(profiles).toContain("resolvedAccess?.command_policy");
});

test("unsupported provider controls use a text alert, not color alone", () => {
  expect(component).toContain('role="alert"');
  expect(component).toContain('>{unsupported ? "Unavailable"');
  expect(component).toContain("Required access restriction is unavailable");
});
