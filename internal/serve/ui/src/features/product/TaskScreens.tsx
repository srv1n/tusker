import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { LayoutGrid, List, ShieldCheck } from "lucide-react";
import { cn } from "@/lib/cn";
import { QueryBoundary } from "@/components/ui/states";
import { useConfirm } from "@/components/ui/action-feedback";
import { api } from "@/lib/api";
import { useReviewBatch, useRun, useRuns, useTask, useTasks } from "@/lib/queries";
import { isBatchSelectable, projectLiveExecution } from "@/features/work/work-utils";
import { BatchBar, type BatchAction, type BatchItemResult, type BatchProgress, WaveReviewGroups } from "@/features/work/WaveReview";
import type { TaskCapsule } from "@/types/domain";
import {
  phaseTone,
  ProductButton,
  ProductEmpty,
  ProductLabel,
  ProductLoading,
  ProductPage,
  ProductRow,
  ProductSection,
  ProductStatus,
  ProductUnavailable,
} from "@/features/product/shared";

const statusOrder = ["in_progress", "review", "ready", "blocked", "backlog", "done"] as const;

function columnLabel(status: (typeof statusOrder)[number]) {
  return status === "in_progress" ? "Working now" : status.replaceAll("_", " ");
}

function statusCopy(task: TaskCapsule): string {
  if (task.liveRun) return "Building now";
  if (task.openGates?.length) return "Waiting for your decision";
  switch (task.status) {
    case "review":
      return "Checking the work";
    case "ready":
      return task.readiness === "ready" ? "Ready to start" : "Waiting for prerequisites";
    case "blocked":
      return "Blocked";
    case "done":
      return "Delivered";
    case "in_progress":
      return "Building";
    default:
      return "Planned";
  }
}

function TaskLink({ projectId, task }: { projectId: string; task: TaskCapsule }) {
  const label = statusCopy(task);
  const priorityTone =
    task.priority === "p0"
      ? "text-fail bg-fail-soft border-fail/30"
      : task.priority === "p1"
        ? "text-warn bg-warn-soft border-warn/30"
        : "text-muted bg-panel border-line";

  return (
    <Link
      to="/p/$projectId/tasks/$taskId"
      params={{ projectId, taskId: task.id }}
      className="group block rounded-xl border border-line bg-raised p-3.5 shadow-2xs transition-all hover:border-line hover:shadow-xs active:scale-[0.99]"
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="font-mono text-[10.5px] font-medium text-faint">{task.id}</span>
        <ProductStatus tone={phaseTone(label)}>{label}</ProductStatus>
      </div>
      <div className="text-[13.5px] font-semibold leading-snug text-ink group-hover:text-accent transition-colors">
        {task.title}
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px]">
        {task.epicId && (
          <span className="rounded bg-panel px-1.5 py-0.5 font-mono text-[10px] text-muted">
            {task.epicId}
          </span>
        )}
        <span className={cn("rounded border px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wider", priorityTone)}>
          {task.priority.toUpperCase()}
        </span>
        <span className="text-faint font-mono text-[10.5px]">{task.risk} risk</span>
      </div>
    </Link>
  );
}

