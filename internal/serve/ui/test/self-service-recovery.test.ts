import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient } from "@tanstack/react-query";
import { WaveList } from "../src/features/workbench/overview/WaveList";
import { WaveOverview } from "../src/features/workbench/overview/WaveOverview";
import { groupWaves, type Startability } from "../src/features/workbench/overview/groupWaves";
import { AutomationScopeRow } from "../src/features/product/OperationsScreens";
import { invalidateAutomationToggleQueries, qk } from "../src/lib/queries";
import { makeRun, makeTask, makeWave } from "../src/features/workbench/overview/previewFixtures";
import type { RecoveryDiagnosis, WaveListItem, WaveSummary } from "../src/types/domain";

function recovery(overrides: Partial<RecoveryDiagnosis> & { blockingCause?: string; causeCode?: string }): RecoveryDiagnosis {
  return {
    authorization: "armed",
    queued: false,
    capabilities: { safeRepair: false, safeRepairReason: "bounded safe repair is daemon-applied under existing authority; the server exposes no manual repair mutation" },
    ...overrides,
  };
}

const START: Record<string, Startability> = {
  "W-ARMED-QUEUED": { state: "blocked", reason: "Queued." },
  "W-OFF": { state: "blocked", reason: "Background work is off." },
  "W-STALE": { state: "unknown", reason: "Stale read." },
  "W-CAP": { state: "blocked", reason: "Capacity." },
};

function listItem(id: string, authorization: string, rec?: RecoveryDiagnosis): WaveListItem {
  return { id, title: id, status: "open", authorization, memberCount: 1, doneCount: 0, recovery: rec };
}

function renderList(waves: WaveListItem[], backgroundWorkEnabled?: boolean) {
  return renderToStaticMarkup(createElement(WaveList, {
    waves, projectName: "demo", backgroundWorkEnabled, query: "", onQueryChange: () => {}, onOpenWave: () => {}, loading: false,
  }));
}

