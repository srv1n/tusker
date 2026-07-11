import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import {
  clockTime,
  redriveDisabledReason,
  runStats,
  waitingForDaemonReason,
} from "../src/features/runs/detail/helpers";
import { outcomeLabelOf, outcomeToneOf } from "../src/components/ui/tone";
import type { RunDetail } from "../src/types/domain";

// ---------------------------------------------------------------------------
// SRV-T-0015 A1 — event-tail timestamps never render NaN
// ---------------------------------------------------------------------------

test("clockTime renders a valid UTC wall clock for the API timestamp", () => {
  // The runs API now emits the "at" field (RFC3339); this is what the tail parses.
  expect(clockTime("2026-07-08T18:22:05Z")).toBe("18:22:05");
});

test("clockTime degrades to a placeholder instead of NaN:NaN:NaN", () => {
  expect(clockTime("")).toBe("--:--:--");
  expect(clockTime("not-a-timestamp")).toBe("--:--:--");
  // The literal defect the fix removes.
  expect(clockTime("")).not.toContain("NaN");
});

// ---------------------------------------------------------------------------
// SRV-T-0015 A2/A3 — labeled columns incl. lease/outcome, terminal distinction
// ---------------------------------------------------------------------------

test("runs board header labels every column including lease and state", () => {
  const src = readFileSync("src/features/runs/board/rows.tsx", "utf8");
  for (const label of ["Task", "Runner", "Lane", "Lease", "Tokens", "State"]) {
    expect(src).toMatch(new RegExp(`<span(?: className="[^"]*")?>${label}</span>`));
  }
});

test("recent rows are visually distinct from live runs (terminal tag + attempt count)", () => {
  const src = readFileSync("src/features/runs/board/rows.tsx", "utf8");
  expect(src).toContain("terminal");
  expect(src).toContain("attempts");
  expect(src).toContain("bg-panel/40");
});

test("both the active and recent boards render the labeled header", () => {
  const src = readFileSync("src/features/runs/ProjectRuns.tsx", "utf8");
  const headerCount = src.split("<RunsTableHeader />").length - 1;
  expect(headerCount).toBe(2);
});

// ---------------------------------------------------------------------------
// SRV-T-0016 — generic outcome rendering, Retry honesty, canonical badge
// ---------------------------------------------------------------------------

test("outcome rendering is generic — a new API outcome still displays", () => {
  // A closed switch would have thrown/blanked; the open enum humanizes it.
  expect(outcomeLabelOf("review-complete")).toBe("Review complete");
  expect(outcomeLabelOf("awaiting_land")).toBe("Awaiting land");
  expect(outcomeToneOf("review-complete")).toBe("neutral");
  // Known values keep their curated tone/label.
  expect(outcomeLabelOf("succeeded")).toBe("Succeeded");
  expect(outcomeToneOf("succeeded")).toBe("pass");
});

test("redrive is disabled with an explanation for review/done, allowed otherwise", () => {
  const review = redriveDisabledReason("review");
  expect(review).not.toBeNull();
  expect(review).toContain("review");

  const done = redriveDisabledReason("done");
  expect(done).not.toBeNull();

  expect(redriveDisabledReason("ready")).toBeNull();
  expect(redriveDisabledReason("in_progress")).toBeNull();
  expect(redriveDisabledReason(undefined)).toBeNull();
});

test("Retry maps to redrive, disables from canonical status, and surfaces the result", () => {
  const src = readFileSync("src/features/runs/detail/RunHeader.tsx", "utf8");
  // Labeled as redrive, not an ambiguous "Retry".
  expect(src).toContain("Redrive");
  expect(src).toContain("tusker redrive");
  // Disabled decision is driven by canonical task status (capsule), not the run row.
  expect(src).toContain("redriveDisabledReason(capsule?.status, run, waitingForDaemonReason)");
  expect(src).toContain("disabled={redriveDisabled}");
  // The redrive result reason is rendered, never swallowed.
  expect(src).toContain("retry.result.reason");
});

test("the redrive action posts to the run redrive endpoint", () => {
  const src = readFileSync("src/lib/api.ts", "utf8");
  expect(src).toContain("/runs/${taskId}/redrive");
  expect(src).toContain("post(");
});

test("the interrupt action posts to the guarded run interrupt endpoint", () => {
  const src = readFileSync("src/lib/api.ts", "utf8");
  expect(src).toContain("/runs/${taskId}/interrupt");
  expect(src).toContain("processRunning");
});

test("retry queued plus daemon down pauses timing and disables duplicate redrive", () => {
  const queued = {
    taskId: "SRV-T-0021",
    taskTitle: "Queued run",
    projectId: "tusker",
    runner: "codex",
    model: "gpt-5.4-codex",
    lane: "execute",
    leaseState: "unclaimed",
    leaseStateRaw: "retry_queued",
    processRunning: false,
    outcome: "retry-queued",
    elapsedSec: 90,
    sinceLastEventSec: 600,
    liveness: "dead",
    tokens: { input: 10, output: 5 },
    attemptCount: 1,
    workspacePath: "/tmp/SRV-T-0021",
    attempts: [],
    events: [],
  } satisfies RunDetail;
  const reason = waitingForDaemonReason(queued, {
    daemonAlive: false,
    daemonDownReason: "Daemon process is not running. Start the daemon to dispatch queued work.",
  });

  expect(reason).toContain("Start the daemon");
  expect(runStats(queued, reason !== null)[0]).toEqual({ label: "Elapsed", value: "Paused" });
  expect(redriveDisabledReason("ready", queued, reason)).toContain("already queued");

  const header = readFileSync("src/features/runs/detail/RunHeader.tsx", "utf8");
  const tail = readFileSync("src/features/runs/detail/EventTail.tsx", "utf8");
  expect(header).toContain("Waiting for daemon");
  expect(header).toContain("!waitingForDaemonReason");
  expect(tail).toContain("waitingForDaemonReason ? (");
  expect(tail).toContain("Waiting for daemon");
});
