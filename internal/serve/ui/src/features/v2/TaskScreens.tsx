import { useMemo, useState } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { LayoutGrid, List, ShieldCheck } from "lucide-react";
import { QueryBoundary } from "@/components/ui/states";
import { useRun, useRuns, useTask, useTasks } from "@/lib/queries";
import { projectLiveExecution } from "@/features/work/work-utils";
import type { TaskCapsule } from "@/types/domain";
import {
  phaseTone,
  V2Button,
  V2Empty,
  V2Label,
  V2Loading,
  V2Page,
  V2Row,
  V2Section,
  V2Status,
  V2Unavailable,
} from "@/features/v2/shared";

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
  return (
    <Link
      to="/p/$projectId/tasks/$taskId"
      params={{ projectId, taskId: task.id }}
      className="block border-b border-line-soft px-1 py-4 transition-colors hover:bg-hover/70"
    >
      <div className="mb-2 flex items-center justify-between gap-3">
        <span className="font-mono text-[10px] text-faint">{task.id}</span>
        <V2Status tone={phaseTone(label)}>{label}</V2Status>
      </div>
      <div className="text-[14px] font-semibold leading-5 text-ink">{task.title}</div>
      <div className="mt-3 flex items-center gap-3 text-[11px] text-muted">
        <span>{task.epicId}</span>
        <span>{task.priority.toUpperCase()}</span>
        <span>{task.risk} risk</span>
      </div>
    </Link>
  );
}

