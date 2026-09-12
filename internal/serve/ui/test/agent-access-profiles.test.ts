import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const profiles = readFileSync(
  new URL("../src/features/settings/app/ProfilesSection.tsx", import.meta.url),
  "utf8",
);
const adapter = readFileSync(
  new URL(
    "../src/features/settings/app/CommandPolicyDetails.tsx",
    import.meta.url,
  ),
  "utf8",
);
const api = readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8");
const domain = readFileSync(
  new URL("../src/types/domain.ts", import.meta.url),
  "utf8",
);

test("agent access profiles use the shared v1 contract and explicit actions", () => {
  for (const value of [
    "tusker.agent-access/v1",
    "work_in_projects",
    "review_only",
    "No private folders added",
    "Add reference",
    "Check setup",
    "Run test",
    "Save profile",
    "Access",
    "Current mode",
    "Protected folders",
    "Block without prompting",
    "Ask before running",
    "Use recommended defaults",
    "Permission preset",
  ])
    expect(profiles).toContain(value);
  expect(adapter).toContain("Agent adapter");
  expect(profiles).toContain('destructive_actions: "deny"');
  expect(profiles).toContain("api.modelPrivateFoldersSet");
  expect(profiles).toContain("api.runnerConformance");
  expect(profiles).toContain("never discard authored access");
  expect(profiles).toContain("Settings supported");
  expect(profiles).toContain("find((item) => !item.hidden && item.default)");
  expect(profiles).not.toContain("find((item) => !item.hidden) ||");
});

test("access mode explicitly switches between project and full access", () => {
  expect(profiles).toContain("access || accessFromPreset(draft.preset)");
  expect(profiles).toContain("access ? access.mode : presetMode(draft.preset)");
  expect(profiles).toContain(
    "Choose an agent, add protected folders, and save.",
  );
  expect(profiles).toContain('<option value="full_access">Full access</option>');
  expect(profiles).toContain('preset: "danger-full-access"');
  expect(profiles).toContain('aria-label="Reasoning"');
  for (const value of [
    "stage the new controls",
    "Existing permissions",
    "legacy profile",
  ]) {
    expect(profiles).not.toContain(value);
  }
});

test("profile saves and reports carry access DTOs", () => {
  expect(api).toContain("access?: AgentAccessV1");
  expect(api).toContain('action: "private-folders"');
  expect(domain).toContain("export interface AgentAccessV1");
  expect(domain).toContain("export interface ResolvedAccess");
  expect(domain).toContain("access?: ResolvedAccess");
  expect(api).toContain("setup = false");
  expect(api).toContain("access: draft.access");
});
