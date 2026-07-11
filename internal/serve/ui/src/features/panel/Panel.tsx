import { useEffect, useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useNeeds, useReviewBatch, useRuns } from "@/lib/queries";
import { isTuskerShellMode } from "@/routes/__root";
import type { NeedItem, RunSummary, TaskCapsule } from "@/types/domain";

declare global {
  interface Window {
    tuskerShell?: { openFull?: (path: string) => void; onNavigate?: (path: string) => boolean };
  }
}

type TriageRow = { id: string; title: string; chip: string; path: string; tone?: "attention" | "running" | "failed" };

function taskPath(projectId: string, taskId: string): string {
  return `/p/${encodeURIComponent(projectId)}/work?task=${encodeURIComponent(taskId)}`;
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
          <button key={`${title}-${row.id}`} type="button" onClick={() => onOpen(row)} className="flex w-full min-w-0 items-center gap-2 py-2 text-left hover:bg-hover">
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
  const needs = useNeeds().data ?? [];
  const review = useReviewBatch().data ?? [];
  const runs = useRuns().data ?? [];
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
    const bridge = window.tuskerShell?.openFull;
    if (bridge) {
      bridge("/");
      return;
    }
    void navigate({ to: "/" });
  };
  useEffect(() => {
    const shell = window.tuskerShell;
    if (!shell) return;
    const onNavigate = (path: string) => { void navigate({ to: path as "/" }); return true; };
    shell.onNavigate = onNavigate;
    return () => { if (window.tuskerShell?.onNavigate === onNavigate) delete window.tuskerShell.onNavigate; };
  }, [navigate]);
  const attentionRows = useMemo(() => needs.map((item: NeedItem) => ({
    id: item.taskId, title: item.taskTitle, chip: item.kind === "failed" ? "failed" : age(item.since), path: taskPath(item.projectId, item.taskId), tone: item.kind === "failed" ? "failed" as const : "attention" as const,
  })), [needs]);
  const reviewRows = useMemo(() => review.map((task: TaskCapsule) => ({ id: task.id, title: task.title, chip: age(task.updatedAt), path: taskPath(task.projectId || "", task.id), tone: "attention" as const })), [review]);
  const runningRows = useMemo(() => runs.filter((run: RunSummary) => run.outcome === "running" || run.processRunning).map((run) => ({ id: run.taskId, title: run.taskTitle, chip: run.lane, path: taskPath(run.projectId, run.taskId), tone: "running" as const })), [runs]);
  const failureRows = useMemo(() => runs.filter((run) => run.outcome === "failed" || run.leaseStateRaw === "parked_no_progress").map((run) => ({ id: run.taskId, title: run.taskTitle, chip: "failed", path: taskPath(run.projectId, run.taskId), tone: "failed" as const })), [runs]);
  const allEmpty = attentionRows.length + reviewRows.length + runningRows.length + failureRows.length === 0;
  return (
    <div className="h-full w-full overflow-y-auto overflow-x-hidden bg-surface">
      <header className="sticky top-0 z-10 border-b border-line bg-surface/95 px-3 py-3 backdrop-blur">
        <div className="flex items-center justify-between gap-3">
          <h1 className="min-w-0 truncate font-serif text-[19px] font-semibold">Tusker triage</h1>
          <div className="flex shrink-0 items-center gap-2">
            <span className="text-[11px] text-muted">live</span>
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
      </header>
      {allEmpty ? <div className="flex h-[calc(100%-58px)] items-center justify-center px-6 text-center font-serif text-[18px] text-muted">Nothing needs you</div> : <div className="pb-3"><Section title="Needs attention" rows={attentionRows} onOpen={open} /><Section title="Review queue" rows={reviewRows} onOpen={open} /><Section title="Running now" rows={runningRows} onOpen={open} /><Section title="Recent failures" rows={failureRows} onOpen={open} /></div>}
    </div>
  );
}
