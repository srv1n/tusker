import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient } from "@tanstack/react-query";
import { WaveList } from "../src/features/workbench/overview/WaveList";
import { AutomationScopeRow } from "../src/features/product/OperationsScreens";
import { invalidateAutomationToggleQueries, qk } from "../src/lib/queries";
import { makeRun, makeTask, makeWave, sampleState } from "../src/features/workbench/overview/previewFixtures";
import type { RecoveryDiagnosis, TaskState, WaveListItem, WaveSummary } from "../src/types/domain";

function recovery(overrides: Partial<RecoveryDiagnosis> & { blockingCause?: string; causeCode?: string }): RecoveryDiagnosis {
  return {
    authorization: "armed",
    queued: false,
    capabilities: { safeRepair: false, safeRepairReason: "bounded safe repair is daemon-applied under existing authority; the server exposes no manual repair mutation" },
    ...overrides,
  };
}

// The list shows only the server-computed wave state; recovery causes arrive as its reason.
function listItem(id: string, authorization: string, state: TaskState, rec?: RecoveryDiagnosis): WaveListItem {
  return { id, title: id, status: "open", state, authorization, memberCount: 1, doneCount: 0, recovery: rec };
}

function renderList(waves: WaveListItem[]) {
  return renderToStaticMarkup(createElement(WaveList, {
    waves, query: "", onOpenWave: () => {}, loading: false,
  }));
}

describe("self-service recovery rendering", () => {
  test("A1: an armed+queued row waits under Planned with no alarm chip", () => {
    const html = renderList([
      listItem("W-ARMED-QUEUED", "armed", sampleState("planned", "queued"), recovery({ queued: true, causeCode: "queued", nextActor: "daemon" })),
    ]);
    expect(html).toContain("Planned · 1");
    expect(html).not.toContain("Blocked");
    expect(html).not.toContain("Working");
  });

  test("A1: project-off, daemon-unavailable, and capacity waits stay distinct", () => {
    const html = renderList([
      listItem("W-OFF", "armed", sampleState("blocked", "Background work is off for this project.")),
      listItem("W-STALE-D", "armed", sampleState("blocked", "Work waits on daemon reconciliation but the last recorded poll was 2026-09-21T00:00:00Z.")),
      listItem("W-CAP", "armed", sampleState("planned", "1 active run(s) hold execution capacity: T-9.")),
    ]);
    expect(html).toContain("Background work is off for this project.");
    expect(html).toContain("last recorded poll was");
    expect(html).toContain("hold execution capacity: T-9.");
  });

  test("A2: disabled repair capability renders a reason, never a mutation", () => {
    const rec = recovery({ causeCode: "reconcile-overdue", blockingCause: "Scheduled reconciliation is overdue since 2026-09-22T00:00:00Z." });
    expect(rec.capabilities.safeRepair).toBe(false);
    expect(rec.capabilities.safeRepairReason ?? "").toContain("no manual repair mutation");
    // The overview renders the cause text but no repair control: the only
    // buttons on a card are navigation and filter controls.
    const html = renderList([listItem("W-ESC", "armed", sampleState("blocked", "Scheduled reconciliation is overdue since 2026-09-22T00:00:00Z."), rec)]);
    expect(html).toContain("Scheduled reconciliation is overdue");
    expect(html.match(/<button/g)?.length ?? 0).toBeGreaterThan(0);
    expect(html).not.toContain("Repair");
    expect(html).not.toContain("repair");
  });

  test("A2: refused higher-impact actions keep explicit labels", () => {
    const html = renderList([listItem("W-PAUSED", "paused", sampleState("blocked", "Wave is paused.", { reason_code: "paused" }))]);
    expect(html).toContain(">Blocked<");
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
