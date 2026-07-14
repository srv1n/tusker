import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
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
  expect(briefSource).toContain("artifact.evidenceRef");
  expect(briefSource).toContain("row.link");
  expect(briefSource).not.toContain("tokenCount");
  expect(briefSource).not.toContain("transcript");
  expect(briefSource).not.toContain("rawLog");
});
