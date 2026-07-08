import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { isLiveHeaderRun, runStats } from "../src/features/runs/detail/helpers";
import type { RunDetail } from "../src/types/domain";

const completedRun = {
  taskId: "TRC-T-0002",
  taskTitle: "Completed trace run",
  projectId: "tusker",
  runner: "codex",
  model: "gpt-5.4-codex",
  lane: "execute",
  leaseState: "unclaimed",
  outcome: "succeeded",
  elapsedSec: 886,
  sinceLastEventSec: 6,
  liveness: "fresh",
  tokens: { input: 150, output: 40 },
  attemptCount: 1,
  terminal: true,
  workspacePath: "/tmp/TRC-T-0002",
  attempts: [
    {
      n: 1,
      outcome: "succeeded",
      durationSec: 886,
      tokens: { input: 150, output: 40 },
      startedAt: "2026-07-08T03:00:00Z",
    },
  ],
  events: [],
} satisfies RunDetail;

test("runs-detail header derives live state from terminal lease/outcome", () => {
  const source = readFileSync("src/features/runs/detail/RunHeader.tsx", "utf8");

  expect(isLiveHeaderRun(completedRun)).toBe(false);
  expect(source).toContain("const live = isLiveHeaderRun(run)");
  expect(source).toContain("{live && (");
  expect(source.indexOf("<LivenessIndicator")).toBeGreaterThan(source.indexOf("{live && ("));
});

test("runs-detail stats freeze released elapsed and show recorded turn tokens", () => {
  expect(runStats(completedRun)).toEqual([
    { label: "Elapsed", value: "14m 46s" },
    { label: "Input", value: "150" },
    { label: "Output", value: "40" },
    { label: "Attempts", value: "1" },
  ]);
});

test("runs-detail header still marks a held running run as live", () => {
  const runningRun = {
    ...completedRun,
    leaseState: "held",
    outcome: "running",
  } satisfies RunDetail;

  expect(isLiveHeaderRun(runningRun)).toBe(true);
});
