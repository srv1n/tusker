import { useEffect, useMemo, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useRouterState } from "@tanstack/react-router";
import { MoreHorizontal } from "lucide-react";
import { cn } from "@/lib/cn";
import { api } from "@/lib/api";
import { qk, useProjects, useRun, useRuns, useTask, useTasks, useWaves, useWave, useWaveList, useWaveReview } from "@/lib/queries";
import { WaveCallout, WaveMeta, WavePrimaryAction, WaveReviewDetail } from "@/features/workbench/integration/WaveAuthority";
import { TaskBoard } from "../board";
import { WaveFlow, type DependencyFact, type FlowViewport } from "../flow";
import { crossWaveWaitSummary } from "../flow/flowGraph";
import { TaskInspector } from "../inspector/TaskInspector";
import { WaveList } from "../overview/WaveList";
import { WaveResults } from "../results/WaveResults";
import { getWorkspaceViewState, NAVIGATION_CHANGED_EVENT, readNavigationState, updateWorkspaceViewState, writeNavigationState, type StorageLike } from "../navigation";
import { canShowWaveResults, settleEnteredWaveView, waveSummaryWithReview, type WorkView } from "./integrationModel";
import { projectContainsCheckout, type ProjectSummary, type WaveReviewMember } from "@/types/domain";

type Params = { projectId?: string; waveId?: string };

function useProjectId() {
  return (useParams({ strict: false }) as Params).projectId ?? "";
}

function navigationStorage(): StorageLike | null {
  try {
    return typeof window !== "undefined" && window.localStorage ? window.localStorage : null;
  } catch {
    return null;
  }
}

function savedOverview(projectId: string, projectIds: string[]): { query: string } {
  const waves = getWorkspaceViewState(readNavigationState(navigationStorage(), projectIds), projectId).waves;
  return { query: waves?.overviewQuery ?? "" };
}

function persistOverview(projectId: string, projectIds: string[], patch: { overviewQuery?: string }) {
  if (!projectIds.includes(projectId)) return;
  const storage = navigationStorage();
  writeNavigationState(storage, updateWorkspaceViewState(readNavigationState(storage, projectIds), projectId, { waves: patch }));
  if (typeof window !== "undefined") window.dispatchEvent(new Event(NAVIGATION_CHANGED_EVENT));
}

/** A checkout route belongs to its registered parent project in navigation. */
export function projectContextName(projects: ProjectSummary[] | undefined, projectId: string): string {
  return projects?.find((project) => projectContainsCheckout(project, projectId))?.name ?? projectId;
}

/** Every Work screen: one 52px toolbar row, then the screen body. */
function Shell({ left, right, children }: { left: React.ReactNode; right?: React.ReactNode; children: React.ReactNode }) {
  return <main data-wux-ready="true" className="flex h-full min-h-0 flex-col overflow-hidden bg-surface text-ink">
    <header className="flex h-[52px] flex-none items-center gap-3 border-b border-line px-4 sm:px-5">
      {left}
      {right ? <div className="ml-auto flex min-w-0 items-center gap-2">{right}</div> : null}
    </header>
    {children}
  </main>;
}

function Reading({ children }: { children: React.ReactNode }) {
  return <div className="min-h-0 flex-1 overflow-y-auto"><div className="mx-auto w-full max-w-[1180px] px-4 py-5 sm:px-6">{children}</div></div>;
}

const segment = (active: boolean) => cn("rounded px-2.5 py-1 text-[12px] font-medium", active ? "bg-raised text-ink shadow-2xs" : "text-muted hover:text-ink");

