import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useNeeds, useProjects, useReviewBatch, useRuns } from "@/lib/queries";
import { isTuskerShellMode } from "@/routes/__root";
import type { NeedItem, RunSummary, TaskCapsule } from "@/types/domain";
import { HumanActionCard } from "@/features/human-action/HumanActionCard";
import {
  ALL_PROJECTS,
  ALL_PROJECTS_VALUE,
  PANEL_PROJECT_STORAGE_KEY,
  projectIdOf,
  projectOptionLabel,
  projectOverviewPath,
  projectSelection,
  projectSelectionFromValue,
  projectSelectionValue,
  resolveProjectSelection,
  sameProjectSelection,
} from "@/features/panel/projectSelection";
import { humanActionIdentity, partitionPanelNeeds, taskIdentity } from "@/features/panel/panelModel";
import { openTaskSearch } from "@/features/search/TaskSearch";

declare global {
  interface Window {
    tuskerShell?: {
      openFull?: (path: string) => void;
      onNavigate?: (path: string) => boolean;
      pickFolder?: () => Promise<string | undefined>;
    };
  }
}

type TriageRow = { key: string; id: string; title: string; chip: string; path: string; tone?: "attention" | "running" | "failed" };

function taskPath(projectId: string, taskId: string): string {
  return `/p/${encodeURIComponent(projectId)}/docs?path=${encodeURIComponent(taskId)}`;
}

function age(value: string | undefined): string {
  if (!value) return "now";
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "now";
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  return `${Math.floor(minutes / 60)}h`;
}