export function TasksV2() {
  const { projectId } = useParams({ strict: false }) as { projectId: string };
  const tasks = useTasks(projectId);
  const runs = useRuns(projectId);
  const [view, setView] = useState<"board" | "list">("board");
  const [epic, setEpic] = useState("all");
  // Runtime ownership is projected into the board from fresh leases. It is
  // intentionally not written back as a durable task lifecycle transition.
  const all = useMemo(
    () => projectLiveExecution(tasks.data ?? [], runs.data ?? []),
    [tasks.data, runs.data],
  );
  const epics = useMemo(() => [...new Set(all.map((task) => task.epicId))].sort(), [all]);
  const filtered = epic === "all" ? all : all.filter((task) => task.epicId === epic);

  return (
    <V2Page
      title="Tasks"
      eyebrow={projectId}
      intro="The technical work behind each delivery. Runtime activity is projected from leases and attempts; it is never invented as a durable task state."
      wide
      actions={
        <div className="flex border border-line bg-raised">
          <V2Button tone={view === "board" ? "primary" : "text"} className="rounded-none border-0" onClick={() => setView("board")}>
            <LayoutGrid size={13} /> Board
          </V2Button>
          <V2Button tone={view === "list" ? "primary" : "text"} className="rounded-none border-0" onClick={() => setView("list")}>
            <List size={13} /> List
          </V2Button>
        </div>
      }
    >
      <div className="mb-8 flex flex-wrap items-center gap-2">
        <V2Label className="mr-2">Epic</V2Label>
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

      <QueryBoundary q={tasks} loading={<V2Loading rows={5} />}>
        {() => <QueryBoundary q={runs} loading={<V2Loading rows={5} />}>
          {() =>
          filtered.length === 0 ? (
            <V2Empty title="No tasks in this view" detail="Change the epic filter or author a task contract for this project." />
          ) : view === "list" ? (
            <div>
              {filtered
                .slice()
                .sort((a, b) => statusOrder.indexOf(a.status) - statusOrder.indexOf(b.status))
                .map((task) => (
                  <V2Row
                    key={task.id}
                    meta={`${task.id} · ${task.epicId}`}
                    title={task.title}
                    detail={`${task.priority.toUpperCase()} · ${task.risk} risk · ${task.readiness.replaceAll("_", " ")}`}
                    status={<V2Status tone={phaseTone(statusCopy(task))}>{statusCopy(task)}</V2Status>}
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
            <div className="grid gap-8 md:grid-cols-3 xl:grid-cols-6">
              {statusOrder.map((status) => {
                const column = filtered.filter((task) => task.status === status);
                if (column.length === 0) return null;
                return (
                  <section key={status} className="min-w-0">
                    <div className="mb-2 flex items-center justify-between border-b border-line pb-2">
                      <V2Label>{columnLabel(status)}</V2Label>
                      <span className="font-mono text-[10px] text-faint">{column.length}</span>
                    </div>
                    {column.map((task) => <TaskLink key={task.id} projectId={projectId} task={task} />)}
                  </section>
                );
              })}
            </div>
          )
          }
        </QueryBoundary>}
      </QueryBoundary>
    </V2Page>
  );
}

export function TaskDetailV2() {
  const { projectId, taskId } = useParams({ strict: false }) as { projectId: string; taskId: string };
  const task = useTask(taskId, projectId);
  const run = useRun(taskId, false, projectId);

  return (
    <QueryBoundary q={task} loading={<div className="h-full bg-surface p-12"><V2Loading rows={6} /></div>}>
      {(detail) => {
        const label = statusCopy(detail);
        return (
          <V2Page
            title={detail.title}
            eyebrow={`${projectId} / Tasks / ${detail.id}`}
            intro={detail.intent || "Task contract and objective proof."}
            actions={<V2Status tone={phaseTone(label)}>{label}</V2Status>}
          >
            {detail.humanAction && (
              <V2Unavailable>
                <strong>{detail.humanAction.title}.</strong> {detail.humanAction.action} Completion condition: {detail.humanAction.completionCondition}
              </V2Unavailable>
            )}

            <div className="mt-10 grid gap-12 lg:grid-cols-[minmax(0,1fr)_320px]">
              <div>
                <V2Section title="What we agreed" count={detail.acceptance.length}>
                  {detail.acceptance.map((row) => (
                    <div key={row.id} className="grid grid-cols-[52px_1fr_auto] items-start gap-4 border-b border-line-soft py-4">
                      <span className="font-mono text-[10px] text-faint">{row.id}</span>
                      <div className="text-[14px] leading-5 text-ink">{row.text}</div>
                      <V2Status tone={row.proof === "pass" ? "pass" : row.proof === "fail" ? "fail" : "neutral"}>{row.proof}</V2Status>
                    </div>
                  ))}
                </V2Section>

                <V2Section title="Proof" count={detail.verification.length}>
                  {detail.verification.map((row) => (
                    <div key={row.id} className="border-b border-line-soft py-4">
                      <div className="flex items-start gap-3">
                        <ShieldCheck size={16} className={row.result === "pass" ? "text-pass" : row.result === "fail" ? "text-fail" : "text-faint"} />
                        <code className="min-w-0 flex-1 break-words font-mono text-[11px] leading-5 text-ink-soft">{row.command}</code>
                        <V2Status tone={row.result === "pass" ? "pass" : row.result === "fail" ? "fail" : "neutral"}>{row.result}</V2Status>
                      </div>
                      {row.detail && <p className="ml-7 mt-2 text-[12px] leading-5 text-muted">{row.detail}</p>}
                    </div>
                  ))}
                </V2Section>
              </div>

              <aside>
                <V2Section title="Exact details">
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
                </V2Section>

                <V2Section title="Runtime">
                  {run.isLoading ? (
                    <V2Loading rows={2} />
                  ) : run.data ? (
                    <div className="space-y-3 text-[12px]">
                      <V2Status tone={phaseTone(run.data.outcome)}>{run.data.outcome.replaceAll("-", " ")}</V2Status>
                      <p className="leading-5 text-muted">
                        {run.data.runner} · {run.data.model} · attempt {run.data.attemptCount}
                      </p>
                      {run.data.workspacePath && <code className="block break-all font-mono text-[10px] leading-4 text-faint">{run.data.workspacePath}</code>}
                    </div>
                  ) : (
                    <p className="text-[12px] leading-5 text-muted">No runtime record exists for this task.</p>
                  )}
                </V2Section>
              </aside>
            </div>
          </V2Page>
        );
      }}
    </QueryBoundary>
  );
}
