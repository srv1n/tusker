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
  const controls = source("src/components/ui/controls.tsx");

  expect(settings).toContain("<Toggle");
  expect(settings).toContain("project.automationEnabled");
  expect(settings).toContain("ariaLabel={`Background work");
  expect(settings).toContain("automationPending.current");
  expect(settings).toContain("Registration alone never enables it.");
  expect(settings).toContain("<ActionResultLine pending={automation.isPending}");
  expect(settings).not.toContain("<ActionResultLine pending={automation.isPending} error={automation.error} result={automation.data} />\n          <Button variant=\"primary\"");
  expect(controls).toContain('role="switch"');
  expect(controls).toContain("aria-label={ariaLabel}");
  expect(controls).toContain("aria-checked={checked}");
  expect(controls).toContain("focus-visible:ring-2");
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
  const repair = source("src/features/product/ProjectRegistrationRepair.tsx");
  const api = source("src/lib/api.ts");
  const queries = source("src/lib/queries.ts");
  const sidebar = source("src/components/Sidebar.tsx");

  expect(settings).toContain('project.health === "error"');
  expect(settings).toContain("Open registration repair");
  expect(settings).toContain("onOpenAdvanced={() => setTab(\"advanced\")}");
  expect(settings).toContain("<ProjectRegistrationRepair project={project} needsAttention={needsRegistrationRepair} />");
  expect(repair).toContain("Rebinding requires background work to be off.");
  expect(repair).toContain("project.automationEnabled || rebind.isPending");
  expect(repair).toContain('aria-label="Repair repository path"');
  expect(repair).toContain('aria-label="Repair vault path"');
  expect(repair).toContain('aria-label="Browse repository folder"');
  expect(repair).toContain('aria-label="Browse vault folder"');
  expect(repair).toContain('replace(/\\/+$/, "")');
  expect(repair).toContain("setVaultRoot(repositoryVault(path))");
  expect(repair).toContain("Use repository/.tusker");
  expect(repair).toContain('type="checkbox" checked={allowDirty}');
  expect(repair).toContain('typeToConfirm: "ALLOW DIRTY"');
  expect(repair).toContain('title: "Rebind project registration?"');
  expect(repair).toContain("Check repair");
  expect(repair).toContain("Apply repair");
  expect(repair).toContain('dryRun: true');
  expect(repair).toContain("setPreviewSelection(null)");
  expect(repair).toContain("retained_queued_count");
  expect(settings).not.toMatch(/<Button[^>]*(Reset|Retire)/i);
  expect(api).toContain("/rebind");
  expect(api).toContain("allowDirty?: boolean");
  expect(api).toContain("confirm?: string");
  expect(api).toContain("dryRun?: boolean");
  expect(queries).toContain("useProjectRebind");
  expect(queries).toContain("query.queryKey.some((part) => part === projectId)");
  expect(sidebar).not.toContain("Repair in Settings");
  expect(sidebar).not.toContain("Troubleshooting in Settings");
});
