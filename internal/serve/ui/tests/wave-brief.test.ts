import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { WaveBriefView } from "../src/features/ops/ProjectOps";
import type { WaveBrief } from "../src/types/domain";
import fixture from "./fixtures/wave-brief.json";

const source = readFileSync(new URL("../src/features/ops/ProjectOps.tsx", import.meta.url), "utf8");
const briefSource = source.slice(source.indexOf("export function WaveBriefView"), source.indexOf("function GatePanel"));

test("morning wave surface consumes the shared artifact-first contract", () => {
  const brief = fixture as WaveBrief;
  expect(brief.schema).toBe("tusker.wave-brief/v1");
  expect(brief.sectionOrder).toEqual(["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"]);
  expect(brief.seeIt[0]).toMatchObject({ kind: "screenshot", priority: 1, acceptanceIds: ["A1"], evidenceRef: "APP-T-0001-E-0001" });
  expect(readFileSync(new URL("./fixtures/wave-brief-screenshot.svg", import.meta.url), "utf8")).toContain("Artifact-first wave brief fixture");
  expect(briefSource).toContain(">Outcome<");
  expect(briefSource).toContain(">See it<");
  for (const section of ["Landed", "Rework/parked", "Human action", "Documentation"]) expect(briefSource).toContain(`title="${section}"`);
});

test("wave surface deep-links durable records and excludes orchestration exhaust", () => {
  const brief = structuredClone(fixture) as WaveBrief;
  brief.humanAction = [{ gateId: "APP-G-0001", gateHref: "/p/app/docs?path=APP-T-0002&gate=APP-G-0001", action: "Provision OAuth.", resumeId: "APP-G-0001", blockedTaskIds: ["APP-T-0002"] }];
  brief.documentation = [{ taskId: "APP-T-0001", taskHref: "/p/app/docs?path=APP-T-0001", node: "knowledge/domains/app/CANON.md", nodeHref: "/p/app/docs?path=knowledge%2Fdomains%2Fapp%2FCANON.md", state: "documented" }];
  const html = renderToStaticMarkup(createElement(WaveBriefView, { brief }));
  for (const href of [brief.waveHref, brief.outcome.tasks[0].taskHref, brief.seeIt[0].evidenceHref, brief.reworkParked[0].taskHref, brief.humanAction[0].gateHref, brief.documentation[0].nodeHref]) {
    expect(html).toContain(`href="${href.replaceAll("&", "&amp;")}"`);
  }
  for (const exhaust of ["Latest attempt", "rawLogPath", "logsSummary", "tokenCount", "transcript"]) expect(source).not.toContain(exhaust);
});
