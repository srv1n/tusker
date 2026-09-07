import { describe, expect, test } from "bun:test";
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
});
