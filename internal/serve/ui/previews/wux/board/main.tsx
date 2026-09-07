import { useState } from "react";
import { createRoot } from "react-dom/client";
import type { RunSummary, TaskCapsule } from "@/types/domain";
import "@/styles/app.css";
import { TaskBoard } from "@/features/workbench/board";

const sampleTasks: TaskCapsule[] = [
  { id: "WUX-T-0001", title: "Restore keyboard focus after filtering", epicId: "", epicTitle: "", status: "backlog", readiness: "draft", priority: "p2", risk: "low", hasGate: false, updatedAt: "2026-09-07", projectId: "preview" },
  { id: "WUX-T-0002", title: "Keep task details available when a wave is blocked", epicId: "WUX", epicTitle: "Work experience", status: "blocked", readiness: "blocked_dependency", priority: "p1", risk: "medium", hasGate: true, updatedAt: "2026-09-07", projectId: "preview" },
  { id: "WUX-T-0003", title: "Review the board interaction contract", epicId: "WUX", epicTitle: "Work experience", status: "review", readiness: "ready", priority: "p1", risk: "medium", hasGate: false, updatedAt: "2026-09-07", projectId: "preview" },
  { id: "WUX-T-0004", title: "Document the original task state model", epicId: "WUX", epicTitle: "Work experience", status: "done", readiness: "ready", priority: "p2", risk: "low", hasGate: false, updatedAt: "2026-09-06", projectId: "preview" },
  { id: "WUX-T-0005", title: "Make the board safe to start", epicId: "WUX", epicTitle: "Work experience", status: "ready", readiness: "ready", priority: "p1", risk: "medium", hasGate: false, updatedAt: "2026-09-07", projectId: "preview" },
  { id: "WUX-T-0006", title: "Build a narrow-screen task list", epicId: "WUX", epicTitle: "Work experience", status: "in_progress", readiness: "ready", priority: "p1", risk: "medium", hasGate: false, updatedAt: "2026-09-07", liveRun: true, projectId: "preview" },
];

const sampleRuns: RunSummary[] = [{
  taskId: "WUX-T-0006", taskTitle: "Build a narrow-screen task list", projectId: "preview", runner: "codex", model: "gpt-5.6-luna", lane: "execute", leaseState: "held", outcome: "running", elapsedSec: 92, sinceLastEventSec: 3, liveness: "fresh", attemptCount: 1,
}];

const tagsByTaskId: Record<string, string[]> = {
  "WUX-T-0001": ["accessibility", "frontend"],
  "WUX-T-0002": ["recovery", "frontend"],
  "WUX-T-0003": ["review", "frontend"],
  "WUX-T-0004": ["docs"],
  "WUX-T-0005": ["auth", "frontend"],
  "WUX-T-0006": ["accessibility", "frontend"],
};

function Preview() {
  const [mode, setMode] = useState<"board" | "list">("board");
  const [selectedTags, setSelectedTags] = useState<string[]>([]);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const selectedTask = sampleTasks.find((task) => task.id === selectedTaskId);

  return (
    <div data-wux-ready="true" className="min-h-screen bg-surface px-5 py-8 text-ink sm:px-8 lg:px-12">
      <div className="mx-auto max-w-[1240px]">
        <header className="mb-8 border-b border-line pb-6">
          <div className="font-mono text-[10.5px] font-semibold uppercase tracking-[0.14em] text-faint">Sample data · Work</div>
          <h1 className="mt-2 text-[30px] font-semibold tracking-[-0.03em] sm:text-[38px]">Task board</h1>
          <p className="mt-2 max-w-2xl text-[14px] leading-relaxed text-muted">Browse bounded work in two views. Topics are optional preview data and match all selected filters.</p>
        </header>
        <TaskBoard
          tasks={sampleTasks}
          runs={sampleRuns}
          mode={mode}
          onModeChange={setMode}
          onSelectTask={setSelectedTaskId}
          tagsByTaskId={tagsByTaskId}
          selectedTags={selectedTags}
          onSelectedTagsChange={setSelectedTags}
          tagsAvailable
        />
        {selectedTask && (
          <aside aria-label="Selected task" className="mt-6 rounded-xl border border-accent/30 bg-accent-soft/40 p-4">
            <div className="font-mono text-[10px] uppercase tracking-[0.12em] text-accent">Contextual inspection</div>
            <h2 className="mt-1 text-[15px] font-semibold text-ink">{selectedTask.title}</h2>
            <p className="mt-2 text-[12px] leading-5 text-muted">Status actions remain with the caller, where server guards are applied. This preview only selects the task.</p>
            <button type="button" onClick={() => setSelectedTaskId(null)} className="mt-3 text-[12px] font-medium text-ink underline underline-offset-4">Close inspection</button>
          </aside>
        )}
      </div>
    </div>
  );
}

createRoot(document.getElementById("root")!).render(<Preview />);
