import { useEffect, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import { useNavigate, useParams, useRouterState } from "@tanstack/react-router";
import { api } from "@/lib/api";
import { useRun, useRuns, useTask, useTasks, useWaves } from "@/lib/queries";
import { TaskBoard } from "../board";
import { WaveFlow, type FlowViewport } from "../flow";
import { TaskInspector } from "../inspector/TaskInspector";
import { WaveOverview } from "../overview";
import { WaveResults } from "../results/WaveResults";
import { initialWaveView, waveStartability, type WorkView } from "./integrationModel";

type Params = { projectId?: string; waveId?: string };

function useProjectId() {
  return (useParams({ strict: false }) as Params).projectId ?? "";
}

function Shell({ tab, children }: { tab: "waves" | "board"; children: React.ReactNode }) {
  const projectId = useProjectId();
  const navigate = useNavigate();
  return <main data-wux-ready="true" className="h-full overflow-y-auto bg-surface px-4 py-6 text-ink sm:px-7 lg:px-10">
    <div className="mx-auto w-full max-w-[1180px]">
      <header className="mb-7 border-b border-line pb-5">
        <p className="font-mono text-[10px] uppercase tracking-[0.14em] text-faint">{projectId}</p>
        <h1 className="mt-1 font-serif text-[32px] font-semibold tracking-[-0.025em]">Work</h1>
        <nav className="mt-5 flex gap-5" aria-label="Work views">
          {(["waves", "board"] as const).map((value) => <button key={value} type="button" aria-current={tab === value ? "page" : undefined} onClick={() => navigate({ to: value === "waves" ? "/p/$projectId/waves" : "/p/$projectId/tasks", params: { projectId } })} className={tab === value ? "border-b-2 border-ink pb-2 text-[13px] font-semibold" : "pb-2 text-[13px] text-muted hover:text-ink"}>{value === "waves" ? "Waves" : "Board"}</button>)}
        </nav>
      </header>
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
  const [query, setQuery] = useState("");
  const [showCompleted, setShowCompleted] = useState(false);
  return <Shell tab="waves"><WaveOverview
    waves={waves.data ?? []} tasks={tasks.data ?? []} runs={runs.data ?? []}
    startability={waveStartability(waves.data ?? [])}
    descriptions={Object.fromEntries((waves.data ?? []).filter((wave) => wave.brief.outcome.summary).map((wave) => [wave.id, wave.brief.outcome.summary]))}
    query={query} showCompleted={showCompleted} onQueryChange={setQuery} onShowCompletedChange={setShowCompleted}
    onOpenWave={(waveId) => navigate({ to: "/p/$projectId/waves/$waveId", params: { projectId, waveId } })}
    onOpenUnassigned={() => navigate({ to: "/p/$projectId/tasks", params: { projectId } })}
    loading={waves.isPending || tasks.isPending || runs.isPending}
    error={[waves.error, tasks.error, runs.error].find(Boolean) instanceof Error ? String([waves.error, tasks.error, runs.error].find(Boolean)) : undefined}
  /></Shell>;
}

function InspectorHost({ selected, onClose }: { selected: string | null; onClose: () => void }) {
  return selected ? <LoadedInspector selected={selected} onClose={onClose} /> : null;
}

function LoadedInspector({ selected, onClose }: { selected: string; onClose: () => void }) {
  const projectId = useProjectId();
  const navigate = useNavigate();
  const task = useTask(selected, projectId);
  const run = useRun(selected, false, projectId);
  return <TaskInspector selectedTaskId={selected} task={task.data ?? null} run={run.data ?? null} loading={task.isPending} error={task.error instanceof Error ? task.error.message : undefined} onClose={onClose} onOpenTask={(taskId) => navigate({ to: "/p/$projectId/tasks/$taskId", params: { projectId, taskId } })} />;
}

export function WorkBoard() {
  const projectId = useProjectId();
  const tasks = useTasks(projectId);
  const runs = useRuns(projectId);
  const [mode, setMode] = useState<"board" | "list">("board");
  const [selected, setSelected] = useState<string | null>(null);
  return <Shell tab="board"><TaskBoard tasks={tasks.data ?? []} runs={runs.data ?? []} mode={mode} onModeChange={setMode} onSelectTask={setSelected} selectedTags={[]} onSelectedTagsChange={() => {}} tagsAvailable={false} /><InspectorHost selected={selected} onClose={() => setSelected(null)} /></Shell>;
}

export function WorkWave() {
  const { waveId = "" } = useParams({ strict: false }) as Params;
  const projectId = useProjectId();
  const location = useRouterState({ select: (state) => state.location });
  const waves = useWaves(projectId);
  const runs = useRuns(projectId);
  const wave = waves.data?.find((item) => item.id === waveId);
  const details = useQueries({ queries: (wave?.memberIds ?? []).map((id) => ({ queryKey: ["task", id, projectId], queryFn: () => api.task(id, projectId) })) });
  const tasks = details.flatMap((query) => query.data ? [query.data] : []);
  const requested = new URLSearchParams(location.searchStr).get("view") ?? undefined;
  const [chosenView, setChosenView] = useState<WorkView | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [viewport, setViewport] = useState<FlowViewport>({ x: 32, y: 32, scale: 1 });
  useEffect(() => {
    setChosenView(null);
    setSelected(null);
  }, [waveId]);
  const view = wave ? chosenView ?? initialWaveView(wave, requested) : "flow";
  if (waves.isPending || !wave) return <Shell tab="waves"><p role={waves.error ? "alert" : "status"} className="text-[13px] text-muted">{waves.error ? "Wave unavailable." : "Loading wave…"}</p></Shell>;
  return <Shell tab="waves">
    <div className="mb-5 flex flex-wrap items-end justify-between gap-3"><div><p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">{wave.id}</p><h2 className="mt-1 font-serif text-[26px] font-semibold">{wave.title}</h2></div><div className="flex gap-2"><button type="button" aria-pressed={view === "flow"} onClick={() => setChosenView("flow")} className="rounded-md border border-line px-3 py-2 text-[12px]">Flow</button><button type="button" aria-pressed={view === "results"} onClick={() => setChosenView("results")} className="rounded-md border border-line px-3 py-2 text-[12px]">Results</button></div></div>
    {view === "results" ? <WaveResults wave={wave} tasks={tasks} onOpenFlow={() => setChosenView("flow")} onOpenTask={setSelected} /> : <WaveFlow memberIds={wave.memberIds} tasks={tasks} runs={runs.data ?? []} selectedTaskId={selected ?? undefined} viewport={viewport} onViewportChange={setViewport} onSelectTask={setSelected} loading={details.some((query) => query.isPending)} error={details.find((query) => query.error)?.error instanceof Error ? String(details.find((query) => query.error)?.error) : undefined} />}
    <InspectorHost selected={selected} onClose={() => setSelected(null)} />
  </Shell>;
}
