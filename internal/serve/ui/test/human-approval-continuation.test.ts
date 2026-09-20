import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HumanActionCard } from "../src/features/human-action/HumanActionCard";
import { qk } from "../src/lib/queries";
import { WaveAuthorityControls } from "../src/features/workbench/integration/WaveAuthority";
import { ConfirmProvider } from "../src/components/ui/action-feedback";
import type { HumanAction, WaveReview } from "../src/types/domain";

const action: HumanAction = {
  kind: "decision", rawKind: "decision", title: "CLI pilot authorization",
  action: "Authorize the CLI pilot.", whyAgentCannot: "Human authority is required.",
  completionCondition: "Authorization is recorded.", gateId: "APP-G-0001",
  materialRevision: "sha256:gate-material",
  blockedTaskIds: ["APP-T-0001"], covers: ["A1"], acceptance: [],
};

describe("human approval continuation", () => {
  test("authorized wave shows each exact action as approve and continue", () => {
    const review: WaveReview = {
      schema: "tusker.wave-review/v1", waveId: "W-0001", title: "Pilot", outcome: "Run pilot",
      state: "Waiting", authorization: "authorized", materialFingerprint: "sha256:material",
      members: [{ taskId: "APP-T-0001", title: "Pilot task", state: "waiting" }], frontiers: [["APP-T-0001"]],
      humanActions: [{ taskId: "APP-T-0001", taskTitle: "Pilot task", action }],
      blockers: [{ code: "HUMAN_GATE_OPEN", taskId: "APP-T-0001", gateId: action.gateId, reason: "open human gate APP-G-0001 blocks APP-T-0001", action: "open APP-T-0001 in the Tusker Mac app and confirm APP-G-0001" }],
      controls: [{ action: "wave start", enabled: true, scope: "W-0001" }],
    };
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(qk.waveReview("app", "W-0001"), review);
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(WaveAuthorityControls, { projectId: "app", waveId: "W-0001" }))));
    expect(html).toContain("Waiting for you");
    expect(html).toContain("A specific decision is needed before this work can continue.");
    expect(html).toContain("Authorize the CLI pilot.");
    expect(html).toContain("Approve and continue");
    expect(html).toContain("Review scope and limits");
    expect(html).not.toContain("Review your action");
    expect(html).not.toContain("Current status");
    expect(html).not.toContain('data-wave-control="wave start"');
    expect(html).not.toContain("Mark complete");
  });

  test("standalone action never claims continuation", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(HumanActionCard, { action, taskId: "APP-T-0001", taskTitle: "Pilot task", projectId: "app" }))));
    expect(html).toContain("Confirm action");
    expect(html).not.toContain("Approve and continue");
  });

  test("an approval remains primary while review proves another member can start", () => {
    const review: WaveReview = {
      schema: "tusker.wave-review/v1", waveId: "W-0001", title: "Pilot", outcome: "Run pilot",
      state: "Waiting", authorization: "authorized", materialFingerprint: "sha256:material",
      members: [
        { taskId: "APP-T-0001", title: "Pilot task", state: "waiting" },
        { taskId: "APP-T-0002", title: "Independent task", state: "ready" },
      ], frontiers: [["APP-T-0001", "APP-T-0002"]],
      humanActions: [{ taskId: "APP-T-0001", taskTitle: "Pilot task", action }],
      blockers: [{ code: "HUMAN_GATE_OPEN", taskId: "APP-T-0001", gateId: action.gateId, reason: "open human gate APP-G-0001 blocks APP-T-0001", action: "open APP-T-0001 in the Tusker Mac app and confirm APP-G-0001" }],
      controls: [
        { action: "wave start", enabled: true, scope: "W-0001" },
        { action: "task start", enabled: true, scope: "APP-T-0002" },
      ],
    };
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(qk.waveReview("app", "W-0001"), review);
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ConfirmProvider, null, createElement(WaveAuthorityControls, { projectId: "app", waveId: "W-0001" }))));
    expect(html).toContain("Approve and continue");
    expect(html).toContain('data-wave-control="wave start"');
    expect(html.indexOf("Approve and continue")).toBeLessThan(html.indexOf('data-wave-control="wave start"'));
    expect(html).toContain("other eligible tasks can still start");
  });
});