export function Tasks() {
  const { projectId } = useParams({ strict: false }) as { projectId: string };
  const tasks = useTasks(projectId);
  const runs = useRuns(projectId);
  const reviewBatch = useReviewBatch(projectId);
  const qc = useQueryClient();
  const confirm = useConfirm();
  const [view, setView] = useState<"board" | "list">("board");
  const [epic, setEpic] = useState("all");
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set());
  const [progress, setProgress] = useState<BatchProgress | null>(null);
  const [results, setResults] = useState<{ action: BatchAction; items: BatchItemResult[] } | null>(null);
  const running = progress !== null;

  useEffect(() => {
    setSelectedIds(new Set());
    setResults(null);
  }, [projectId]);

  // Runtime ownership is projected into the board from fresh leases. It is
  // intentionally not written back as a durable task lifecycle transition.
  const all = useMemo(
    () => projectLiveExecution(tasks.data ?? [], runs.data ?? []),
    [tasks.data, runs.data],
  );
  const epics = useMemo(() => [...new Set(all.map((task) => task.epicId))].sort(), [all]);
  const filtered = epic === "all" ? all : all.filter((task) => task.epicId === epic);
  const selectedTasks = [...selectedIds]
    .map((id) => all.find((task) => task.id === id))
    .filter((task): task is TaskCapsule => task !== undefined && isBatchSelectable(task));
  const activeIds = selectedTasks.map((task) => task.id);
  const closeIds = selectedTasks.filter((task) => task.status === "review").map((task) => task.id);
  const landIds = selectedTasks.filter((task) => task.status === "done").map((task) => task.id);

  const runBatch = async (action: BatchAction, ids: string[]) => {
    setResults(null);
    setProgress({ action, done: 0, total: ids.length });
    const items: BatchItemResult[] = [];
    for (const id of ids) {
      try {
        const res = action === "close"
          ? await api.closeTask(id, {}, projectId)
          : await api.landTask(id, {}, projectId);
        items.push({ taskId: id, ok: res.ok && !res.refused, reason: res.reason });
      } catch (err) {
        items.push({ taskId: id, ok: false, reason: err instanceof Error ? err.message : "request failed" });
      }
      setProgress({ action, done: items.length, total: ids.length });
    }
    for (const key of ["projects", "needs", "tasks", "runs", "waves", "review", "gates", "evidence", "decisions", "feedback", "daemon"]) {
      void qc.invalidateQueries({ queryKey: [key] });
    }
    setSelectedIds(new Set());
    setProgress(null);
    setResults({ action, items });
  };

  return (
    <ProductPage
      title="Tasks"
      eyebrow={projectId}
      intro="The technical work behind each delivery. Runtime activity is projected from leases and attempts; it is never invented as a durable task state."
      wide
      actions={
        <div className="flex border border-line bg-raised">
          <ProductButton tone={view === "board" ? "primary" : "text"} className="rounded-none border-0" onClick={() => setView("board")}>
            <LayoutGrid size={13} /> Board
          </ProductButton>
          <ProductButton tone={view === "list" ? "primary" : "text"} className="rounded-none border-0" onClick={() => setView("list")}>
            <List size={13} /> List
          </ProductButton>
        </div>
      }
    >
      <div className="mb-8 flex flex-wrap items-center gap-2">
        <ProductLabel className="mr-2">Epic</ProductLabel>
        {["all", ...epics].map((value) => (
          <button
            key={value}
            type="button"
            onClick={() => setEpic(value)}
            className={`border px-3 py-1.5 text-[11px] font-medium ${
              epic === value ? "border-ink bg-ink text-surface" : "border-line bg-raised text-muted hover:border-ink"
            }`}
          >
            {value === "all" ? "All work" : value}
          </button>
        ))}
        <span className="ml-auto font-mono text-[10px] text-faint">{filtered.length} tasks</span>
      </div>

      <QueryBoundary q={reviewBatch} loading={null}>
        {(batch) => (
          <WaveReviewGroups
            batch={batch}
            disabled={running || tasks.isLoading || runs.isLoading}
            onSelectWave={(wave) => setSelectedIds(new Set(wave.members.filter(isBatchSelectable).map((task) => task.id)))}
          />
        )}
      </QueryBoundary>

      <QueryBoundary q={tasks} loading={<ProductLoading rows={5} />}>
        {() => <QueryBoundary q={runs} loading={<ProductLoading rows={5} />}>
          {() =>
          filtered.length === 0 ? (
            <ProductEmpty title="No tasks in this view" detail="Change the epic filter or author a task contract for this project." />
          ) : view === "list" ? (
            <div>
              {filtered
                .slice()
                .sort((a, b) => statusOrder.indexOf(a.status) - statusOrder.indexOf(b.status))
                .map((task) => (
                  <ProductRow
                    key={task.id}
                    meta={`${task.id} · ${task.epicId}`}
                    title={task.title}
                    detail={`${task.priority.toUpperCase()} · ${task.risk} risk · ${task.readiness.replaceAll("_", " ")}`}
                    status={<ProductStatus tone={phaseTone(statusCopy(task))}>{statusCopy(task)}</ProductStatus>}
                    action={
                      <Link
                        to="/p/$projectId/tasks/$taskId"
                        params={{ projectId, taskId: task.id }}
                        className="text-[12px] font-semibold text-ink underline underline-offset-4"
                      >
                        Open
                      </Link>
                    }
                  />
                ))}
            </div>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 items-start">
              {statusOrder.map((status) => {
                const column = filtered.filter((task) => task.status === status);
                return (
                  <section key={status} className="flex min-w-0 flex-col rounded-xl border border-line bg-panel/40 p-3 shadow-2xs">
                    <div className="mb-3 flex items-center justify-between pb-1">
                      <span className="text-[12.5px] font-semibold text-ink">{columnLabel(status)}</span>
                      <span className="rounded-full border border-line bg-surface px-2 py-0.5 font-mono text-[10.5px] font-semibold text-muted shadow-2xs">{column.length}</span>
                    </div>
                    <div className="flex flex-col gap-2.5">
                      {column.length === 0 ? (
                        <div className="rounded-lg border border-dashed border-line-soft bg-surface/50 py-7 text-center font-mono text-[11px] text-faint">
                          Empty
                        </div>
                      ) : (
                        column.map((task) => <TaskLink key={task.id} projectId={projectId} task={task} />)
                      )}
                    </div>
                  </section>
                );
              })}
            </div>
          )
          }
        </QueryBoundary>}
      </QueryBoundary>
      <BatchBar
        activeIds={activeIds}
        closeIds={closeIds}
        landIds={landIds}
        progress={progress}
        results={results}
        disabled={running}
        confirm={confirm}
        onRun={runBatch}
        onClearSelection={() => setSelectedIds(new Set())}
        onDismissResults={() => setResults(null)}
      />
    </ProductPage>
  );
}

