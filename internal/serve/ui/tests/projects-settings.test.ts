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
  expect(sidebar).toContain("useProjectRefresh");
  expect(sidebar).toContain("aria-label={`Refresh ${project.name}`}");
  expect(sidebar).toContain("refresh.mutate()");
  expect(sidebar).toContain("Refresh failed — check this project’s source.");
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
  const settings = source("src/features/product/OperationsScreens.tsx");
  const api = source("src/lib/api.ts");

  expect(settings).toContain("<Toggle");
  expect(settings).toContain("project.automationEnabled");
  expect(settings).toContain("Registration alone never enables it.");
  expect(api).toContain("post(`/projects/${projectId}/automation`, { enabled })");
});

test("production diagnostics project live execution from run APIs, never static runtime fixtures", () => {
  const settings = source("src/features/product/OperationsScreens.tsx");
  expect(settings).toContain("useRuns(projectId)");
  expect(settings).toContain("run.workspacePath");
  expect(settings).toContain("run.workspaceMode");
  expect(settings).not.toMatch(/import\s*\{[^}]*\bworktrees\b[^}]*\}\s*from/s);
  expect(settings).not.toContain("AGX-T-0003");
  expect(settings).not.toContain("CLN-T-0007");
  expect(settings).toContain("No active workspace leases.");
});

test("workspace mode and concurrency persist through project settings API", () => {
  const settings = source("src/features/product/OperationsScreens.tsx");
  const api = source("src/lib/api.ts");
  expect(settings).toContain('aria-label="Workspace mode"');
  expect(settings).toContain('aria-label="Project concurrent tasks"');
  expect(api).toContain("post(`/projects/${projectId}/settings`, body)");
});

test("advanced settings expose bounded registration repair without reset controls", () => {
  const settings = source("src/features/product/OperationsScreens.tsx");
  const api = source("src/lib/api.ts");
  const queries = source("src/lib/queries.ts");
  const sidebar = source("src/components/Sidebar.tsx");

  expect(settings).toContain('project.health === "error"');
  expect(settings).toContain("Open registration repair");
  expect(settings).toContain("onOpenAdvanced={() => setTab(\"advanced\")}");
  expect(settings).toContain("<ProjectRegistrationRepair project={project} needsAttention={needsRegistrationRepair} />");
  expect(settings).toContain("Rebinding requires background work to be off.");
  expect(settings).toContain("project.automationEnabled || rebind.isPending");
  expect(settings).toContain('aria-label="Repair repository path"');
  expect(settings).toContain('aria-label="Repair vault path"');
  expect(settings).toContain('aria-label="Browse repository folder"');
  expect(settings).toContain('aria-label="Browse vault folder"');
  expect(settings).toContain('replace(/\\/+$/, "")');
  expect(settings).toContain("setVaultRoot(repositoryVault(path))");
  expect(settings).toContain("Use repository/.tusker");
  expect(settings).toContain('type="checkbox" checked={allowDirty}');
  expect(settings).toContain('typeToConfirm: "ALLOW DIRTY"');
  expect(settings).toContain('title: "Rebind project registration?"');
  expect(settings).toContain("Check repair");
  expect(settings).toContain("Apply repair");
  expect(settings).toContain('dryRun: true');
  expect(settings).toContain("setPreviewSelection(null)");
  expect(settings).toContain("retained_queued_count");
  expect(settings).not.toMatch(/<Button[^>]*(Reset|Retire)/i);
  expect(api).toContain("/rebind");
  expect(api).toContain("allowDirty?: boolean");
  expect(api).toContain("confirm?: string");
  expect(api).toContain("dryRun?: boolean");
  expect(queries).toContain("useProjectRebind");
  expect(queries).toContain("query.queryKey.some((part) => part === projectId)");
  expect(sidebar).toContain('to="/p/$projectId/settings"');
  expect(sidebar).toContain("Repair in Settings");
  expect(sidebar).toContain("project.health === \"error\"");
});
