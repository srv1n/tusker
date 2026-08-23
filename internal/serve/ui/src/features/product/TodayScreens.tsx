import type { ReactNode } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowRight, Waves } from "lucide-react";
import { useNeeds, useProjects, useRuns, useTasks, useWaves } from "@/lib/queries";
import type { NeedItem, ProjectSummary, RunSummary, TaskCapsule, WaveSummary } from "@/types/domain";
import {
  phaseTone,
  ProductEmpty,
  ProductLoading,
  ProductPage,
  ProductRow,
  ProductSection,
  ProductStatus,
  ProductUnavailable,
} from "./shared";

const ACTIVE_RUN_STATES = new Set(["claimed", "starting", "running"]);

function plural(count: number, noun: string) {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

function activeRuns(runs: RunSummary[]) {
  return runs.filter(
    (run) =>
      !run.terminal &&
      run.liveness === "fresh" &&
      ACTIVE_RUN_STATES.has((run.leaseStateRaw ?? run.leaseState ?? "").toLowerCase()),
  );
}

function activeWaves(waves: WaveSummary[], running: RunSummary[]) {
  const runningTaskIds = new Set(running.map((run) => run.taskId));
  return waves.filter((wave) => {
    if (wave.landedAt || wave.authorization.state !== "armed" || wave.authorization.stale) return false;
    return wave.memberIds.some((taskId) => runningTaskIds.has(taskId)) ||
      wave.members.some((member) => runningTaskIds.has(member.id));
  });
}

function deliveredWaves(waves: WaveSummary[]) {
  return waves
    .filter((wave) => wave.landedAt)
    .sort((left, right) => Date.parse(right.landedAt ?? "") - Date.parse(left.landedAt ?? ""));
}

function taskHref(projectId: string, taskId: string) {
  return `/p/${encodeURIComponent(projectId)}/docs?path=${encodeURIComponent(taskId)}`;
}

function projectHref(projectId: string) {
  return `/p/${encodeURIComponent(projectId)}/`;
}

function timeLabel(value: string | null | undefined) {
  if (!value) return "Delivered";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Delivered";
  const diff = Math.max(0, Date.now() - date.getTime());
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 60) return `Delivered ${Math.max(1, minutes)}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `Delivered ${hours}h ago`;
  return `Delivered ${Math.floor(hours / 24)}d ago`;
}

function ProjectLink({ projectId, children }: { projectId: string; children: ReactNode }) {
  return (
    <a className="text-[12px] font-medium text-info hover:text-ink" href={projectHref(projectId)}>
      {children}
    </a>
  );
}

/**
 * Cross-project briefing. The only global data source is the project rollup:
 * no omitted project id is interpreted as an aggregate runs/tasks/waves API.
 */
export function GlobalToday() {
  const projectsQ = useProjects();
  const projects = projectsQ.data ?? [];
  const attentionProjects = projects.filter((project) => project.needsCount > 0);
  const workingProjects = projects.filter((project) => project.activeRuns > 0);
  const attentionCount = attentionProjects.reduce((total, project) => total + project.needsCount, 0);

  return (
    <ProductPage
      title="Today"
      intro={
        projectsQ.isLoading
          ? "Loading the registered project rollups…"
          : attentionCount > 0
            ? `${plural(attentionCount, "item")} need your attention across ${plural(attentionProjects.length, "project")}.`
            : `Nothing needs you across ${plural(projects.length, "registered project")}.`
      }
      wide
    >
      {projectsQ.isError ? (
        <ProductUnavailable>Project rollups could not be read. Nothing below is inferred from partial data.</ProductUnavailable>
      ) : projectsQ.isLoading ? (
        <ProductLoading rows={4} />
      ) : projects.length === 0 ? (
        <ProductEmpty
          title="Connect your first project"
          detail="Once a project is registered, Today will show only its available project rollup. Registration does not enable automation."
        />
      ) : (
        <>
          {attentionProjects.length > 0 && (
            <ProductSection title="Needs your attention" count={attentionCount}>
              <div>
                {attentionProjects.map((project) => (
                  <ProjectRollupRow key={project.id} project={project} kind="attention" />
                ))}
              </div>
            </ProductSection>
          )}

          {attentionProjects.length === 0 && (
            <div className="mb-12 border-y border-line py-7">
              <div className="text-[21px] font-semibold tracking-[-0.025em] text-ink">Nothing needs you.</div>
              <p className="mt-1 text-[14px] text-muted">
                Tusker will interrupt only when a project records a human-owned need.
              </p>
            </div>
          )}

          <ProductSection title="Working across projects" count={workingProjects.length}>
            {workingProjects.length > 0 ? (
              <div>
                {workingProjects.map((project) => (
                  <ProjectRollupRow key={project.id} project={project} kind="working" />
                ))}
              </div>
            ) : (
              <ProductEmpty
                title="No project reports active work"
                detail="This summary uses each project's active-run rollup; delivery history is intentionally not guessed from it."
              />
            )}
          </ProductSection>
        </>
      )}
    </ProductPage>
  );
}

function ProjectRollupRow({ project, kind }: { project: ProjectSummary; kind: "attention" | "working" }) {
  const isAttention = kind === "attention";
  const title = isAttention
    ? `${plural(project.needsCount, "item")} need your attention`
    : `${plural(project.activeRuns, "active run")}`;
  const health = project.health.trim() || "status unavailable";

  return (
    <ProductRow
      meta={project.name}
      title={title}
      detail={isAttention ? "Open the project to see the exact decision or action." : `Project health: ${health}.`}
      status={
        <ProductStatus tone={isAttention ? "warn" : "info"}>
          {isAttention ? "Needs you" : "Working"}
        </ProductStatus>
      }
      action={<ProjectLink projectId={project.id}>Open project <ArrowRight className="inline" size={13} /></ProjectLink>}
    />
  );
}

/** Project-scoped release-manager briefing. Every counter derives from one scoped query. */
export function ProjectToday() {
  const { projectId } = useParams({ strict: false }) as { projectId: string };
  const projectsQ = useProjects();
  const needsQ = useNeeds(projectId);
  const wavesQ = useWaves(projectId);
  const tasksQ = useTasks(projectId);
  const runsQ = useRuns(projectId);

  const project = projectsQ.data?.find((item) => item.id === projectId);
  const needs = needsQ.data ?? [];
  const waves = wavesQ.data ?? [];
  const tasks = tasksQ.data ?? [];
  const runs = runsQ.data ?? [];
  const running = activeRuns(runs);
  const movingWaves = activeWaves(waves, running);
  const landed = deliveredWaves(waves).slice(0, 3);
  const isLoading = needsQ.isLoading || wavesQ.isLoading || tasksQ.isLoading || runsQ.isLoading;

  if (projectsQ.isError) {
    return <ProductPage title="Today" eyebrow={projectId}><ProductUnavailable>Project registration could not be read.</ProductUnavailable></ProductPage>;
  }

  if (projectsQ.data && !project) {
    return (
      <ProductPage title="Project not found" eyebrow={projectId}>
        <ProductEmpty title="This project is not registered" detail="Choose a registered project before viewing its delivery briefing." />
      </ProductPage>
    );
  }

  return (
    <ProductPage
      title={project?.name ?? projectId}
      eyebrow="Today"
      intro={projectSummary({ needs, waves, tasks, running, isLoading })}
      actions={
        <a
          className="inline-flex min-h-9 items-center gap-2 rounded-[3px] border border-line bg-raised px-3 text-[12px] font-semibold text-ink hover:border-ink hover:bg-hover"
          href={`/p/${encodeURIComponent(projectId)}/waves`}
        >
          <Waves size={14} />
          Deliveries
        </a>
      }
    >
      {isLoading && <ProductLoading rows={3} />}

      {!isLoading && (
        <>
          <TodayAttention projectId={projectId} needs={needs} error={needsQ.isError} />
          <TodayWorking
            projectId={projectId}
            waves={movingWaves}
            tasks={tasks}
            running={running}
            wavesError={wavesQ.isError}
            tasksError={tasksQ.isError}
            runsError={runsQ.isError}
          />
          <TodayDelivered projectId={projectId} waves={landed} error={wavesQ.isError} />
        </>
      )}
    </ProductPage>
  );
}

function projectSummary({
  needs,
  waves,
  tasks,
  running,
  isLoading,
}: {
  needs: NeedItem[];
  waves: WaveSummary[];
  tasks: TaskCapsule[];
  running: RunSummary[];
  isLoading: boolean;
}) {
  if (isLoading) return "Reading the current project projection…";
  if (needs.length > 0) return `${plural(needs.length, "item")} need your attention. The rest of the project remains visible below.`;
  if (running.length > 0) return `${plural(running.length, "active run")} across ${plural(activeWaves(waves, running).length, "open wave")} and ${plural(tasks.length, "task")}.`;
  return `${plural(activeWaves(waves, running).length, "open wave")} and ${plural(tasks.length, "task")} are currently recorded. Nothing needs you.`;
}

function TodayAttention({ projectId, needs, error }: { projectId: string; needs: NeedItem[]; error: boolean }) {
  if (error) {
    return (
      <ProductSection title="Needs attention">
        <ProductUnavailable>Human-action items could not be read. Tusker is not treating a missing response as an empty queue.</ProductUnavailable>
      </ProductSection>
    );
  }

  if (needs.length === 0) return null;

  return (
    <ProductSection title="Needs attention" count={needs.length}>
      {needs.map((need) => (
        <ProductRow
          key={need.id}
          meta={`${need.kind.replaceAll("-", " ")} · ${need.projectName}`}
          title={need.taskTitle}
          detail={needDetail(need)}
          status={<ProductStatus tone="warn">Needs your action</ProductStatus>}
          action={<a className="text-[12px] font-medium text-info hover:text-ink" href={taskHref(projectId, need.taskId)}>Open task</a>}
        />
      ))}
    </ProductSection>
  );
}

function needDetail(need: NeedItem) {
  if (need.humanAction) return need.humanAction.action;
  switch (need.kind) {
    case "clarify": return need.question;
    case "provision": return need.ask;
    case "approve-spec": return `Approve ${need.specTitle}.`;
    case "review": return "Review the available acceptance proof.";
    case "failed": return need.lastError || "The latest attempt failed.";
  }
}

function TodayWorking({
  projectId,
  waves,
  tasks,
  running,
  wavesError,
  tasksError,
  runsError,
}: {
  projectId: string;
  waves: WaveSummary[];
  tasks: TaskCapsule[];
  running: RunSummary[];
  wavesError: boolean;
  tasksError: boolean;
  runsError: boolean;
}) {
  return (
    <ProductSection title="Working now" count={waves.length}>
      {(wavesError || tasksError || runsError) && (
        <ProductUnavailable>
          {wavesError ? "Wave" : tasksError ? "Task" : "Run"} data could not be read, so this briefing does not calculate a complete working total.
        </ProductUnavailable>
      )}
      {!wavesError && waves.length === 0 && !tasksError && !runsError && (
        <ProductEmpty
          title="No delivery is running"
          detail={`${plural(tasks.length, "task")} and ${plural(running.length, "active run")} are currently recorded for this project.`}
        />
      )}
      {!wavesError && waves.length > 0 && (
        <div>
          {waves.map((wave) => {
            const done = wave.counts.done ?? 0;
            const total = wave.memberIds.length;
            const current = wave.status || wave.authorization.state;
            return (
              <ProductRow
                key={wave.id}
                meta={wave.id}
                title={wave.title}
                detail={wave.brief.outcome.summary || `${done} of ${total} tasks are done.`}
                status={<ProductStatus tone={phaseTone(current)}>{current}</ProductStatus>}
                action={<Link className="text-[12px] font-medium text-info hover:text-ink" to="/p/$projectId/waves/$waveId" params={{ projectId, waveId: wave.id }}>Open wave</Link>}
              />
            );
          })}
        </div>
      )}
    </ProductSection>
  );
}

function TodayDelivered({ projectId, waves, error }: { projectId: string; waves: WaveSummary[]; error: boolean }) {
  if (error) return null;
  if (waves.length === 0) return null;

  return (
    <ProductSection title="Recently delivered" count={waves.length}>
      {waves.map((wave) => (
        <ProductRow
          key={wave.id}
          meta={wave.id}
          title={wave.title}
          detail={timeLabel(wave.landedAt)}
          status={<ProductStatus tone="pass">Delivered</ProductStatus>}
          action={<Link className="text-[12px] font-medium text-info hover:text-ink" to="/p/$projectId/waves/$waveId" params={{ projectId, waveId: wave.id }}>View delivery</Link>}
        />
      ))}
      <p className="mt-3 text-[12px] text-muted">
        <Link className="text-info hover:text-ink" to="/p/$projectId/waves" params={{ projectId }}>View delivery history</Link>
      </p>
    </ProductSection>
  );
}