export function TaskDetail() {
  const { projectId, taskId } = useParams({ strict: false }) as { projectId: string; taskId: string };
  const task = useTask(taskId, projectId);
  const run = useRun(taskId, false, projectId);

  return (
    <QueryBoundary q={task} loading={<div className="h-full bg-surface p-12"><ProductLoading rows={6} /></div>}>
      {(detail) => {
        const label = statusCopy(detail);
        return (
          <ProductPage
            title={detail.title}
            eyebrow={`${projectId} / Tasks / ${detail.id}`}
            intro={detail.intent || "Task contract and objective proof."}
            actions={<ProductStatus tone={phaseTone(label)}>{label}</ProductStatus>}
          >
            {detail.humanAction && (
              <ProductUnavailable>
                <strong>{detail.humanAction.title}.</strong> {detail.humanAction.action} Completion condition: {detail.humanAction.completionCondition}
              </ProductUnavailable>
            )}

            <div className="mt-10 grid gap-12 lg:grid-cols-[minmax(0,1fr)_320px]">
              <div>
                <ProductSection title="What we agreed" count={detail.acceptance.length}>
                  {detail.acceptance.map((row) => (
                    <div key={row.id} className="grid grid-cols-[52px_1fr_auto] items-start gap-4 border-b border-line-soft py-4">
                      <span className="font-mono text-[10px] text-faint">{row.id}</span>
                      <div className="text-[14px] leading-5 text-ink">{row.text}</div>
                      <ProductStatus tone={row.proof === "pass" ? "pass" : row.proof === "fail" ? "fail" : "neutral"}>{row.proof}</ProductStatus>
                    </div>
                  ))}
                </ProductSection>

                <ProductSection title="Proof" count={detail.verification.length}>
                  {detail.verification.map((row) => (
                    <div key={row.id} className="border-b border-line-soft py-4">
                      <div className="flex items-start gap-3">
                        <ShieldCheck size={16} className={row.result === "pass" ? "text-pass" : row.result === "fail" ? "text-fail" : "text-faint"} />
                        <code className="min-w-0 flex-1 break-words font-mono text-[11px] leading-5 text-ink-soft">{row.command}</code>
                        <ProductStatus tone={row.result === "pass" ? "pass" : row.result === "fail" ? "fail" : "neutral"}>{row.result}</ProductStatus>
                      </div>
                      {row.detail && <p className="ml-7 mt-2 text-[12px] leading-5 text-muted">{row.detail}</p>}
                    </div>
                  ))}
                </ProductSection>
              </div>

              <aside>
                <ProductSection title="Exact details">
                  <dl className="space-y-4 text-[12px]">
                    {[
                      ["Epic", `${detail.epicId} · ${detail.epicTitle}`],
                      ["Priority", detail.priority.toUpperCase()],
                      ["Risk", detail.risk],
                      ["Readiness", detail.readiness.replaceAll("_", " ")],
                      ["Updated", detail.updatedAt],
                    ].map(([name, value]) => (
                      <div key={name} className="border-b border-line-soft pb-3">
                        <dt className="mb-1 font-mono text-[9px] uppercase tracking-[0.12em] text-faint">{name}</dt>
                        <dd className="text-ink">{value}</dd>
                      </div>
                    ))}
                  </dl>
                </ProductSection>

                <ProductSection title="Runtime">
                  {run.isLoading ? (
                    <ProductLoading rows={2} />
                  ) : run.data ? (
                    <div className="space-y-3 text-[12px]">
                      <ProductStatus tone={phaseTone(run.data.outcome)}>{run.data.outcome.replaceAll("-", " ")}</ProductStatus>
                      <p className="leading-5 text-muted">
                        {run.data.runner} · {run.data.model} · attempt {run.data.attemptCount}
                      </p>
                      {run.data.workspacePath && <code className="block break-all font-mono text-[10px] leading-4 text-faint">{run.data.workspacePath}</code>}
                    </div>
                  ) : (
                    <p className="text-[12px] leading-5 text-muted">No runtime record exists for this task.</p>
                  )}
                </ProductSection>
              </aside>
            </div>
          </ProductPage>
        );
      }}
    </QueryBoundary>
  );
}