/** Waves and Board are two views of one place. */
function WorkTitle({ projectId, current }: { projectId: string; current: "waves" | "board" }) {
  return <>
    <h1 className="text-[15px] font-semibold">Work</h1>
    <nav aria-label="Work views" className="flex rounded-md border border-line bg-panel p-0.5">
      <Link to="/p/$projectId/waves" params={{ projectId }} aria-current={current === "waves" ? "page" : undefined} className={segment(current === "waves")}>Waves</Link>
      <Link to="/p/$projectId/tasks" params={{ projectId }} aria-current={current === "board" ? "page" : undefined} className={segment(current === "board")}>Board</Link>
    </nav>
  </>;
}

export function WorkOverview() {
  const projectId = useProjectId();
  const navigate = useNavigate();
  const waves = useWaveList(projectId);
  const projects = useProjects();
  const projectIds = useMemo(
    () => [...new Set((projects.data ?? []).flatMap((project) => [project.id, ...(project.checkouts ?? []).map((checkout) => checkout.id)]))],
    [projects.data],
  );
  const [query, setQuery] = useState(() => savedOverview(projectId, [projectId]).query);
  useEffect(() => {
    const saved = savedOverview(projectId, projectIds.length > 0 ? projectIds : [projectId]);
    setQuery(saved.query);
  }, [projectId]);
  const updateQuery = (value: string) => {
    setQuery(value);
    persistOverview(projectId, projectIds, { overviewQuery: value });
  };
  return <Shell
    left={<WorkTitle projectId={projectId} current="waves" />}
    right={<input type="search" value={query} onChange={(event) => updateQuery(event.target.value)} placeholder="Filter waves" aria-label="Filter waves by ID, title, or description" className="h-8 w-48 min-w-0 rounded-md border border-line bg-raised px-2.5 text-[12.5px] text-ink placeholder:text-faint" />}
  ><Reading><WaveList
    waves={waves.data ?? []}
    backgroundWorkEnabled={projects.data?.find((project) => projectContainsCheckout(project, projectId))?.automationEnabled}
    query={query}
    onOpenWave={(waveId) => navigate({ to: "/p/$projectId/waves/$waveId", params: { projectId, waveId } })}
    loading={waves.isPending}
    error={waves.error instanceof Error ? waves.error.message : undefined}
  /></Reading></Shell>;
}

function InspectorHost({ selected, onClose, reviewMember }: { selected: string | null; onClose: () => void; reviewMember?: WaveReviewMember }) {
  return selected ? <LoadedInspector selected={selected} onClose={onClose} reviewMember={reviewMember} /> : null;
}

function LoadedInspector({ selected, onClose, reviewMember }: { selected: string; onClose: () => void; reviewMember?: WaveReviewMember }) {
  const projectId = useProjectId();
  const navigate = useNavigate();
  const task = useTask(selected, projectId);
  const run = useRun(selected, false, projectId);
  return <TaskInspector selectedTaskId={selected} task={task.data ?? null} run={run.data ?? null} loading={task.isPending} error={task.error instanceof Error ? task.error.message : undefined} reviewMember={reviewMember} onClose={onClose} onOpenTask={(taskId) => navigate({ to: "/p/$projectId/tasks/$taskId", params: { projectId, taskId } })} />;
}

