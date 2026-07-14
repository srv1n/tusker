import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = (path: string) => readFileSync(path, "utf8");

test("project registration is available in the sidebar and defaults automation off", () => {
  const sidebar = source("src/components/Sidebar.tsx");
  const api = source("src/lib/api.ts");

  expect(sidebar).toContain('aria-label={addingProject ? "Close add project form" : "Add project"}');
  expect(sidebar).toContain("data-add-project-form");
  expect(sidebar).toContain("Registers only. Daemon automation stays off.");
  expect(sidebar).toContain("register.mutateAsync");
  expect(api).toContain('post("/projects", body)');
});

test("project settings own the explicit daemon automation choice", () => {
  const settings = source("src/features/settings/ProjectSettings.tsx");
  const api = source("src/lib/api.ts");

  expect(settings).toContain("data-project-automation");
  expect(settings).toContain('role="switch"');
  expect(settings).toContain("project.automationEnabled");
  expect(settings).toContain("Registration alone never enables it.");
  expect(api).toContain("post(`/projects/${projectId}/automation`, { enabled })");
});

test("production settings project live execution from run APIs, never static runtime fixtures", () => {
  const settings = source("src/features/settings/ProjectSettings.tsx");
  const parts = source("src/features/settings/project/parts.tsx");
  expect(settings).toContain("useRuns(projectId)");
  expect(settings).toContain("r.workspacePath");
  expect(settings).toContain("r.workspaceMode");
  expect(settings).not.toMatch(/import\s*\{[^}]*\bworktrees\b[^}]*\}\s*from/s);
  expect(settings).not.toContain("AGX-T-0003");
  expect(settings).not.toContain("CLN-T-0007");
  expect(parts).toContain("No active execution workspaces.");
});

test("workspace mode and concurrency persist through project settings API", () => {
  const settings = source("src/features/settings/ProjectSettings.tsx");
  const api = source("src/lib/api.ts");
  expect(settings).toContain('aria-label="Workspace mode"');
  expect(settings).toContain('aria-label="Project concurrency"');
  expect(api).toContain("post(`/projects/${projectId}/settings`, body)");
});
