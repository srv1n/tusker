import { expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { WaveList } from "../src/features/workbench/overview/WaveList";
import { sampleState } from "../src/features/workbench/overview/previewFixtures";
import type { WaveListItem } from "../src/types/domain";

const wave = (id: string, overrides: Partial<WaveListItem> = {}): WaveListItem => ({
  id, title: id, status: "open", state: sampleState("planned"), authorization: "armed", memberCount: 1, doneCount: 0, ...overrides,
});

test("wave list groups waves by their server state only", () => {
  const html = renderToStaticMarkup(createElement(WaveList, {
    waves: [
      wave("W-LIVE", { state: sampleState("working", "1 working"), liveRun: true }),
      wave("W-QUEUED", { state: sampleState("planned", "queued") }),
      wave("W-REVIEW", { state: sampleState("in_review", "reviewing"), liveRun: true }),
      wave("W-DONE", { state: sampleState("done"), memberCount: 1, doneCount: 1 }),
      wave("W-STALE", { state: sampleState("blocked", "1 blocked") }),
    ],
    query: "", onOpenWave: () => {}, loading: false,
  }));
  const group = (label: string) => html.match(new RegExp(`<section[^>]*aria-label="${label}"[\\s\\S]*?<\\/section>`))?.[0] ?? "";
  expect(group("Working")).toContain("W-LIVE");
  expect(group("Working")).not.toContain("W-QUEUED");
  expect(group("Working")).not.toContain("W-REVIEW");
  expect(group("In review")).toContain("W-REVIEW");
  expect(group("Planned")).toContain("W-QUEUED");
  expect(group("Done")).toContain("W-DONE");
  expect(group("Blocked")).toContain("W-STALE");
  expect(group("Blocked")).toContain("1 blocked");
  expect(html.indexOf('aria-label="Blocked"')).toBeLessThan(html.indexOf('aria-label="Working"'));
});
