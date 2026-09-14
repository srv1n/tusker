import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = (path: string) => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("project rail shell exposes the navigation surfaces", () => {
  const strip = source("src/features/workbench/navigation/ProjectStrip.tsx");
  const root = source("src/routes/__root.tsx");
  const knowledge = source("src/features/knowledge/KnowledgeTree.tsx");
  const knowledgeShell = source("src/features/knowledge/KnowledgeShell.tsx");
  const css = source("src/features/workbench/navigation/ProjectStrip.css");

  expect(root).toContain("<ProjectStrip expanded={rails.projectExpanded}");
  expect(root).toContain('RAIL_LAYOUT_STORAGE_KEY = "tusker.rails.layout.v1"');
  expect(root).toContain('className="tusker-shell');
  expect(root).not.toContain("<Sidebar ");
  expect(strip).toContain('data-project-strip');
  expect(strip).toContain('"project-rail relative');
  expect(root).not.toContain('aria-label="Project sections"');
  expect(root).not.toContain('"section-rail relative flex flex-none flex-col bg-surface');
  expect(root).not.toContain('sectionExpanded: !value.sectionExpanded');
  expect(strip).toContain('"project-rail relative flex h-full flex-none flex-col');
  expect(strip).toContain('border-r border-line-soft bg-raised');
  expect(strip).toContain("<ProjectSubtree expanded={expanded}");
  expect(strip).toContain('expanded ? "w-52" : "w-14"');
  expect(strip).toContain('selected && "items-center rounded-xl bg-panel p-1 ring-1 ring-inset ring-line"');
  expect(strip).toContain('selected ? "bg-raised text-ink shadow-sm"');
  expect(strip).toContain('active ? "bg-info-soft text-info"');
  expect(strip).toContain('aria-label={expanded ? "Minimize project navigation" : "Expand project navigation"}');
  expect(strip).toContain('"project-strip-track flex min-h-0 flex-1 flex-col gap-2');
  expect(knowledgeShell).toContain('w-[280px]');
  expect(knowledge).toContain('const INDENT_STEP = 16');
  expect(knowledge).toContain('"relative flex h-8 items-center rounded-lg');
  expect(root).not.toContain("data-app-topbar");
  expect(strip).not.toContain('aria-label="Tusker home"');
  expect(strip).toContain("Search tasks");
  expect(strip).toContain('Notifications, ${count} items need you');
  expect(strip).toContain("App settings");
  expect(strip).toContain('aria-label="App actions"');
  for (const label of ["Waves", "Board", "Docs", "Settings"]) expect(strip).toContain(label);
  expect(strip).toContain('aria-label="More project destinations"');
  expect(strip).toContain("toggleProjectPinned");
  expect(strip).toContain("movePinnedProject");
  expect(strip).toContain("overflow-y-auto");
  expect(strip).not.toContain("Scroll projects left");
  expect(strip).not.toContain("Scroll projects right");
  expect(strip).toContain("PanelLeftOpen");
  expect(strip).toContain("PanelLeftClose");
  expect(css).toContain("project-rail");
});

test("secondary actions keep registration, refresh and project destinations reachable", () => {
  const strip = source("src/features/workbench/navigation/ProjectStrip.tsx");
  const sidebar = source("src/components/Sidebar.tsx");

  for (const label of ["Add project", "Refresh projects", "Refresh project"]) expect(strip).toContain(label);
  for (const label of ["Trains", "Diagnostics"]) expect(strip).toContain(label);
  expect(strip).toContain("<AddProjectForm");
  expect(sidebar).toContain("Registers only. Daemon automation stays off.");
});

test("app actions and notifications cannot remain open together", () => {
  const strip = source("src/features/workbench/navigation/ProjectStrip.tsx");

  expect(strip).toContain('open={menuProjectId === "notifications"}');
  expect(strip).toContain('onOpenChange={(open) => setMenuProjectId(open ? "notifications" : null)}');
  expect(strip).toContain('setMenuProjectId((open) => open === "app" ? null : "app")}');
  expect(strip).toContain('to="/settings" aria-label="App settings"');
  expect(strip).toContain('className="project-notification-control fixed right-4 top-4');
});
