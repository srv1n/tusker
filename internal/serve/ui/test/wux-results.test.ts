import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { artifactKindLabel, artifactPresentation, resultTrustSummary } from "../src/features/workbench/results/WaveResults";

const source = readFileSync(new URL("../src/features/workbench/results/WaveResults.tsx", import.meta.url), "utf8");

test("results drained does not prove success", () => {
  const summary = resultTrustSummary({ outcome: { fullyDrained: true, counts: {}, summary: "All workers exited", tasks: [] }, seeIt: [] });

  expect(summary.drained).toBe(true);
  expect(summary.acceptedEvidence).toBe(0);
  expect(summary.hasEvidenceGap).toBe(true);
  expect(source).toContain("drained work, successful processes and uploaded artifacts do not prove acceptance");
});

test("results unpaired image stays single", () => {
  expect(artifactKindLabel("image")).toBe("Image · single");
  expect(artifactKindLabel("screenshot")).toBe("Image · single");
  expect(artifactKindLabel("image").toLowerCase()).not.toContain("before");
  expect(source).toContain("Do not infer before/after");
});

test("results missing evidence remains visible", () => {
  expect(artifactPresentation({ kind: "image", artifactRef: undefined }).state).toBe("Asset unavailable");
  expect(artifactPresentation({ kind: "video", artifactRef: "artifacts/run.mp4" }).state).toBe("Unsupported asset");
  expect(source).toContain("Evidence link unavailable");
  expect(source).toContain("No task evidence supplied.");
});

test("results keeps the outcome reader actions and facts visible", () => {
  for (const text of ["Delivered outcome", "Acceptance and review", "Independent review facts", "Open flow", "Canonical acceptance only"]) expect(source).toContain(text);
  expect(source).toContain("onOpenTask");
  expect(source).toContain("wave.brief.outcome.summary");
  expect(source).toContain("task.evidence");
});
