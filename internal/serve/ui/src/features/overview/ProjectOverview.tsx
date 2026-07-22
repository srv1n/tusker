import type { ReactNode } from "react";
import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { Diamond, Flag, RefreshCw, Search, Settings } from "lucide-react";
import { cn } from "@/lib/cn";
import {
  useDaemon,
  useEpics,
  useNeeds,
  useProjects,
  useProjectRefresh,
  useRuns,
  useTasks,
} from "@/lib/queries";
import { EmptyState, ErrorState, QueryBoundary, Skeleton } from "@/components/ui/states";
import { PageScroll } from "@/components/ui/page";
import { Dot, Mono } from "@/components/ui/primitives";
import { Button } from "@/components/ui/controls";
import {
  priorityTone,
  riskTone,
  statusLabel,
  statusTone,
  tone,
  type Tone,
} from "@/components/ui/tone";
import type {
  EpicSummary,
  ProjectSummary,
  RunSummary,
  TaskCapsule,
  TaskStatus,
} from "@/types/domain";
import { openTaskSearch } from "@/features/search/TaskSearch";
import { NeedsList } from "@/features/needs/ProjectNeeds";
import { LivenessLegend, RunsBoard } from "@/features/runs/RunsBoard";

const route = getRouteApi("/p/$projectId/");

/** The action-relevant columns the overview mini-board surfaces. */
const BOARD_COLUMNS: TaskStatus[] = ["in_progress", "review", "blocked", "ready"];

/** Order active runs worst-first so a dead process rises to the top. */
const livenessRank: Record<RunSummary["liveness"], number> = { dead: 0, stale: 1, fresh: 2 };

/** tone → subtle outline for the tiny priority pill (full literals for the JIT). */
const toneBorder: Record<Tone, string> = {
  fail: "border-fail/50",
  pass: "border-pass/50",
  warn: "border-warn/50",
  info: "border-info/50",
  accent: "border-accent/50",
  muted: "border-line",
  neutral: "border-line",
};

// ----------------------------------------------------------------------------
// Screen
// ----------------------------------------------------------------------------

export function ProjectOverview() {
  const { projectId } = route.useParams();
  const projectsQ = useProjects();

  if (projectsQ.isError) {
    return <ErrorState error={projectsQ.error} onRetry={() => projectsQ.refetch()} />;
  }

  const project = projectsQ.data?.find((item) => item.id === projectId);
  if (projectsQ.data && !project) {
    return (
      <PageScroll>
        <EmptyState
          title="Project not found"
          hint={`No project “${projectId}” is registered with the daemon.`}
          action={
            <Link
              to="/"
              className="rounded-lg border border-line px-3 py-1.5 text-[12.5px] font-medium text-ink-soft transition-colors hover:bg-hover"
            >
              Back to all projects
            </Link>
          }
        />
      </PageScroll>
    );
  }

  const immediateProject: ProjectSummary = project ?? {
    id: projectId,
    name: projectId,
    repoRoot: "",
    vaultRoot: "",
    automationEnabled: false,
    health: "loading",
    needsCount: 0,
    activeRuns: 0,
    worstLiveness: null,
    daemonConnected: true,
  };

  return (
    <PageScroll>
      <OverviewContent project={immediateProject} projectId={projectId} />
    </PageScroll>
  );
}

