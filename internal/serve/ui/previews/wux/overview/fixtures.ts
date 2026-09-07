import type { RunSummary, TaskCapsule, WaveSummary } from "@/types/domain";
import type { Startability } from "@/features/workbench/overview/groupWaves";
import {
  humanActionEntry,
  makeRun,
  makeTask,
  makeWave,
} from "@/features/workbench/overview/previewFixtures";

// Clearly labeled sample data for the isolated WUX overview preview.
// Production data remains a separate integration requirement.

export interface OverviewFixture {
  waves: WaveSummary[];
  tasks: TaskCapsule[];
  runs: RunSummary[];
  startability: Record<string, Startability>;
  descriptions: Record<string, string>;
}

const LONG_DESCRIPTION =
  "Rework the everyday project surface so people can see what needs attention, what is moving and what is ready " +
  "without reading a second inventory of tickets, across desktop and narrow widths, with truthful states " +
  "even when some reads fail or arrive stale and long titles wrap instead of truncating meaning away.";

export function mixedFixture(): OverviewFixture {
  const waves: WaveSummary[] = [
    makeWave({
      id: "W-NEED",
      title: "Approve the launch checklist",
      memberIds: ["T-1", "T-2"],
      brief: {
        schema: "tusker.wave-brief/v1",
        waveId: "W-NEED",
        title: "Approve the launch checklist",
        waveHref: "",
        sectionOrder: ["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"],
        outcome: { summary: "", fullyDrained: false, counts: {}, tasks: [] },
        seeIt: [],
        landed: [],
        reworkParked: [],
        humanAction: [humanActionEntry("G-1", ["T-1"])],
        documentation: [],
      },
    }),
    makeWave({ id: "W-RUN", title: "Ship the editor refresh", memberIds: ["T-3", "T-4"] }),
    makeWave({ id: "W-READY", title: "Start the docs pass", memberIds: ["T-5"] }),
    makeWave({ id: "W-QUEUED", title: "Queue the migration", memberIds: ["T-6"] }),
    makeWave({
      id: "W-PAUSED",
      title: "Paused platform upgrade",
      memberIds: ["T-7"],
      authorization: { state: "paused", stale: false, action: "Sample: paused by the owner." },
    }),
    makeWave({
      id: "W-PLAN",
      title: "Plan the onboarding tour",
      memberIds: ["T-8"],
      authorization: { state: "disarmed", stale: false, action: "" },
    }),
    makeWave({ id: "W-LONG", title: "Everyday project surface rework", memberIds: ["T-9"] }),
    makeWave({
      id: "W-DRAINED",
      title: "Finished tasks, no landing receipt",
      memberIds: ["T-10"],
      brief: {
        schema: "tusker.wave-brief/v1",
        waveId: "W-DRAINED",
        title: "Finished tasks, no landing receipt",
        waveHref: "",
        sectionOrder: ["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"],
        outcome: { summary: "", fullyDrained: true, counts: {}, tasks: [] },
        seeIt: [],
        landed: [],
        reworkParked: [],
        humanAction: [],
        documentation: [],
      },
    }),
    makeWave({
      id: "W-DONE",
      title: "Released the sidebar",
      memberIds: ["T-11"],
      landedAt: "2026-09-06T10:00:00Z",
    }),
    makeWave({ id: "W-OLD", title: "Superseded spike", status: "superseded", memberIds: ["T-12"] }),
    makeWave({
      id: "W-STALE",
      title: "Wave with a stale read",
      memberIds: ["T-13"],
      authorization: { state: "stale", stale: true, action: "" },
    }),
  ];
  const tasks: TaskCapsule[] = [
    makeTask({ id: "T-1", title: "Decide the launch scope", hasGate: true }),
    makeTask({ id: "T-2", title: "Draft the announcement" }),
    makeTask({ id: "T-3", title: "Rebuild the editor toolbar", status: "in_progress" }),
    makeTask({ id: "T-4", title: "Review the toolbar draft", status: "review" }),
    makeTask({ id: "T-5", title: "Outline the docs pass" }),
    makeTask({ id: "T-6", title: "Stage the migration queue" }),
    makeTask({ id: "T-7", title: "Upgrade the platform adapter" }),
    makeTask({ id: "T-8", title: "Sketch the onboarding tour" }),
    makeTask({ id: "T-9", title: "Rework rows, states and empty cases" }),
    makeTask({ id: "T-10", title: "Wrap the unlanded batch", status: "done" }),
    makeTask({ id: "T-11", title: "Land the sidebar", status: "done" }),
    makeTask({ id: "T-12", title: "Spike the old approach", status: "done" }),
    makeTask({ id: "T-13", title: "Task under a stale wave", status: "in_progress" }),
    makeTask({ id: "T-LOOSE-1", title: "Untriaged inbox item" }),
    makeTask({ id: "T-LOOSE-2", title: " stray idea without a wave".trim() }),
  ];
  const runs: RunSummary[] = [
    makeRun({ taskId: "T-1" }),
    makeRun({ taskId: "T-3" }),
    makeRun({ taskId: "T-4", lane: "review" }),
    makeRun({ taskId: "T-13", liveness: "stale", sinceLastEventSec: 1200 }),
  ];
  return {
    waves,
    tasks,
    runs,
    startability: {
      "W-NEED": { state: "blocked", reason: "Sample: waiting on a human decision." },
      "W-RUN": { state: "blocked", reason: "Sample: work is underway." },
      "W-READY": { state: "ready", reason: "Sample: readiness permits start." },
      "W-QUEUED": { state: "unknown", reason: "Sample: scheduler read failed." },
      "W-PAUSED": { state: "blocked", reason: "Sample: paused." },
      "W-PLAN": { state: "blocked", reason: "Sample: not authorized yet." },
      "W-LONG": { state: "blocked", reason: "Sample: still being scoped." },
      "W-DRAINED": { state: "unknown", reason: "Sample: landing receipt missing." },
      "W-DONE": { state: "unknown", reason: "Sample: terminal wave." },
      "W-OLD": { state: "unknown", reason: "Sample: terminal wave." },
      "W-STALE": { state: "unknown", reason: "Sample: stale read." },
    },
    descriptions: {
      "W-NEED": "Sample: one decision blocks this wave; execution continues on the other branch.",
      "W-RUN": "Sample: editor refresh in flight with an independent review running alongside.",
      "W-READY": "Sample: scope agreed and readiness permits start.",
      "W-QUEUED": "Sample: queued behind an unrelated migration window.",
      "W-PAUSED": "Sample: paused by the owner until the adapter settles.",
      "W-LONG": `Sample: ${LONG_DESCRIPTION}`,
      "W-DRAINED": "Sample: every task reads done, but no landing receipt exists — not completion.",
      "W-DONE": "Sample: landed and reviewable under completed history.",
      "W-OLD": "Sample: superseded; kept as history, never shown as success.",
      "W-STALE": "Sample: required reads are stale, so no ready or completed claim is made.",
    },
  };
}
