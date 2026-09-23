import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { runnerStatus } from "../src/features/workbench/navigation/runnerStatus";
import type { DaemonStatus, ProjectSummary } from "../src/types/domain";

const source = (path: string) => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("project rail is search, flat projects, three sections, status and settings", () => {
  const strip = source("src/features/workbench/navigation/ProjectStrip.tsx");
  const root = source("src/routes/__root.tsx");

  expect(root).toContain("<ProjectStrip expanded={rails.projectExpanded}");
  expect(root).toContain('RAIL_LAYOUT_STORAGE_KEY = "tusker.rails.layout.v1"');
  expect(root).toContain('event.key === "\\\\"');
  expect(root).not.toContain("InvariantCircuitBanner");
  expect(root).toContain("{embedded && <CrashLoopCircuitBannerFromDaemon />}");

  expect(strip).toContain('expanded ? "w-[220px]" : "w-14"');
  expect(strip).toContain("openTaskSearch()");
  for (const label of ['label: "Inbox"', 'label: "Work"', 'label: "Docs"']) expect(strip).toContain(label);
  expect(strip).toContain('aria-current={active ? "page" : undefined}');
  expect(strip).toContain('to="/p/$projectId/diagnostics"');
  expect(strip).toContain('aria-label="App settings"');
  for (const label of ["Add project", "Refresh projects", "Refresh project", "Project settings", "<AddProjectForm"]) expect(strip).toContain(label);
  expect(strip).toContain("toggleProjectPinned");
  expect(strip).toContain("movePinnedProject");
  // Removed chrome.
  for (const gone of ["NotificationControl", "More project destinations", "App actions", "Trains", "Minimize</", "recentProjectIds"]) expect(strip).not.toContain(gone);
});

test("runner status row surfaces an open circuit and never hides it", () => {
  const daemon = { connected: true, addr: "", activeRuns: 2, queuedTasks: 0, daemonAlive: true } as DaemonStatus;
  const project = { automationEnabled: true } as ProjectSummary;
  expect(runnerStatus(daemon, project)).toEqual({ label: "2 running", tone: "pass" });
  expect(runnerStatus({ ...daemon, activeRuns: 0 }, project).label).toBe("Runner idle");
  expect(runnerStatus(daemon, { ...project, automationEnabled: false }).label).toBe("Auto-run off");
  expect(runnerStatus({ ...daemon, daemonAlive: false, daemonDownReason: "no pid" }, project)).toEqual({ label: "Runner offline", tone: "warn", reason: "no pid" });
  const crash = runnerStatus({ ...daemon, crashLoop: { open: true, summary: "six abnormal starts" } }, { ...project, automationEnabled: false });
  expect(crash.tone).toBe("fail");
  expect(crash.label).toBe("Auto-run stopped");
  expect(crash.reason).toContain("six abnormal starts");
  expect(runnerStatus({ ...daemon, invariantCircuit: { open: true, violations: [{ check: "x", detail: "lease drift" }] } }).reason).toContain("lease drift");
  expect(runnerStatus(undefined).label).toBe("Runner unknown");
});