export function WorkBoard() {
  const projectId = useProjectId();
  const location = useRouterState({ select: (state) => state.location });
  const tasks = useTasks(projectId);
  const runs = useRuns(projectId);
  const waves = useWaves(projectId);
  const [mode, setMode] = useState<"board" | "list">("board");
  const [selected, setSelected] = useState<string | null>(null);
  const unassigned = new URLSearchParams(location.searchStr).get("scope") === "unassigned";
  const unassignedIds = useMemo(() => {
    const membered = new Set((waves.data ?? []).flatMap((wave) => [...wave.memberIds, ...wave.members.map((member) => member.id)]));
    return (tasks.data ?? []).filter((task) => !membered.has(task.id)).map((task) => task.id);
  }, [tasks.data, waves.data]);
  const modeToggle = <div className="flex rounded-md border border-line bg-panel p-0.5" aria-label="Task view">{(["board", "list"] as const).map((value) => <button key={value} type="button" aria-pressed={mode === value} onClick={() => setMode(value)} className={segment(mode === value)}>{value === "board" ? "Board" : "List"}</button>)}</div>;
  const body = unassigned && waves.isPending
    ? <p role="status" className="p-5 text-[13px] text-muted">Loading wave membership…</p>
    : unassigned && waves.error
      ? <p role="alert" className="p-5 text-[13px] text-fail">Unassigned tasks are unavailable until wave membership can be loaded.</p>
      : <TaskBoard tasks={tasks.data ?? []} runs={runs.data ?? []} mode={mode} onModeChange={setMode} onSelectTask={setSelected} taskIds={unassigned ? unassignedIds : undefined} selectedTags={[]} onSelectedTagsChange={() => {}} tagsAvailable={false} />;
  return <Shell left={<WorkTitle projectId={projectId} current="board" />} right={modeToggle}>{body}<InspectorHost selected={selected} onClose={() => setSelected(null)} /></Shell>;
}

