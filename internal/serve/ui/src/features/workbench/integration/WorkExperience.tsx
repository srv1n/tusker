import { useEffect, useMemo, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import { useNavigate, useParams, useRouterState } from "@tanstack/react-router";
import { api } from "@/lib/api";
import { qk, useProjects, useRun, useRuns, useTask, useTasks, useWaves, useWaveReview, waveReviewQuery } from "@/lib/queries";
import { WaveAuthorityControls, WaveReviewDetail } from "@/features/workbench/integration/WaveAuthority";
import { TaskBoard } from "../board";
import { WaveFlow, type DependencyFact, type FlowViewport } from "../flow";
import { TaskInspector } from "../inspector/TaskInspector";
import { WaveOverview, type WaveOverviewFilter } from "../overview";
import { WaveResults } from "../results/WaveResults";
import { getWorkspaceViewState, NAVIGATION_CHANGED_EVENT, readNavigationState, updateWorkspaceViewState, writeNavigationState, type StorageLike } from "../navigation";
import { canShowWaveResults, settleEnteredWaveView, usableWaveReview, waveStartability, waveSummaryWithReview, type WorkView } from "./integrationModel";
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

function savedOverview(projectId: string, projectIds: string[]): { query: string; category: WaveOverviewFilter } {
  const waves = getWorkspaceViewState(readNavigationState(navigationStorage(), projectIds), projectId).waves;
  return { query: waves?.overviewQuery ?? "", category: waves?.overviewFilter ?? "all" };
}

function persistOverview(projectId: string, projectIds: string[], patch: { overviewQuery?: string; overviewFilter?: WaveOverviewFilter }) {
  if (!projectIds.includes(projectId)) return;
  const storage = navigationStorage();
  writeNavigationState(storage, updateWorkspaceViewState(readNavigationState(storage, projectIds), projectId, { waves: patch }));
  if (typeof window !== "undefined") window.dispatchEvent(new Event(NAVIGATION_CHANGED_EVENT));
}

/** A checkout route belongs to its registered parent project in navigation. */
export function projectContextName(projects: ProjectSummary[] | undefined, projectId: string): string {
  return projects?.find((project) => projectContainsCheckout(project, projectId))?.name ?? projectId;
}

function Shell({ children }: { children: React.ReactNode }) {
  return <main data-wux-ready="true" className="h-full overflow-y-auto bg-surface px-4 py-6 text-ink sm:px-7 lg:px-10">
    <div className="mx-auto w-full max-w-[1180px]">
      {children}
    </div>
  </main>;
}

export function WorkOverview() {
  const projectId = useProjectId();
  const navigate = useNavigate();
  const waves = useWaves(projectId);
  const tasks = useTasks(projectId);
  const runs = useRuns(projectId);
  const projects = useProjects();
  const projectIds = useMemo(
    () => [...new Set((projects.data ?? []).flatMap((project) => [project.id, ...(project.checkouts ?? []).map((checkout) => checkout.id)]))],
    [projects.data],
  );
  const reviews = useQueries({ queries: (waves.data ?? []).map((wave) => waveReviewQuery(wave.id, projectId)) });
  const reviewData = reviews.flatMap((query) => {
    const data = usableWaveReview(query.data, query.error);
    return data ? [data] : [];
  });
  const reviewErrors = Object.fromEntries((waves.data ?? []).map((wave, index) => [wave.id, reviews[index]?.error]));
  const displayedWaves = (waves.data ?? []).map((wave, index) => waveSummaryWithReview(wave, reviews[index]?.data, reviews[index]?.error));
  const [query, setQuery] = useState(() => savedOverview(projectId, [projectId]).query);
  const [category, setCategory] = useState<WaveOverviewFilter>(() => savedOverview(projectId, [projectId]).category);
  useEffect(() => {
    const saved = savedOverview(projectId, projectIds.length > 0 ? projectIds : [projectId]);
    setQuery(saved.query);
    setCategory(saved.category);
  }, [projectId]);
  const updateQuery = (value: string) => {
    setQuery(value);
    persistOverview(projectId, projectIds, { overviewQuery: value });
  };
  const updateCategory = (value: WaveOverviewFilter) => {
    setCategory(value);
    persistOverview(projectId, projectIds, { overviewFilter: value });
  };
  return <Shell><WaveOverview
    waves={displayedWaves} tasks={tasks.data ?? []} runs={runs.data ?? []}
    startability={waveStartability(displayedWaves, reviewData, reviewErrors)}
    descriptions={Object.fromEntries((waves.data ?? []).map((wave) => [wave.id, wave.expectedOutcome ?? wave.brief.expectedOutcome ?? wave.brief.outcome.summary]))}
    projectName={projectContextName(projects.data, projectId)}
    query={query} category={category} onQueryChange={updateQuery} onCategoryChange={updateCategory}
    onOpenWave={(waveId) => navigate({ to: "/p/$projectId/waves/$waveId", params: { projectId, waveId } })}
    onOpenUnassigned={() => navigate({ to: "/p/$projectId/tasks", params: { projectId }, search: { scope: "unassigned" } })}
    loading={waves.isPending || tasks.isPending || runs.isPending || reviews.some((query) => query.isPending)}
    error={[waves.error, tasks.error, runs.error].find(Boolean) instanceof Error ? String([waves.error, tasks.error, runs.error].find(Boolean)) : undefined}
  /></Shell>;
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
  if (unassigned && waves.isPending) return <Shell><p role="status" className="text-[13px] text-muted">Loading wave membership…</p></Shell>;
  if (unassigned && waves.error) return <Shell><p role="alert" className="text-[13px] text-fail">Unassigned tasks are unavailable until wave membership can be loaded.</p></Shell>;
  return <Shell><TaskBoard tasks={tasks.data ?? []} runs={runs.data ?? []} mode={mode} onModeChange={setMode} onSelectTask={setSelected} taskIds={unassigned ? unassignedIds : undefined} selectedTags={[]} onSelectedTagsChange={() => {}} tagsAvailable={false} /><InspectorHost selected={selected} onClose={() => setSelected(null)} /></Shell>;
}

export function WorkWave() {
  const { waveId = "" } = useParams({ strict: false }) as Params;
  const projectId = useProjectId();
  const location = useRouterState({ select: (state) => state.location });
  const waves = useWaves(projectId);
  const runs = useRuns(projectId);
  const wave = waves.data?.find((item) => item.id === waveId);
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
  if (waves.isPending || !currentWave) return <Shell><p role={waves.error ? "alert" : "status"} className="text-[13px] text-muted">{waves.error ? "Wave unavailable." : "Loading wave…"}</p></Shell>;
  return <Shell>
    <div className="mb-5"><p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">{currentWave.id}</p><h2 className="mt-1 font-serif text-[26px] font-semibold">{currentWave.title}</h2>{(currentWave.expectedOutcome ?? currentWave.brief.expectedOutcome) && <p className="mt-2 max-w-2xl text-[13px] text-muted">{currentWave.expectedOutcome ?? currentWave.brief.expectedOutcome}</p>}</div>
    <div className="mb-5"><WaveAuthorityControls projectId={projectId} waveId={currentWave.id} /></div>
    <nav aria-label="Wave views" className="mb-5 flex gap-2 border-b border-line pb-3">{(["flow", "work", "results"] as const).map((tab) => <button key={tab} type="button" aria-pressed={view === tab} onClick={() => setChosenView(tab)} className={`rounded-md px-4 py-2 text-[13px] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 ${view === tab ? "bg-ink text-surface" : "border border-line"}`}>{tab === "work" ? "Tasks" : tab === "flow" ? "Dependencies" : "Results"}</button>)}</nav>
    {view === "work" ? <WaveReviewDetail projectId={projectId} waveId={currentWave.id} showControls={false} showDependencies={false} /> : view === "results" ? canShowWaveResults(review.data, review.error) ? <WaveResults wave={currentWave} tasks={tasks} onOpenDependencies={() => setChosenView("flow")} onOpenTask={setSelected} /> : <section aria-label="Results unavailable" className="rounded-lg border border-line bg-raised px-4 py-5"><h3 className="text-[14px] font-semibold text-ink">Results are not available yet</h3><p className="mt-1 text-[13px] text-muted">{review.error ? "The acceptance record could not be loaded. Try refreshing." : review.isPending ? "Checking the acceptance record…" : "This wave has not been accepted. Tasks and dependencies remain available."}</p></section> : <WaveFlow memberIds={currentWave.memberIds} tasks={tasks} runs={runs.data ?? []} reviewMembers={review.error ? undefined : review.data?.members} dependencyFacts={dependencyFacts} authorization={waveAuthorization} startEnabled={waveStartEnabled} selectedTaskId={selected ?? undefined} viewport={viewport} onViewportChange={setViewport} onSelectTask={setSelected} loading={review.isPending || details.some((query) => query.isPending)} error={review.error instanceof Error ? review.error.message : details.find((query) => query.error)?.error instanceof Error ? String(details.find((query) => query.error)?.error) : undefined} />}
    <InspectorHost selected={selected} reviewMember={review.error ? undefined : review.data?.members.find((member) => member.taskId === selected)} onClose={() => setSelected(null)} />
  </Shell>;
}
