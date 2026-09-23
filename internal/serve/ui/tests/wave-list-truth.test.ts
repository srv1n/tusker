import { expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { WaveList } from "../src/features/workbench/overview/WaveList";
import type { WaveListItem } from "../src/types/domain";

const wave = (id: string, overrides: Partial<WaveListItem> = {}): WaveListItem => ({
  id, title: id, status: "open", authorization: "armed", memberCount: 1, doneCount: 0, ...overrides,
});

test("wave list groups only live work as running", () => {
  const html = renderToStaticMarkup(createElement(WaveList, {
    waves: [
      wave("W-LIVE", { liveRun: true }),
      wave("W-QUEUED", { recovery: { authorization: "armed", queued: true, capabilities: { safeRepair: false }, causeCode: "queued" } }),
      wave("W-REVIEW", { reviewWait: true }),
      wave("W-DONE", { memberCount: 1, doneCount: 1 }),
      wave("W-STALE", { authorization: "stale" }),
    ],
    query: "", onOpenWave: () => {}, loading: false,
  }));
  const group = (label: string) => html.match(new RegExp(`<section[^>]*aria-label="${label}"[\\s\\S]*?<\\/section>`))?.[0] ?? "";
  expect(group("Running")).toContain("W-LIVE");
  expect(group("Running")).not.toContain("W-QUEUED");
  expect(group("Running")).not.toContain("W-REVIEW");
  expect(group("Review wait")).toContain("W-REVIEW");
  expect(group("Up next")).toContain("W-QUEUED");
  expect(group("Done")).toContain("W-DONE");
  expect(group("Needs you")).toContain("W-STALE");
});