function Section({ title, rows, onOpen }: { title: string; rows: TriageRow[]; onOpen: (row: TriageRow) => void }) {
  return (
    <section className="border-b border-line-soft px-3 py-2 last:border-b-0">
      <div className="mb-1 flex items-center justify-between">
        <h2 className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted">{title}</h2>
        <span className="font-mono text-[11px] text-faint">{rows.length}</span>
      </div>
      <div className="divide-y divide-line-soft">
        {rows.map((row) => (
          <button key={`${title}-${row.key}`} type="button" onClick={() => onOpen(row)} className="flex w-full min-w-0 items-center gap-2 py-2 text-left hover:bg-hover">
            <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-soft">{row.id}</span>
            <span className="min-w-0 flex-[2] truncate text-[13px] text-ink">{row.title || "Untitled task"}</span>
            <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] ${row.tone === "failed" ? "bg-fail-soft text-fail" : row.tone === "running" ? "bg-info-soft text-info" : "bg-warn-soft text-warn"}`}>{row.chip}</span>
          </button>
        ))}
      </div>
    </section>
  );
}

export function Panel() {
  const navigate = useNavigate();
  const projectsQ = useProjects();
  const [storedSelection, setStoredSelection] = useState(() => {
    if (typeof window === "undefined") return ALL_PROJECTS;
    try {
      return projectSelectionFromValue(window.localStorage.getItem(PANEL_PROJECT_STORAGE_KEY));
    } catch {
      return ALL_PROJECTS;
    }
  });
  const projects = projectsQ.data ?? [];
  const selection = projectsQ.data
    ? resolveProjectSelection(storedSelection, projects)
    : projectsQ.isError ? ALL_PROJECTS : storedSelection;
  const readsEnabled = !projectsQ.isPending;
  const selectedProjectId = projectIdOf(selection);
  const needsQ = useNeeds(selectedProjectId, readsEnabled);
  const reviewQ = useReviewBatch(selectedProjectId, readsEnabled);
  const runsQ = useRuns(selectedProjectId, readsEnabled);
  const needs = needsQ.data ?? [];
  const review = reviewQ.data
    ? [...reviewQ.data.waves.flatMap((wave) => wave.members), ...reviewQ.data.unwaved].filter((task) => task.status === "review")
    : [];
  const runs = runsQ.data ?? [];
  const open = (row: TriageRow) => {
    const embedded = isTuskerShellMode();
    if (embedded) {
      const bridge = window.tuskerShell?.openFull;
      if (bridge) {
        bridge(row.path);
        return;
      }
    }
    void navigate({ to: row.path as "/" });
  };
  const openDesktop = () => {
    const path = projectOverviewPath(selection);
    const bridge = window.tuskerShell?.openFull;
    if (bridge) {
      bridge(path);
      return;
    }
    void navigate({ to: path as "/" });
  };
  const selectProject = (nextSelection: typeof selection) => {
    setStoredSelection(nextSelection);
    try {
      if (nextSelection.kind === "all") window.localStorage.removeItem(PANEL_PROJECT_STORAGE_KEY);
      else window.localStorage.setItem(PANEL_PROJECT_STORAGE_KEY, projectSelectionValue(nextSelection));
    } catch {
      // Storage can be unavailable in hardened webviews; the in-memory choice still works.
    }
  };
  useEffect(() => {
    if (!projectsQ.data || sameProjectSelection(selection, storedSelection)) return;
    selectProject(selection);
  }, [projectsQ.data, selection, storedSelection]);
  useEffect(() => {
    const shell = window.tuskerShell;
    if (!shell) return;
    const onNavigate = (path: string) => { void navigate({ to: path as "/" }); return true; };
    shell.onNavigate = onNavigate;
    return () => { if (window.tuskerShell?.onNavigate === onNavigate) delete window.tuskerShell.onNavigate; };
  }, [navigate]);
  const { humanActionRows, attentionNeeds } = useMemo(() => partitionPanelNeeds(needs), [needs]);
  const attentionRows = useMemo(() => attentionNeeds.map((item: NeedItem) => ({
    key: taskIdentity(item.projectId, item.taskId), id: item.taskId, title: item.taskTitle, chip: item.kind === "failed" ? "failed" : age(item.since), path: taskPath(item.projectId, item.taskId), tone: item.kind === "failed" ? "failed" as const : "attention" as const,
  })), [attentionNeeds]);
  const reviewRows = useMemo(() => review.map((task: TaskCapsule) => ({ key: taskIdentity(task.projectId || "", task.id), id: task.id, title: task.title, chip: age(task.updatedAt), path: taskPath(task.projectId || "", task.id), tone: "attention" as const })), [review]);
  const runningRows = useMemo(() => runs.filter((run: RunSummary) => run.outcome === "running" || run.processRunning).map((run) => ({ key: taskIdentity(run.projectId, run.taskId), id: run.taskId, title: run.taskTitle, chip: run.lane, path: taskPath(run.projectId, run.taskId), tone: "running" as const })), [runs]);
  const failureRows = useMemo(() => runs.filter((run) => run.outcome === "failed" || run.leaseStateRaw === "parked_no_progress").map((run) => ({ key: taskIdentity(run.projectId, run.taskId), id: run.taskId, title: run.taskTitle, chip: "failed", path: taskPath(run.projectId, run.taskId), tone: "failed" as const })), [runs]);
  const dataLoading = projectsQ.isPending || (readsEnabled && (needsQ.isPending || reviewQ.isPending || runsQ.isPending));
  const allEmpty = humanActionRows.length + attentionRows.length + reviewRows.length + runningRows.length + failureRows.length === 0;
  return (
    <div className="h-full w-full overflow-y-auto overflow-x-hidden bg-surface">
      <header className="sticky top-0 z-10 border-b border-line bg-surface/95 px-3 py-3 backdrop-blur">
        <div className="flex items-center justify-between gap-3">
          <h1 className="min-w-0 truncate font-serif text-[19px] font-semibold">Tusker triage</h1>
          <div className="flex shrink-0 items-center gap-2">
            <span className="text-[11px] text-muted">live</span>
            <button
              type="button"
              onClick={openTaskSearch}
              aria-label="Search tasks"
              className="rounded-md border border-line bg-panel px-2 py-1.5 text-[11px] font-semibold text-ink-soft shadow-sm hover:bg-hover hover:text-ink"
            >
              Search
            </button>
            <button
              type="button"
              onClick={openDesktop}
              aria-label="Open the main Tusker window"
              className="rounded-md border border-line bg-panel px-2.5 py-1.5 text-[11px] font-semibold text-ink-soft shadow-sm hover:bg-hover hover:text-ink"
            >
              Open Tusker <span aria-hidden="true">↗</span>
            </button>
          </div>
        </div>
        <label className="mt-2 block min-w-0">
          <span className="sr-only">Project shown in Tusker triage</span>
          <select
            aria-label="Project shown in Tusker triage"
            value={projectsQ.data ? projectSelectionValue(selection) : ALL_PROJECTS_VALUE}
            onChange={(event) => selectProject(projectSelectionFromValue(event.target.value))}
            disabled={projectsQ.isPending || projectsQ.isError}
            className="h-8 w-full min-w-0 truncate rounded-md border border-line bg-panel px-2 text-[11px] font-semibold text-ink-soft outline-none focus-visible:ring-2 focus-visible:ring-accent/30 disabled:opacity-60"
          >
            <option value={ALL_PROJECTS_VALUE}>All projects</option>
            {projects.map((project) => (
              <option key={project.id} value={projectSelectionValue(projectSelection(project.id))}>{projectOptionLabel(project)}</option>
            ))}
          </select>
        </label>
      </header>
      {dataLoading ? <div aria-live="polite" className="flex min-h-40 items-center justify-center px-6 text-center text-[13px] text-muted">Loading project triage…</div> : allEmpty ? <div className="flex min-h-40 items-center justify-center px-6 text-center font-serif text-[18px] text-muted">Nothing needs you in {selection.kind === "all" ? "any project" : projects.find((project) => project.id === selection.projectId)?.name ?? "this project"}</div> : <div className="pb-3">
        {humanActionRows.length > 0 && (
          <section className="border-b border-line-soft px-3 py-3">
            <div className="mb-2 flex items-center justify-between">
              <h2 className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted">Your action</h2>
              <span className="font-mono text-[11px] text-faint">{humanActionRows.length}</span>
            </div>
            <div className="space-y-2">
              {humanActionRows.map((item) => (
                <HumanActionCard
                  key={humanActionIdentity(item)}
                  action={item.humanAction!}
                  taskId={item.taskId}
                  taskTitle={item.taskTitle}
                  projectId={item.projectId}
                  blockedTaskIds={item.blockedTaskIds}
                  compact
                />
              ))}
            </div>
          </section>
        )}
        <Section title="Needs attention" rows={attentionRows} onOpen={open} />
        <Section title="Review queue" rows={reviewRows} onOpen={open} />
        <Section title="Running now" rows={runningRows} onOpen={open} />
        <Section title="Recent failures" rows={failureRows} onOpen={open} />
      </div>}
    </div>
  );
}