function OverviewContent({ project, projectId }: { project: ProjectSummary; projectId: string }) {
  const navigate = useNavigate();
  const runsQ = useRuns(projectId);
  const needsQ = useNeeds(projectId);
  const epicsQ = useEpics(projectId);
  const tasksQ = useTasks(projectId);
  const daemonQ = useDaemon();
	const refresh = useProjectRefresh(projectId);

  const daemon = daemonQ.data;

  const runs = runsQ.data ?? [];
  const activeRuns = runs
    .filter((r) => !r.terminal && r.liveness === "fresh" && ["claimed", "starting", "running"].includes(r.leaseStateRaw ?? ""))
    .sort((a, b) => livenessRank[a.liveness] - livenessRank[b.liveness]);
  const tasks = tasksQ.data ?? [];

  // Prefer the live queue length, falling back to the project summary count.
  const needsCount = needsQ.data ? needsQ.data.length : project.needsCount;
  const inProgressCount = activeRuns.length;

  // A task whose latest run took more than one attempt reads as "rework".
  const reworkIds = new Set(runs.filter((r) => r.attemptCount > 1).map((r) => r.taskId));

  const statsLoading = runsQ.isLoading || needsQ.isLoading || tasksQ.isLoading;

  return (
    <div className="animate-rise">
      {/* Header */}
      <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div className="min-w-0">
          <div className="mb-1.5 flex flex-wrap items-center gap-2.5">
            <Diamond size={11} strokeWidth={1.5} className="text-fainter" />
            {/* Real identifier from /api/projects. The checkout path/branch aren't
                carried by ProjectSummary, so we show only what the payload has —
                no synthesized path/branch. */}
            <Mono className="text-[11px] text-faint">{project.id}</Mono>
            {daemon && (
              <>
                <span className="text-fainter">·</span>
                <span className="inline-flex items-center gap-1.5">
                  <Dot tone={daemon.connected ? "pass" : "fail"} pulse={daemon.connected} size={6} />
                  <Mono className={cn("text-[11px]", daemon.connected ? "text-faint" : "text-fail")}>
                    {daemon.connected ? daemon.addr : "daemon offline"}
                  </Mono>
                </span>
              </>
            )}
          </div>
          <h1 className="font-serif text-[32px] font-semibold leading-none tracking-[-0.02em] text-ink">
            {project.name}
          </h1>
        </div>
        <div className="flex flex-none items-center gap-2.5">
			<Button variant="default" onClick={openTaskSearch}>
				<Search size={14} strokeWidth={1.75} />
				Search tasks
			</Button>
			<Button
				variant="default"
				disabled={refresh.isPending}
				onClick={() => refresh.mutate()}
			>
				<RefreshCw size={14} strokeWidth={1.75} className={refresh.isPending ? "animate-spin" : undefined} />
				{refresh.isPending ? "Refreshing…" : "Refresh now"}
			</Button>
          <Button
            variant="default"
            onClick={() => navigate({ to: "/p/$projectId/settings", params: { projectId } })}
          >
            <Settings size={14} strokeWidth={1.75} />
            Details
          </Button>
          <Button
            variant={needsCount > 0 ? "danger" : "default"}
            onClick={() =>
              document
                .getElementById("overview-attention")
                ?.scrollIntoView({ behavior: "smooth", block: "start" })
            }
          >
            {needsCount > 0 ? `${needsCount} ${needsCount === 1 ? "need" : "needs"} you` : "Needs"}
          </Button>
        </div>
      </header>

      {/* At-a-glance counters (plain numbers, no charts) */}
      <div className="mb-6 grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-line bg-line sm:grid-cols-4">
        <StatCell
          label="Active runs"
          tone={activeRuns.length > 0 ? "info" : undefined}
          value={statsLoading ? <StatSkeleton /> : String(activeRuns.length)}
        />
        <StatCell
          label="Needs you"
          tone={needsCount > 0 ? "fail" : undefined}
          value={statsLoading ? <StatSkeleton /> : String(needsCount)}
        />
        <StatCell
          label="In progress"
          value={statsLoading ? <StatSkeleton /> : String(inProgressCount)}
        />
        <StatCell label="Runs" value={statsLoading ? <StatSkeleton /> : String(runs.length)} />
      </div>

      {/* Attention — absorbs the Needs-me page (attention first). */}
      <section id="overview-attention" className="mb-8 scroll-mt-6">
        <div className="mb-2.5 flex items-center justify-between">
          <SectionCaps>Needs you</SectionCaps>
          {needsCount > 0 && (
            <Mono className="text-[11px] font-semibold text-fail">{needsCount}</Mono>
          )}
        </div>
        <NeedsList projectId={projectId} />
      </section>

      {/* Epic rollups */}
      <div className="mb-5 flex flex-wrap items-center gap-2">
        <span className="mr-1 font-mono text-[9.5px] font-medium uppercase tracking-[0.12em] text-fainter">
          Epic
        </span>
        <QueryBoundary q={epicsQ} loading={<EpicPillsSkeleton />}>
          {(epics) => <EpicPills epics={epics} tasks={tasks} projectId={projectId} />}
        </QueryBoundary>
      </div>

      {/* Work board */}
      <QueryBoundary q={tasksQ} loading={<BoardSkeleton />}>
        {(allTasks) => <Board tasks={allTasks} reworkIds={reworkIds} projectId={projectId} />}
      </QueryBoundary>

      {/* Runs — absorbs the Runs page (runs below). */}
      <section className="mt-8">
        <div className="mb-2.5 flex items-center justify-between gap-4">
          <SectionCaps>Runs</SectionCaps>
          <LivenessLegend />
        </div>
        <RunsBoard projectId={projectId} />
      </section>
    </div>
  );
}

// ----------------------------------------------------------------------------
// Pieces
// ----------------------------------------------------------------------------

function StatCell({ label, value, tone: t }: { label: string; value: ReactNode; tone?: Tone }) {
  return (
    <div className="bg-raised px-4 py-3.5">
      <div className="font-mono text-[9px] uppercase tracking-[0.1em] text-faint">{label}</div>
      <div
        className={cn(
          "mt-1 font-serif text-[26px] font-semibold leading-none tracking-[-0.02em]",
          t ? tone[t].text : "text-ink",
        )}
      >
        {value}
      </div>
    </div>
  );
}

function SectionCaps({ children }: { children: ReactNode }) {
  return (
    <span className="font-mono text-[9.5px] font-medium uppercase tracking-[0.12em] text-faint">
      {children}
    </span>
  );
}