describe("self-service recovery rendering", () => {
  test("A1: armed+queued row shows the actual blocking cause", () => {
    const html = renderList([
      listItem("W-ARMED-QUEUED", "armed", recovery({ queued: true, blockingCause: "Queued. Next check 2026-09-22T00:01:00Z.", causeCode: "queued", nextActor: "daemon" })),
    ]);
    expect(html).toContain("Armed + Queued");
    expect(html).toContain("Queued. Next check");
    expect(html).not.toContain("Running");
  });

  test("A1: project-off, daemon-unavailable, and capacity waits stay distinct", () => {
    const html = renderList([
      listItem("W-OFF", "armed", recovery({ blockingCause: "Background work is off for this project.", causeCode: "project_disabled", nextActor: "operator" }), ),
      listItem("W-STALE-D", "armed", recovery({ blockingCause: "Work waits on daemon reconciliation but the last recorded poll was 2026-09-21T00:00:00Z.", causeCode: "doctor-daemon-stale", nextActor: "operator" })),
      listItem("W-CAP", "armed", recovery({ blockingCause: "1 active run(s) hold execution capacity: T-9.", causeCode: "doctor-project-capacity", nextActor: "daemon" })),
    ], true);
    expect(html).toContain("Background work is off for this project.");
    expect(html).toContain("last recorded poll was");
    expect(html).toContain("hold execution capacity: T-9.");
  });

  test("A1: a queued directive never earns Running", () => {
    const waves: WaveSummary[] = [makeWave({ id: "W-Q", title: "Queued wave", memberIds: ["T-Q"] })];
    const tasks = [makeTask({ id: "T-Q", title: "Queued task", status: "ready" })];
    // Unclaimed run: a queued reservation, not an execution.
    const runs = [makeRun({ taskId: "T-Q", leaseState: "unclaimed", leaseStateRaw: "unclaimed", outcome: "queued", liveness: "fresh" })];
    const grouped = groupWaves({ waves, tasks, runs, startability: { "W-Q": { state: "blocked", reason: "Queued." } } });
    const running = grouped.groups.find((group) => group.id === "running")!;
    expect(running.waves.map((entry) => entry.wave.id)).not.toContain("W-Q");
    const overview = renderToStaticMarkup(createElement(WaveOverview, {
      waves, tasks, runs, startability: { "W-Q": { state: "blocked", reason: "Queued." } },
      query: "", category: "all", onQueryChange: () => {}, onCategoryChange: () => {}, onOpenWave: () => {}, onOpenUnassigned: () => {},
    }));
    expect(overview).not.toContain(">Running<");
  });

  test("A2: disabled repair capability renders a reason, never a mutation", () => {
    const rec = recovery({ causeCode: "reconcile-overdue", blockingCause: "Scheduled reconciliation is overdue since 2026-09-22T00:00:00Z." });
    expect(rec.capabilities.safeRepair).toBe(false);
    expect(rec.capabilities.safeRepairReason ?? "").toContain("no manual repair mutation");
    // The overview renders the cause text but no repair control: the only
    // buttons on a card are navigation and filter controls.
    const html = renderList([listItem("W-ESC", "armed", rec)], true);
    expect(html).toContain("Scheduled reconciliation is overdue");
    expect(html.match(/<button/g)?.length ?? 0).toBeGreaterThan(0);
    expect(html).not.toContain("Repair");
    expect(html).not.toContain("repair");
  });

  test("A2: refused higher-impact actions keep explicit labels", () => {
    const html = renderList([listItem("W-PAUSED", "paused", recovery({ authorization: "paused", blockingCause: "Wave is paused.", causeCode: "paused", nextActor: "operator", nextAction: "tusker wave resume W-PAUSED" }))], true);
    expect(html).toContain("Paused on record");
    expect(html).toContain("Wave is paused.");
  });

  test("A3: toggle settlement refreshes task/wave/run/attempt/proof/needs caches", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const projectId = "project-cache";
    const seeds: Array<[readonly unknown[], unknown]> = [
      [qk.tasks(projectId), []],
      [qk.task("T-1", projectId), null],
      [qk.waves(projectId), []],
      [qk.waveList(projectId), []],
      [qk.wave(projectId, "W-1"), null],
      [qk.waveReview(projectId, "W-1"), null],
      [qk.runs(projectId), []],
      [qk.run("T-1", projectId), null],
      [qk.attempts(undefined, projectId), []],
      [qk.evidence(undefined, projectId), []],
      [qk.needs(projectId), []],
      [qk.projectAutomationScope(projectId), null],
      [qk.projects, []],
      [qk.daemon, null],
    ];
    for (const [key, value] of seeds) client.setQueryData(key as never, value);
    await invalidateAutomationToggleQueries(client, projectId);
    const stale = seeds.filter(([key]) => !client.getQueryState(key as never)?.isInvalidated).map(([key]) => JSON.stringify(key));
    expect(stale).toEqual([]);
  });

  test("A4: background settings expose scope and audit without enabling", () => {
    const scopeData = {
      ok: true,
      projectId: "project-scope",
      enabled: false,
      automation: {
        beforeEnabled: false,
        afterEnabled: false,
        actor: "op-tests",
        source: "api",
        scope: {
          project_id: "project-scope",
          waves: [{ wave_id: "W-SC", title: "Scoped", members: ["SC-T-1"], frontier: ["SC-T-1"], eligible_count: 1, total_count: 1, project_id: "project-scope" }],
          directives: [{ record_id: "SC-T-1", wave_id: "W-SC", actor: "human:test", reason: "armed" }],
          excluded: [{ wave_id: "W-OLD", reason: "wave authorization is disarmed" }],
        },
        audit: undefined,
        auditTrail: [{ event_id: "automation-1", project_id: "project-scope", actor: "op-tests", source: "api", before_enabled: false, after_enabled: false, created_at: "2026-09-22T00:00:00Z" }],
      },
    };
    const html = renderToStaticMarkup(
      createElement(AutomationScopeRow, {
        scopeQuery: { data: scopeData, isPending: false, error: null } as never,
      }),
    );
    expect(html).toContain("W-SC");
    expect(html).toContain("SC-T-1");
    expect(html).toContain("Excluded W-OLD: wave authorization is disarmed");
    expect(html).toContain("op-tests");
    // Read-only: no toggle, checkbox, or enable control in the row.
    expect(html).not.toContain("checkbox");
    expect(html).not.toContain("Background work (");

    const emptyFrontier = structuredClone(scopeData);
    emptyFrontier.automation.scope.waves[0].frontier = null as never;
    const emptyHtml = renderToStaticMarkup(createElement(AutomationScopeRow, {
      scopeQuery: { data: emptyFrontier, isPending: false, error: null } as never,
    }));
    expect(emptyHtml).toContain("Armed W-SC: 1/1 eligible");
    expect(emptyHtml).not.toContain("(frontier:");
  });
});
