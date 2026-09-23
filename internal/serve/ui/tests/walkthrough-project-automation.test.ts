import { expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toggle } from "../src/components/ui/controls";
import { beginAutomationToggle, SettingsBasic } from "../src/features/product/OperationsScreens";
import { applyProjectAutomationReadback } from "../src/lib/queries";
import type { ProjectSummary } from "../src/types/domain";

const projects: ProjectSummary[] = [{
  id: "primary", name: "App", repoRoot: "/app", vaultRoot: "/app/.tusker",
  automationEnabled: false, health: "healthy", needsCount: 0, activeRuns: 0,
  worstLiveness: null, daemonConnected: true,
  checkouts: [
    { id: "primary", label: "Primary", repoRoot: "/app", vaultRoot: "/app/.tusker", automationEnabled: false, git: true, detached: false, available: true, activity: "Idle", activeRuns: 0, health: "healthy" },
    { id: "child", label: "Child", repoRoot: "/app-child", vaultRoot: "/app-child/.tusker", automationEnabled: false, git: true, detached: false, available: true, activity: "Idle", activeRuns: 0, health: "healthy" },
  ],
}];

test("automation readback updates the exact checkout, including the primary", () => {
  const child = applyProjectAutomationReadback(projects, "child", true, "Project runtime")!;
  expect(child[0].automationEnabled).toBe(false);
  expect(child[0].checkouts?.[1].automationEnabled).toBe(true);

  const primary = applyProjectAutomationReadback(child, "primary", true, "Project runtime")!;
  expect(primary[0].automationEnabled).toBe(true);
  expect(primary[0].checkouts?.[0].automationEnabled).toBe(true);
});

test("settings block stale rapid toggles and keep automation feedback separate", () => {
  const pending = { current: false };
  expect(beginAutomationToggle(pending)).toBe(true);
  expect(beginAutomationToggle(pending)).toBe(false);
  pending.current = false;
  expect(beginAutomationToggle(pending)).toBe(true);

  const off = renderToStaticMarkup(createElement(Toggle, { checked: false, onChange: () => {}, ariaLabel: "Background work (Off)", label: "Off" }));
  const on = renderToStaticMarkup(createElement(Toggle, { checked: true, onChange: () => {}, ariaLabel: "Background work (On)", label: "On" }));
  for (const markup of [off, on]) expect(markup).toContain("role=\"switch\"");
  expect(off).toContain('aria-label="Background work (Off)"');
  expect(off).toContain('aria-checked="false"');
  expect(off).toContain("focus-visible:ring-2");
  expect(off).toContain("bg-line");
  expect(on).toContain('aria-checked="true"');
  expect(on).toContain("bg-ink");
  expect(on).toContain("translate-x-[14px]");

  // c5a5da52 added the automation scope query to SettingsBasic.
  const settings = renderToStaticMarkup(createElement(QueryClientProvider, { client: new QueryClient() }, createElement(SettingsBasic, {
    project: projects[0], projectIds: ["primary"], operations: undefined,
    automation: { isPending: false, error: null, data: { ok: true, reason: "Background work enabled" }, mutate: () => {} } as never,
    settings: { isPending: false, error: null, data: { ok: true, reason: "Execution settings saved" }, mutate: () => {} } as never,
    onOpenAdvanced: () => {},
  })));
  const automation = settings.indexOf("Background work enabled");
  const execution = settings.indexOf("Capacity &amp; workspace");
  expect(automation).toBeGreaterThan(settings.indexOf("Automation"));
  expect(automation).toBeLessThan(execution);
  expect(settings.indexOf("Execution settings saved")).toBeGreaterThan(execution);
});
