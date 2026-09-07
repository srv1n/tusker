import { useState } from "react";
import { createRoot } from "react-dom/client";
import "@/styles/app.css";
import { Chip } from "@/components/ui/primitives";
import { WaveResults } from "@/features/workbench/results/WaveResults";
import type { TaskDetail, WaveSummary } from "@/types/domain";

const baseTasks: TaskDetail[] = [
  {
    id: "WUX-T-0004", title: "Build the grouped wave overview", projectId: "tusker", waveTerminal: true, epicId: "WUX", epicTitle: "Work experience", status: "done", readiness: "ready", priority: "p1", risk: "medium", hasGate: false, updatedAt: "2026-09-07T06:00:00Z",
    intent: "People can see what needs attention, what is moving and what is ready without reading another ticket inventory.",
    acceptance: [{ id: "A1", text: "Every wave appears once in the right state section.", proof: "pass" }, { id: "A4", text: "The rendered preview remains usable at narrow widths.", proof: "pass" }],
    nonGoals: [], verification: [{ id: "V1", command: "bun test", result: "pass" }],
    evidence: [{ id: "E1", label: "Render: 182 ms · 30 nodes · Chrome 140 on macOS", kind: "file", ref: "#metrics" }], deps: [], gates: [], runHistory: [],
  },
  {
    id: "WUX-T-0005", title: "Build the readable interactive wave graph", projectId: "tusker", waveTerminal: true, epicId: "WUX", epicTitle: "Work experience", status: "done", readiness: "ready", priority: "p1", risk: "medium", hasGate: false, updatedAt: "2026-09-07T06:00:00Z",
    intent: "People can understand prerequisites and current progress without losing their place while inspecting a task.",
    acceptance: [{ id: "A2", text: "Pan, zoom, fit and keyboard access preserve the graph context.", proof: "pass" }],
    nonGoals: [], verification: [{ id: "V1", command: "bun test", result: "pass" }],
    evidence: [{ id: "E2", label: "Desktop graph capture", kind: "image", ref: "#single-image" }], deps: [], gates: [], runHistory: [],
  },
  {
    id: "WUX-T-0006", title: "Build contextual task inspection", projectId: "tusker", waveTerminal: true, epicId: "WUX", epicTitle: "Work experience", status: "done", readiness: "ready", priority: "p1", risk: "medium", hasGate: false, updatedAt: "2026-09-07T06:00:00Z",
    intent: "People can inspect the actual task stage, decision and result without leaving the current work surface.",
    acceptance: [{ id: "A1", text: "The task intent and accepted result appear before technical metadata.", proof: "pass" }],
    nonGoals: [], verification: [{ id: "V1", command: "bun test", result: "pass" }],
    evidence: [], deps: [], gates: [], runHistory: [],
  },
];

const acceptedBrief = {
  schema: "tusker.wave-brief/v1", waveId: "W-0013", title: "Work-area UI", waveHref: "#flow",
  sectionOrder: ["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"],
  outcome: {
    summary: "The work area now makes delivery state, task context and accepted outcomes readable in one place.", fullyDrained: true, counts: { delivered: 3, reviewed: 3 },
    tasks: [
      { taskId: "WUX-T-0004", title: baseTasks[0].title, taskHref: "#WUX-T-0004", implementation: "present", proof: "pass", review: "accepted", landing: "landed", documentation: "current" },
      { taskId: "WUX-T-0005", title: baseTasks[1].title, taskHref: "#WUX-T-0005", implementation: "present", proof: "pass", review: "accepted", landing: "landed", documentation: "current" },
      { taskId: "WUX-T-0006", title: baseTasks[2].title, taskHref: "#WUX-T-0006", implementation: "present", proof: "pass", review: "accepted", landing: "landed", documentation: "current" },
    ],
  },
  seeIt: [
    { taskId: "WUX-T-0004", taskHref: "#WUX-T-0004", kind: "screenshot", priority: 1, summary: "Overview desktop capture", acceptanceIds: ["A1", "A4"], evidenceRef: "E-WUX-0004", artifactRef: "artifacts/overview.png", evidenceHref: "#accepted-overview" },
    { taskId: "WUX-T-0005", taskHref: "#WUX-T-0005", kind: "image", priority: 2, summary: "Graph image without pairing provenance", acceptanceIds: ["A2"], evidenceRef: "E-WUX-0005", evidenceHref: "#missing-image" },
    { taskId: "WUX-T-0006", taskHref: "#WUX-T-0006", kind: "video", priority: 3, summary: "Unsupported recording format", acceptanceIds: ["A1"], evidenceRef: "E-WUX-0006", artifactRef: "artifacts/inspection.mp4", evidenceHref: "#unsupported-video" },
  ],
  landed: baseTasks.map((task) => ({ taskId: task.id, title: task.title, taskHref: `#${task.id}`, commit: "abc1234", target: "main" })),
  reworkParked: [], humanAction: [], documentation: [],
} as const;

const wave: WaveSummary = {
  id: "W-0013", title: "Work-area UI", status: "completed", landedAt: "2026-09-07T06:00:00Z", memberIds: baseTasks.map((task) => task.id), members: baseTasks.map((task) => ({ id: task.id, title: task.title, status: task.status })), counts: { done: 3 },
  authorization: { state: "disarmed", stale: false, action: "Landed after independent review", actor: "agent:preview", at: "2026-09-07T06:00:00Z" }, brief: acceptedBrief,
};

function App() {
  const [evidenceGap, setEvidenceGap] = useState(false);
  const [message, setMessage] = useState("Select a task or open Flow to exercise the result reader.");
  const previewWave = evidenceGap ? { ...wave, brief: { ...wave.brief, seeIt: [] } } : wave;

  return (
    <div>
      <div className="sticky top-0 z-10 flex flex-wrap items-center justify-between gap-3 border-b border-line bg-surface/95 px-4 py-3 backdrop-blur sm:px-6 lg:px-8">
        <div className="flex items-center gap-2"><Chip tone="accent" mono>Preview fixture</Chip><span className="text-[12px] text-muted">Results contract scenarios</span></div>
        <div className="flex flex-wrap gap-2" role="group" aria-label="Preview scenario">
          <button type="button" onClick={() => setEvidenceGap(false)} className={`rounded-md border px-2.5 py-1.5 text-[11px] ${!evidenceGap ? "border-ink bg-ink text-surface" : "border-line bg-raised text-muted"}`}>Accepted evidence</button>
          <button type="button" onClick={() => setEvidenceGap(true)} className={`rounded-md border px-2.5 py-1.5 text-[11px] ${evidenceGap ? "border-warn bg-warn-soft text-warn" : "border-line bg-raised text-muted"}`}>Evidence gap</button>
        </div>
      </div>
      <div aria-live="polite" className="sr-only">{message}</div>
      <WaveResults wave={previewWave} tasks={baseTasks} onOpenFlow={() => setMessage("Flow opened for W-0013.")} onOpenTask={(id) => setMessage(`Task ${id} opened.`)} />
    </div>
  );
}

createRoot(document.getElementById("root")!).render(<App />);