export function WorkWave() {
  const { waveId = "" } = useParams({ strict: false }) as Params;
  const projectId = useProjectId();
  const location = useRouterState({ select: (state) => state.location });
  const waveQuery = useWave(projectId, waveId);
  const runs = useRuns(projectId);
  const wave = waveQuery.data;
  const review = useWaveReview(waveId, projectId);
  // Member details use the canonical task key so stream events and run/wave
  // mutations invalidate them exactly like the inspector's useTask read.
  const details = useQueries({ queries: (wave?.memberIds ?? []).map((id) => ({ queryKey: qk.task(id, projectId), queryFn: () => api.task(id, projectId) })) });
  const tasks = details.flatMap((query) => query.data ? [query.data] : []);
  const requested = new URLSearchParams(location.searchStr).get("view") ?? undefined;
  const [chosenView, setChosenView] = useState<WorkView | null>(null);
  const [enteredView, setEnteredView] = useState<WorkView | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [viewport, setViewport] = useState<FlowViewport>({ x: 32, y: 32, scale: 1 });
  useEffect(() => {
    setChosenView(null);
    setEnteredView(null);
    setSelected(null);
    setViewport({ x: 32, y: 32, scale: 1 });
  }, [projectId, waveId, requested]);
  const currentWave = wave ? waveSummaryWithReview(wave, review.data, review.error) : undefined;
  const dependencyFacts = useMemo<Record<string, DependencyFact> | undefined>(() => {
    if (review.error) return undefined;
    const externals = review.data?.externalDependencies;
    if (!externals) return undefined;
    return Object.fromEntries(externals.map((fact) => [fact.taskId, {
      kind: fact.classification,
      title: fact.title,
      status: fact.status,
      readiness: fact.readiness,
      waveId: fact.waveId,
      waveTitle: fact.waveTitle,
    } satisfies DependencyFact]));
  }, [review.data, review.error]);
  const waveAuthorization = review.error ? undefined : review.data?.authorization;
  const waveStartEnabled = !review.error && (review.data?.controls ?? []).some((control) => control.action === "wave start" && control.enabled);
  useEffect(() => {
    if (currentWave) setEnteredView((current) => settleEnteredWaveView(current, currentWave, review.isPending, requested));
  }, [currentWave?.status, currentWave?.landedAt, requested, review.isPending]);
  // A completed review may arrive after the summary. It owns an implicit
  // entry destination; an explicit tab or deep link stays fixed through polls.
  const requestedView = requested === "work" || requested === "flow" || requested === "results" ? requested : null;
  const view = chosenView ?? enteredView ?? requestedView ?? "flow";
  const crumb = <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1.5 text-[13px]"><Link to="/p/$projectId/waves" params={{ projectId }} className="text-muted hover:text-ink">Work</Link><span className="text-faint">/</span><span className="truncate font-mono text-[12px] text-ink">{waveId}</span></nav>;
  if (waveQuery.isPending || !currentWave) return <Shell left={crumb}><p role={waveQuery.error ? "alert" : "status"} className="p-5 text-[13px] text-muted">{waveQuery.error ? "Wave unavailable." : "Loading wave…"}</p></Shell>;
  const goal = currentWave.expectedOutcome ?? currentWave.brief.expectedOutcome;
  const waitSummary = crossWaveWaitSummary(Object.values(dependencyFacts ?? {}), waveAuthorization ?? "inert", waveStartEnabled);
  const views = [["flow", "Graph"], ["work", "Tasks"], ["results", "Results"]] as const;
  return <Shell left={crumb} right={<>
    <WavePrimaryAction projectId={projectId} waveId={currentWave.id} />
    <details className="relative">
      <summary aria-label="More wave actions" className="flex h-8 w-8 cursor-pointer list-none items-center justify-center rounded-md text-muted hover:bg-hover hover:text-ink"><MoreHorizontal size={16} aria-hidden="true" /></summary>
      <div className="absolute right-0 top-9 z-30 w-44 rounded-md border border-line bg-raised py-1 text-[12.5px] shadow-md">
        <button type="button" className="block w-full px-3 py-1.5 text-left hover:bg-hover" onClick={() => void navigator.clipboard?.writeText(currentWave.id)}>Copy wave ID</button>
        <Link to="/p/$projectId/settings" params={{ projectId }} className="block px-3 py-1.5 hover:bg-hover">Project settings</Link>
      </div>
    </details>
  </>}>
    <div className="flex flex-none flex-wrap items-end justify-between gap-3 px-5 pb-3 pt-4">
      <div className="min-w-0 flex-1">
        <h2 className="truncate text-[22px] font-semibold leading-tight">{currentWave.title}</h2>
        {goal ? <p className="mt-1 truncate text-[13px] text-muted" title={goal}>{goal}</p> : null}
        <WaveMeta projectId={projectId} waveId={currentWave.id} />
      </div>
      <nav aria-label="Wave views" className="flex rounded-md border border-line bg-panel p-0.5">{views.map(([tab, label]) => <button key={tab} type="button" aria-pressed={view === tab} onClick={() => setChosenView(tab)} className={segment(view === tab)}>{label}</button>)}</nav>
    </div>
    <WaveCallout projectId={projectId} waveId={currentWave.id} waitSummary={waitSummary} />
    {view === "flow" ? <WaveFlow memberIds={currentWave.memberIds} tasks={tasks} runs={runs.data ?? []} reviewMembers={review.error ? undefined : review.data?.members} needsYouIds={review.error ? undefined : review.data?.humanActions?.map((item) => item.taskId)} dependencyFacts={dependencyFacts} selectedTaskId={selected ?? undefined} viewport={viewport} onViewportChange={setViewport} onSelectTask={setSelected} loading={review.isPending || details.some((query) => query.isPending)} error={review.error instanceof Error ? review.error.message : details.find((query) => query.error)?.error instanceof Error ? String(details.find((query) => query.error)?.error) : undefined} />
      : <Reading>{view === "work" ? <WaveReviewDetail projectId={projectId} waveId={currentWave.id} showControls={false} showDependencies={false} /> : canShowWaveResults(review.data, review.error) ? <WaveResults wave={currentWave} tasks={tasks} onOpenDependencies={() => setChosenView("flow")} onOpenTask={setSelected} /> : <p role="status" className="text-[13px] text-muted">{review.error ? "Results unavailable: the acceptance record could not be loaded." : review.isPending ? "Checking the acceptance record…" : "Results appear once this wave is accepted."}</p>}</Reading>}
    <InspectorHost selected={selected} reviewMember={review.error ? undefined : review.data?.members.find((member) => member.taskId === selected)} onClose={() => setSelected(null)} />
  </Shell>;
}
