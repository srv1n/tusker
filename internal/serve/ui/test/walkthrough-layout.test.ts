import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { ProjectSummary, WaveReview } from "@/types/domain";
import { initialWaveView, settleEnteredWaveView } from "@/features/workbench/integration";
import { projectContextName } from "@/features/workbench/integration/WorkExperience";
import { canShowWaveResults, waveReviewStage, waveSummaryWithReview } from "@/features/workbench/integration/integrationModel";
import { WaveOverview } from "@/features/workbench/overview/WaveOverview";
import { makeWave } from "@/features/workbench/overview/previewFixtures";

describe("walkthrough layout", () => {
  test("entry waits for review, then keeps an explicit Tasks or Dependencies choice", () => {
    const open = makeWave({ id: "W-1", status: "open" });
    const completed = makeWave({ id: "W-1", status: "closed" });
    expect(initialWaveView(open)).toBe("flow");
    expect(initialWaveView(completed)).toBe("flow");
    expect(settleEnteredWaveView(null, completed, true)).toBeNull();
    expect(settleEnteredWaveView(null, completed, false)).toBe("flow");
    expect(settleEnteredWaveView("work", completed, false)).toBe("work");
    expect(settleEnteredWaveView("flow", completed, false)).toBe("flow");
  });

  test("overview exposes project context and counted status filters", () => {
    const html = renderToStaticMarkup(createElement(WaveOverview, {
      waves: [makeWave({ id: "W-1", title: "Alpha" })], tasks: [], runs: [],
      startability: { "W-1": { state: "ready" } }, projectName: "Example project",
      query: "", category: "all", onQueryChange: () => {}, onCategoryChange: () => {}, onOpenWave: () => {}, onOpenUnassigned: () => {},
    }));
    expect(html).toContain("Example project");
    expect(html).toContain('aria-label="Filter waves by status"');
    expect(html).toContain("All (1)");
    expect(html).toContain("Ready to start (1)");
  });

  test("stale authorization cannot unlock completed results", () => {
    const staleReview = { authorization: "stale", state: "Completed" } as WaveReview;
    const wave = makeWave({ id: "W-1", status: "closed" });
    expect(waveReviewStage(staleReview)).toBe("unknown");
    expect(waveSummaryWithReview(wave, staleReview)).toMatchObject({ status: "unknown", landedAt: null });
  });

  test("a cached completed review does not unlock Results after a refresh error", () => {
    const completed = { authorization: "authorized", state: "Completed" } as WaveReview;
    expect(canShowWaveResults(completed)).toBe(true);
    expect(canShowWaveResults(completed, new Error("review endpoint offline"))).toBe(false);
  });

  test("checkout routes retain their parent project context", () => {
    const parent = { id: "tusker", name: "Tusker", checkouts: [{ id: "checkout-alpha" }] } as ProjectSummary;
    expect(projectContextName([parent], "checkout-alpha")).toBe("Tusker");
  });
});