function EpicPills({
  epics,
  tasks,
  projectId,
}: {
  epics: EpicSummary[];
  tasks: TaskCapsule[];
  projectId: string;
}) {
  if (epics.length === 0) {
    return <span className="text-[12px] text-faint">No epics yet.</span>;
  }
  const activeEpicIds = new Set(
    tasks.filter((t) => t.status === "in_progress").map((t) => t.epicId),
  );
  return (
    <>
      {epics.map((epic) => {
        const total = Object.values(epic.counts).reduce((a, b) => a + b, 0);
        const active = activeEpicIds.has(epic.id);
        return (
          <Link
            key={epic.id}
            to="/p/$projectId/work"
            params={{ projectId }}
            title={epic.title}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[12px] transition-colors",
              active
                ? "border-accent/40 bg-accent-soft font-semibold text-accent"
                : "border-line bg-raised font-medium text-ink-soft hover:border-fainter hover:bg-hover",
            )}
          >
            {epic.id}
            <span className={cn("font-mono text-[10.5px]", active ? "text-accent/70" : "text-faint")}>
              {total}
            </span>
          </Link>
        );
      })}
    </>
  );
}

function Board({
  tasks,
  reworkIds,
  projectId,
}: {
  tasks: TaskCapsule[];
  reworkIds: Set<string>;
  projectId: string;
}) {
  if (tasks.length === 0) {
    return (
      <EmptyState
        title="No tasks yet"
        hint="Author epics and task contracts to populate the board."
        action={
          <Link
            to="/p/$projectId/work"
            params={{ projectId }}
            className="rounded-lg border border-line px-3 py-1.5 text-[12.5px] font-medium text-ink-soft transition-colors hover:bg-hover"
          >
            Open work
          </Link>
        }
      />
    );
  }
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {BOARD_COLUMNS.map((status) => {
        const colTasks = tasks.filter((t) => t.status === status);
        return (
          <div key={status} className="min-w-0">
            <div className="flex items-center gap-1.5 px-0.5 pb-2.5">
              <span className={cn("h-2 w-2 rounded-sm", tone[statusTone[status]].dot)} />
              <span className="text-[12px] font-semibold text-ink-soft">{statusLabel[status]}</span>
              <Mono className="text-[10.5px] text-faint">{colTasks.length}</Mono>
            </div>
            <div className="flex flex-col gap-2.5">
              {colTasks.map((task) => (
                <TaskMiniCard
                  key={task.id}
                  task={task}
                  rework={reworkIds.has(task.id)}
                  projectId={projectId}
                />
              ))}
              {colTasks.length === 0 && (
                <div className="rounded-lg border border-dashed border-line-soft px-2 py-3 text-center font-mono text-[10px] text-fainter">
                  none
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function TaskMiniCard({
  task,
  rework,
  projectId,
}: {
  task: TaskCapsule;
  rework: boolean;
  projectId: string;
}) {
  const priTone = priorityTone[task.priority];
  const rskTone = riskTone[task.risk];
  return (
    <Link
      to="/p/$projectId/docs"
      params={{ projectId }}
      search={{ path: task.id }}
      className="block rounded-lg border border-line bg-raised px-3 py-2.5 transition-colors hover:border-fainter"
    >
      <div className="mb-1.5 flex items-center justify-between gap-2">
        <Mono className="text-[9.5px] text-faint">{task.id}</Mono>
        <span className="flex items-center gap-1.5">
          {rework && (
            <span className="rounded bg-warn-soft px-1.5 py-px font-mono text-[8.5px] font-semibold uppercase leading-none text-warn">
              rework
            </span>
          )}
          {task.hasGate && (
            <Flag
              size={11}
              strokeWidth={2}
              className={task.readiness === "blocked_gate" ? "text-fail" : "text-warn"}
            />
          )}
        </span>
      </div>
      <div className="mb-2 text-[13px] font-medium leading-snug text-ink">{task.title}</div>
      <div className="flex items-center gap-2">
        <span
          className={cn(
            "rounded border px-1 py-px font-mono text-[9px] font-semibold uppercase leading-none",
            tone[priTone].text,
            toneBorder[priTone],
          )}
        >
          {task.priority}
        </span>
        <span className={cn("inline-flex items-center gap-1 font-mono text-[9px]", tone[rskTone].text)}>
          <span className={cn("h-[5px] w-[5px] rounded-full", tone[rskTone].dot)} />
          {task.risk}
        </span>
      </div>
    </Link>
  );
}

// ----------------------------------------------------------------------------
// Loading skeletons
// ----------------------------------------------------------------------------

function StatSkeleton() {
  return <Skeleton className="h-[26px] w-12 rounded" />;
}

function EpicPillsSkeleton() {
  return (
    <>
      {Array.from({ length: 4 }).map((_, i) => (
        <Skeleton key={i} className="h-6 w-16 rounded-full" />
      ))}
    </>
  );
}

function BoardSkeleton() {
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {Array.from({ length: 4 }).map((_, i) => (
        <div key={i}>
          <Skeleton className="mb-2.5 h-3 w-20" />
          <div className="flex flex-col gap-2.5">
            <Skeleton className="h-[74px] w-full rounded-lg" />
            <Skeleton className="h-[74px] w-full rounded-lg" />
          </div>
        </div>
      ))}
    </div>
  );
}
