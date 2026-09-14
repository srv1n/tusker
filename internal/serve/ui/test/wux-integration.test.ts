import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { WaveSummary } from "@/types/domain";
import { initialWaveView, restoredPath, waveStartability } from "@/features/workbench/integration";

const wave = (status: string, landedAt?: string): WaveSummary => ({
  id: "W-1", title: "Wave", status, landedAt, memberIds: [], members: [], counts: {},
  authorization: { state: "armed", stale: false, action: "execute" },
  brief: { schema: "tusker.wave-brief/v1", waveId: "W-1", title: "Wave", waveHref: "#", sectionOrder: ["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"], outcome: { summary: "", fullyDrained: false, counts: {}, tasks: [] }, seeIt: [], landed: [], reworkParked: [], humanAction: [], documentation: [] },
});

describe("work experience integration", () => {
  test("integration explicit deep link wins restore", () => expect(restoredPath("/p/a/waves", "/p/a/tasks/T-1")).toBe("/p/a/tasks/T-1"));
  test("integration completed entry shows results", () => { expect(initialWaveView(wave("closed"))).toBe("results"); expect(initialWaveView(wave("closed"), "flow")).toBe("flow"); });
  test("integration unavailable data never enables start", () => expect(waveStartability([wave("open")])["W-1"]).toEqual({ state: "unknown", reason: "Authoritative start readiness is not exposed by the current Serve API." }));
  test("routed wave exposes the supported Start action and execution status", () => {
    const source = readFileSync(new URL("../src/features/workbench/integration/WorkExperience.tsx", import.meta.url), "utf8");
    expect(source).toContain("WaveAuthorityControls");
    expect(source).not.toContain("useWaveExecute");
    expect(source).not.toContain("Wave Play");
    expect(source).not.toContain("execute.mutate");

    const flow = readFileSync(new URL("../src/features/workbench/flow/WaveFlow.tsx", import.meta.url), "utf8");
    expect(flow).toContain("Executing now:");
    expect(flow).toContain("Nothing is executing now.");
  });
});
