import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { NeedItem, ProjectSummary } from "../src/types/domain";
import {
  ALL_PROJECTS,
  ALL_PROJECTS_VALUE,
  PANEL_PROJECT_STORAGE_KEY,
  projectIdOf,
  projectOptionLabel,
  projectOverviewPath,
  projectSelection,
  projectSelectionFromValue,
  projectSelectionValue,
  resolveProjectSelection,
} from "../src/features/panel/projectSelection";
import { humanActionIdentity, partitionPanelNeeds, taskIdentity } from "../src/features/panel/panelModel";
import { api, withProject } from "../src/lib/api";

const panel = readFileSync(new URL("../src/features/panel/Panel.tsx", import.meta.url), "utf8");
const root = readFileSync(new URL("../src/routes/__root.tsx", import.meta.url), "utf8");
const queries = readFileSync(new URL("../src/lib/queries.ts", import.meta.url), "utf8");
const apiSource = readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8");

const project = (overrides: Partial<ProjectSummary> = {}): ProjectSummary => ({
  id: "tusker",
  name: "Tusker",
  repoRoot: "/tmp/tusker",
  vaultRoot: "/tmp/tusker/.tusker",
  automationEnabled: true,
  health: "healthy",
  needsCount: 0,
  activeRuns: 0,
  worstLiveness: null,
  daemonConnected: true,
  ...overrides,
});

test("panel row navigation prefers the optional shell bridge and has a router fallback", () => {
  expect(panel).toContain("window.tuskerShell?.openFull");
  expect(panel).toContain("navigate({ to: row.path as \"/\" })");
});

test("panel shell mode persists the query flag and recognizes the embedded user agent", () => {
  expect(root).toContain('get("shell") === "1"');
  expect(root).toContain('navigator.userAgent.includes("TuskerShell/")');
  expect(root).toContain('location.pathname === "/panel"');
});

test("panel registers the optional in-page deep-link hook for the native shell", () => {
  expect(panel).toContain("shell.onNavigate = onNavigate");
  expect(panel).toContain("navigate({ to: path as \"/\" })");
});

test("panel header opens the native desktop window with a browser fallback", () => {
  expect(panel).toContain('aria-label="Open the main Tusker window"');
  expect(panel).toContain("bridge(path)");
  expect(panel).toContain('navigate({ to: path as "/" })');
  expect(projectOverviewPath(ALL_PROJECTS)).toBe("/");
  expect(projectOverviewPath(projectSelection("project with spaces"))).toBe("/p/project%20with%20spaces");
});

test("panel switcher retains All projects and every registered project with status context", () => {
  expect(panel).toContain('aria-label="Project shown in Tusker triage"');
  expect(panel).toContain('<option value={ALL_PROJECTS_VALUE}>All projects</option>');
  expect(panel).toContain("projects.map((project)");
  expect(projectOptionLabel(project())).toBe("Tusker · 0 needs · 0 active · healthy");
  expect(projectOptionLabel(project({ needsCount: 1, activeRuns: 2, health: "error" })))
    .toBe("Tusker · 1 need · 2 active · ⚠ error");
});

test("persisted project selection validates against registrations and falls back safely", () => {
  const projects = [project(), project({ id: "other", name: "Other" })];
  expect(resolveProjectSelection(projectSelection("other"), projects)).toEqual(projectSelection("other"));
  expect(resolveProjectSelection(projectSelection("unregistered"), projects)).toBe(ALL_PROJECTS);
  expect(resolveProjectSelection(ALL_PROJECTS, projects)).toBe(ALL_PROJECTS);
  expect(panel).toContain(`window.localStorage.getItem(PANEL_PROJECT_STORAGE_KEY)`);
  expect(PANEL_PROJECT_STORAGE_KEY).toBe("tusker.panel.project");
});

test("All projects is tagged separately from a legal project id named all", () => {
  const literalAllProject = projectSelection("all");
  const stored = projectSelectionValue(literalAllProject);

  expect(projectSelectionValue(ALL_PROJECTS)).toBe(ALL_PROJECTS_VALUE);
  expect(stored).toBe("project:all");
  expect(stored).not.toBe(ALL_PROJECTS_VALUE);
  expect(projectSelectionFromValue(stored)).toEqual(literalAllProject);
  expect(projectIdOf(projectSelectionFromValue(stored))).toBe("all");
  expect(resolveProjectSelection(literalAllProject, [project({ id: "all" })])).toEqual(literalAllProject);
  expect(projectOverviewPath(literalAllProject)).toBe("/p/all");
});

test("aggregate need identity keeps every gate, including multiple gates on one task", () => {
  const action = (gateId: string) => ({ gateId }) as NonNullable<NeedItem["humanAction"]>;
  const needs = [
    { id: "need-gate-G-1", projectId: "alpha", taskId: "T-1", humanAction: action("G-1") } as NeedItem,
    { id: "need-gate-G-2", projectId: "alpha", taskId: "T-1", humanAction: action("G-2") } as NeedItem,
    { id: "need-gate-G-1", projectId: "beta", taskId: "T-1", humanAction: action("G-1") } as NeedItem,
    { projectId: "gamma", taskId: "T-1" } as NeedItem,
  ];
  const partition = partitionPanelNeeds(needs);

  expect(partition.humanActionRows.map((item) => item.humanAction?.gateId)).toEqual(["G-1", "G-2", "G-1"]);
  expect(partition.attentionNeeds.map((item) => item.projectId)).toEqual(["gamma"]);
  expect(taskIdentity("alpha", "T-1")).not.toBe(taskIdentity("beta", "T-1"));
  expect(humanActionIdentity(needs[0]!)).not.toBe(humanActionIdentity(needs[2]!));
  expect(panel).toContain("key={humanActionIdentity(item)}");
  expect(panel).toContain("key={`${title}-${row.key}`}");
});

test("switching scopes existing cached reads and withholds old rows while a new key loads", () => {
  expect(panel).toContain("useNeeds(selectedProjectId, readsEnabled)");
  expect(panel).toContain("useReviewBatch(selectedProjectId, readsEnabled)");
  expect(panel).toContain("useRuns(selectedProjectId, readsEnabled)");
  expect(panel).toContain("projectsQ.isPending || (readsEnabled && (needsQ.isPending || reviewQ.isPending || runsQ.isPending))");
  expect(queries).toContain("queryKey: qk.needs(projectId)");
  expect(queries).toContain("queryKey: qk.reviewBatch(projectId)");
  expect(queries).toContain("queryKey: qk.runs(projectId)");
});

test("project-scoped need and run URLs encode reserved query characters", async () => {
  const originalFetch = globalThis.fetch;
  const requested: string[] = [];
  globalThis.fetch = (async (input) => {
    requested.push(String(input));
    return new Response("[]", { status: 200, headers: { "content-type": "application/json" } });
  }) as typeof fetch;

  try {
    await api.needs("alpha&beta#ops");
    await api.runs("alpha&beta#ops");
  } finally {
    globalThis.fetch = originalFetch;
  }

  expect(withProject("/needs", "alpha&beta#ops")).toBe("/needs?project=alpha%26beta%23ops");
  expect(withProject("/runs?all=true", "alpha&beta#ops")).toBe("/runs?all=true&project=alpha%26beta%23ops");
  expect(requested).toEqual([
    "/api/needs?project=alpha%26beta%23ops",
    "/api/runs?project=alpha%26beta%23ops",
  ]);
  expect(apiSource).toContain('real(withProject("/needs", projectId))');
  expect(apiSource).toContain('real(withProject("/runs", projectId))');
  expect(apiSource).toContain('real(withProject("/review/batch", projectId))');
});
