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

test("project registration offers native folder browsing without pretending browsers reveal paths", () => {
  const sidebar = source("src/components/Sidebar.tsx");
  const panel = source("src/features/panel/Panel.tsx");

  expect(sidebar).toContain('aria-label="Browse repository folder"');
  expect(sidebar).toContain('aria-label="Browse vault folder"');
  expect(sidebar).toContain("const path = await pickFolder()");
  expect(sidebar).toContain("if (path) setValue(path);");
  expect(sidebar).toContain("enter the absolute path manually");
  expect(panel).toContain("pickFolder?: () => Promise<string | undefined>");
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
