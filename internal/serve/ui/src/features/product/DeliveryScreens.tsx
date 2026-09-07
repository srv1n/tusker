import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { useQueries } from "@tanstack/react-query";
import { ArrowRight, FileCheck2, GitMerge, Network, Pause, Play, ShieldAlert } from "lucide-react";
import { api } from "@/lib/api";
import { ActionResultLine } from "@/components/ui/action-feedback";
import { renderMermaid } from "@/features/editor/mermaid";
import { useEpics, useFactoryOperations, useRuns, useTasks, useWaveExecute, useWaves } from "@/lib/queries";
import type { EpicSummary, RunSummary, TaskCapsule, WaveSummary } from "@/types/domain";
import {
  ProductEmpty,
  ProductLabel,
  ProductLoading,
  ProductPage,
  ProductButton,
  ProductPhaseStrip,
  ProductRow,
  ProductSection,
  ProductStatus,
  ProductUnavailable,
  phaseTone,
} from "./shared";

type ProductRouteParams = { projectId?: string; waveId?: string };
type WaveBucket = "running" | "checking" | "needsYou" | "blocked" | "ready" | "delivered";

interface WaveTask extends TaskCapsule {
  run?: RunSummary;
  bucket: WaveBucket;
}

const bucketCopy: Record<WaveBucket, { title: string; empty: string }> = {
  running: { title: "Running now", empty: "Nothing is actively running in this wave." },
  checking: { title: "Checking the work", empty: "Nothing is waiting for an objective review." },
  needsYou: { title: "Waiting on you", empty: "Nothing in this wave needs a human decision." },
  blocked: { title: "Blocked", empty: "Nothing is blocked." },
  ready: { title: "Ready next", empty: "No work is waiting to begin." },
  delivered: { title: "Landed", empty: "No tasks have landed yet." },
};

const bucketOrder: WaveBucket[] = ["running", "checking", "needsYou", "blocked", "ready", "delivered"];

function useProjectId(): string {
  return (useParams({ strict: false }) as ProductRouteParams).projectId ?? "";
}

function isLiveRun(run: RunSummary | undefined): boolean {
  return !!run && !run.terminal && run.liveness === "fresh" && ["claimed", "starting", "running"].includes(run.leaseStateRaw ?? "");
}

function waveTaskIds(wave: WaveSummary): Set<string> {
  return new Set([...wave.memberIds, ...wave.members.map((member) => member.id)]);
}

function blockedByHuman(wave: WaveSummary): Set<string> {
  return new Set(wave.brief.humanAction.flatMap((item) => item.blockedTaskIds));
}

function bucketFor(task: TaskCapsule, run: RunSummary | undefined, humanBlocked: Set<string>): WaveBucket {
  // A stale/failed runner does not make a task "running". This is deliberately
  // stricter than task status: only a fresh held execution earns that label.
  if (task.status === "done") return "delivered";
  if (task.hasGate || humanBlocked.has(task.id)) return "needsYou";
  if (task.status === "blocked") return "blocked";
  if (isLiveRun(run)) return "running";
  if (task.status === "in_progress") return "blocked";
  if (task.status === "review") return "checking";
  return "ready";
}

function deriveWaveTasks(wave: WaveSummary, allTasks: TaskCapsule[], runs: RunSummary[]): WaveTask[] {
  const tasksById = new Map(allTasks.map((task) => [task.id, task]));
  const runsByTask = new Map(runs.map((run) => [run.taskId, run]));
  const humanBlocked = blockedByHuman(wave);

  // `members` is a compact server projection, but it omits task metadata. Keep
  // it as a fallback so an older server payload still renders every wave member.
  return [...waveTaskIds(wave)].map((id) => {
    const member = wave.members.find((item) => item.id === id);
    const task = tasksById.get(id) ?? {
      id,
      title: member?.title ?? id,
      epicId: "",
      epicTitle: "",
      status: member?.status === "done" ? "done" : member?.status === "review" ? "review" : member?.status === "blocked" ? "blocked" : "ready",
      readiness: member?.status === "blocked" ? "blocked_dependency" : "ready",
      priority: "p2",
      risk: "medium",
      hasGate: humanBlocked.has(id),
      updatedAt: "",
    } satisfies TaskCapsule;
    const run = runsByTask.get(id);
    return { ...task, run, bucket: bucketFor(task, run, humanBlocked) };
  });
}

function wavePhase(wave: WaveSummary, tasks: WaveTask[]): string {
  if (wave.landedAt || tasks.length > 0 && tasks.every((task) => task.bucket === "delivered")) return "delivered";
  if (wave.authorization.state === "paused") return "paused";
  if (wave.authorization.stale || wave.authorization.state === "stale") return "stale";
  if (tasks.some((task) => task.bucket === "blocked" || task.bucket === "needsYou")) return "blocked";
  if (tasks.some((task) => task.bucket === "checking")) return "checking";
  if (tasks.some((task) => task.bucket === "running")) return "building";
  return wave.authorization.state === "armed" ? "planned" : "not authorized";
}

function friendlyAuthorization(wave: WaveSummary): string {
  if (wave.authorization.stale || wave.authorization.state === "stale") return "Review again";
  if (wave.authorization.state === "armed") return "Authorized";
  if (wave.authorization.state === "paused") return "Paused";
  return "Not authorized";
}

function waveStatusLabel(wave: WaveSummary, tasks: WaveTask[]): string {
  if (wave.status) return wave.status.replaceAll("_", " ");
  const phase = wavePhase(wave, tasks);
  if (phase === "building") return "In progress";
  if (phase === "checking") return "Checking";
  if (phase === "blocked") return "Needs attention";
  if (phase === "delivered") return "Delivered";
  return phase === "not authorized" ? "Not authorized" : "Planned";
}

function queryFailure(...errors: Array<unknown>): unknown {
  return errors.find(Boolean);
}

export function Waves() {
  const projectId = useProjectId();
  const wavesQ = useWaves(projectId);
  const tasksQ = useTasks(projectId);
  const runsQ = useRuns(projectId);
  const error = queryFailure(wavesQ.error, tasksQ.error, runsQ.error);
  const loading = wavesQ.isLoading || tasksQ.isLoading || runsQ.isLoading;
  const rows = useMemo(
    () => (wavesQ.data ?? []).map((wave) => ({ wave, tasks: deriveWaveTasks(wave, tasksQ.data ?? [], runsQ.data ?? []) })),
    [wavesQ.data, tasksQ.data, runsQ.data],
  );

  return (
    <ProductPage title="Work" wide>
      {error ? <ProductUnavailable>Could not load the delivery projection. {error instanceof Error ? error.message : "Try refreshing this project."}</ProductUnavailable> : null}
      {loading ? <ProductLoading rows={4} /> : null}
      {!loading && !error && rows.length === 0 ? <ProductEmpty title="No waves yet" detail="Reviewed plans will appear here once they are authorized as delivery boundaries." /> : null}
      {!loading && rows.length > 0 ? (
        <div className="border-t-2 border-ink">
          {rows.map(({ wave, tasks }) => {
            const phase = wavePhase(wave, tasks);
            const landed = tasks.filter((task) => task.bucket === "delivered").length;
            const moving = tasks.filter((task) => task.bucket === "running" || task.bucket === "checking").length;
            const attention = tasks.filter((task) => task.bucket === "needsYou" || task.bucket === "blocked").length;
            const status = waveStatusLabel(wave, tasks);
            return (
              <ProductRow
                key={wave.id}
                title={wave.title}
                detail={`${tasks.length} task${tasks.length === 1 ? "" : "s"} · ${landed} landed${moving ? ` · ${moving} moving` : ""}${attention ? ` · ${attention} need attention` : ""}`}
                status={<ProductStatus tone={phaseTone(phase)}>{status}</ProductStatus>}
                action={<Link to="/p/$projectId/waves/$waveId" params={{ projectId, waveId: wave.id }} className="text-[12px] font-medium text-info">Open <ArrowRight className="ml-1 inline" size={14} /></Link>}
              />
            );
          })}
        </div>
      ) : null}
      {!tasksQ.isLoading && !tasksQ.error && (tasksQ.data ?? []).length > 0 && <ProductSection title="Tickets" count={tasksQ.data?.length ?? 0}>
        <div className="border-t border-line">
          {(tasksQ.data ?? []).map((task) => {
            const run = (runsQ.data ?? []).find((item) => item.taskId === task.id);
            const status = ticketStatus(task, run);
            return <ProductRow key={task.id} meta={task.id} title={task.title} detail={`${task.epicId || "Unsorted"} · ${task.readiness.replaceAll("_", " ")}`} status={<ProductStatus tone={phaseTone(status)}>{status}</ProductStatus>} action={<Link to="/p/$projectId/tasks/$taskId" params={{ projectId, taskId: task.id }} className="text-[12px] font-medium text-info hover:text-ink">Open <ArrowRight className="ml-1 inline" size={14} /></Link>} />;
          })}
        </div>
      </ProductSection>}
    </ProductPage>
  );
}

function ticketStatus(task: TaskCapsule, run: RunSummary | undefined): string {
  if (isLiveRun(run)) return "Running";
  if (task.hasGate || task.openGates?.length) return "Blocked";
  if (task.status === "done") return "Delivered";
  if (task.status === "review") return "Checking";
  if (task.status === "in_progress") return "In progress";
  if (task.status === "blocked") return "Blocked";
  return task.status === "ready" ? "Ready" : "Planned";
}

export function WaveDetail({ waveId: requestedWaveId }: { waveId?: string } = {}) {
  const params = useParams({ strict: false }) as ProductRouteParams;
  const projectId = params.projectId ?? "";
  const wavesQ = useWaves(projectId);
  const tasksQ = useTasks(projectId);
  const runsQ = useRuns(projectId);
  const wave = (wavesQ.data ?? []).find((item) => item.id === (requestedWaveId ?? params.waveId));
  const tasks = useMemo(() => wave ? deriveWaveTasks(wave, tasksQ.data ?? [], runsQ.data ?? []) : [], [wave, tasksQ.data, runsQ.data]);
  const grouped = useMemo(() => new Map(bucketOrder.map((bucket) => [bucket, tasks.filter((task) => task.bucket === bucket)])), [tasks]);
  const error = queryFailure(wavesQ.error, tasksQ.error, runsQ.error);

  if (wavesQ.isLoading || tasksQ.isLoading || runsQ.isLoading) {
    return <ProductPage title="Wave"><ProductLoading rows={5} /></ProductPage>;
  }
  if (error) {
    return <ProductPage title="Wave"><ProductUnavailable>Could not load this wave. {error instanceof Error ? error.message : "Try refreshing this project."}</ProductUnavailable></ProductPage>;
  }
  if (!wave) {
    return <ProductPage title="Wave"><ProductEmpty title="No wave selected" detail="Choose a delivery boundary from Waves to inspect its current outcome." /></ProductPage>;
  }

  const phase = wavePhase(wave, tasks);
  const summary = `${tasks.filter((task) => task.bucket === "delivered").length} landed · ${tasks.filter((task) => task.bucket === "running").length} running · ${tasks.filter((task) => task.bucket === "needsYou").length} waiting on you · ${tasks.filter((task) => task.bucket === "blocked").length} blocked`;

  return (
    <ProductPage title={wave.title} intro={wave.brief.outcome.summary || summary} wide actions={<WaveExecuteBoundary projectId={projectId} wave={wave} />}>
      <div className="mb-8 flex flex-wrap items-center gap-3">
        <ProductStatus tone={phaseTone(phase)}>{waveStatusLabel(wave, tasks)}</ProductStatus>
        <span className="font-mono text-[11px] text-faint">{summary} · {tasks.length} tasks</span>
      </div>
      <details className="mb-10 rounded-lg border border-line bg-panel/40 px-4 py-3">
        <summary className="cursor-pointer text-[12px] font-medium text-muted hover:text-ink">Delivery details</summary>
        <div className="mt-4 grid gap-6 lg:grid-cols-[minmax(0,1fr)_280px]">
          <ProductPhaseStrip current={phase} />
          <div>
            <ProductLabel>Authorization</ProductLabel>
            <div className="mt-2 flex items-center justify-between gap-3">
              <ProductStatus tone={wave.authorization.state === "armed" ? "pass" : phaseTone(wave.authorization.state)}>{friendlyAuthorization(wave)}</ProductStatus>
              <span className="text-right text-[12px] text-muted">{wave.authorization.action || "No action recorded"}</span>
            </div>
          </div>
        </div>
      </details>

      <ProductSection title="Dependency DAG">
        <DependencyDag projectId={projectId} tasks={tasks} />
      </ProductSection>

      {wave.brief.humanAction.length > 0 && (
        <ProductSection title="Needs you" count={wave.brief.humanAction.length}>
          <div className="border-t border-line">
            {wave.brief.humanAction.map((action) => (
              <ProductRow
                key={action.gateId}
                title={action.action}
                detail={`${action.blockedTaskIds.length} task${action.blockedTaskIds.length === 1 ? "" : "s"} blocked`}
                status={<ProductStatus tone="warn">Action required</ProductStatus>}
                action={<a href={action.gateHref} className="text-[12px] font-medium text-info hover:text-ink">Open action</a>}
              />
            ))}
          </div>
        </ProductSection>
      )}

      {bucketOrder.map((bucket) => {
        const items = grouped.get(bucket) ?? [];
        // Empty technical states do not get a permanent panel; the delivery
        // summary above remains the complete count source.
        if (items.length === 0) return null;
        const copy = bucketCopy[bucket];
        return (
          <ProductSection key={bucket} title={copy.title} count={items.length}>
            <div className="border-t border-line">
              {items.map((task) => (
                <ProductRow
                  key={task.id}
                  title={task.title}
                  detail={task.run?.error || task.run?.outcome === "running" ? task.run?.error ?? "Work is executing." : task.hasGate ? "A human decision is required before this branch can continue." : task.readiness.replaceAll("_", " ")}
                  status={<ProductStatus tone={phaseTone(task.bucket === "needsYou" ? "waiting" : task.bucket)}>{task.bucket === "needsYou" ? "Waiting on you" : task.bucket === "checking" ? "Checking" : task.bucket === "ready" ? "Ready" : task.bucket === "delivered" ? "Landed" : task.bucket}</ProductStatus>}
                  action={<><Link to="/p/$projectId/tasks/$taskId" params={{ projectId, taskId: task.id }} className="text-[12px] font-medium text-info hover:text-ink">Task</Link>{task.run && <Link to="/p/$projectId/runs/$taskId" params={{ projectId, taskId: task.id }} className="text-[12px] font-medium text-info hover:text-ink">Logs</Link>}</>}
                />
              ))}
            </div>
          </ProductSection>
        );
      })}

      <ProductSection title="Outcome and proof" count={wave.brief.seeIt.length}>
        {wave.brief.seeIt.length === 0 ? <ProductEmpty title="No accepted artifacts yet" detail="Accepted proof appears here as tasks complete their review." /> : (
          <div className="border-t border-line">
            {wave.brief.seeIt.map((artifact) => (
              <ProductRow key={`${artifact.taskId}-${artifact.evidenceRef}`} title={artifact.summary} detail={artifact.acceptanceIds.length ? `Covers ${artifact.acceptanceIds.join(", ")}` : "Accepted evidence"} status={<ProductStatus tone="pass">Verified</ProductStatus>} action={<a href={artifact.evidenceHref} className="inline-flex items-center gap-1 text-[12px] font-medium text-info hover:text-ink">Open result <FileCheck2 size={14} /></a>} />
            ))}
          </div>
        )}
      </ProductSection>
    </ProductPage>
  );
}

function WaveExecuteBoundary({ projectId, wave }: { projectId: string; wave: WaveSummary }) {
  const execute = useWaveExecute(projectId);
  const authorizationFingerprint = wave.authorization.fingerprint ?? "";
  useEffect(() => {
    execute.reset();
  }, [execute.reset, wave.id, authorizationFingerprint]);
  const terminal = Boolean(wave.landedAt) || ["landed", "closed", "cancelled", "superseded"].includes(wave.status);
  const blocker = terminal
    ? "This wave is terminal."
    : !authorizationFingerprint
      ? "The current wave version is unavailable. Refresh before executing."
      : undefined;
  const submit = () => {
    if (blocker || execute.isPending) return;
    execute.mutate({ waveId: wave.id });
  };
  const receipt = execute.data?.execution;
  const queued = receipt?.queuedTaskIds ?? [];
  const alreadyQueued = receipt?.alreadyQueuedTaskIds ?? [];
  return <div className="max-w-[24rem] text-right">
    {blocker ? <p role="status" className="mt-1 text-[10.5px] leading-4 text-warn">Blocked: {blocker}</p> : <p className="mt-1 text-left text-[10.5px] leading-4 text-muted">Queues the latest version of this wave for daemon dispatch. It does not enable project automation.</p>}
    <ProductButton tone="primary" disabled={Boolean(blocker) || execute.isPending} onClick={submit} aria-label={`Execute wave ${wave.id}`} title={blocker ?? "Queue the latest wave version for daemon dispatch"}><Play size={13} />{execute.isPending ? "Queueing wave…" : "Execute wave"}</ProductButton>
    <ActionResultLine className="mt-2 text-left" pending={execute.isPending} error={execute.error} result={execute.data} />
    {receipt && <div className="mt-2 text-left text-[11px] leading-4 text-muted" role="status"><p>{queued.length > 0 ? `Queued ${queued.length} task${queued.length === 1 ? "" : "s"} for daemon dispatch.` : "No new task directives were needed."}{alreadyQueued.length > 0 ? ` ${alreadyQueued.length} already queued.` : ""}</p><a href={receipt.statusLink} className="font-semibold text-info hover:text-ink">Open delivery status</a></div>}
  </div>;
}

function DependencyDag({ projectId, tasks }: { projectId: string; tasks: WaveTask[] }) {
  const details = useQueries({ queries: tasks.map((task) => ({ queryKey: ["task", projectId, task.id], queryFn: () => api.task(task.id, projectId) })) });
  const loading = details.some((query) => query.isLoading);
  const failed = details.find((query) => query.error);
  const edges = details.flatMap((query) => query.data?.deps.map((dependency) => ({ from: dependency.id, to: query.data?.id ?? "", title: dependency.title })) ?? []).filter((edge) => edge.to);
  const source = useMemo(() => dependencySource(tasks, edges), [tasks, edges]);
  const [diagram, setDiagram] = useState<{ svg?: string; error?: string } | null>(null);

  useEffect(() => {
    let active = true;
    setDiagram(null);
    if (!source) return () => { active = false; };
    void renderMermaid(source).then((result) => { if (active) setDiagram(result); });
    return () => { active = false; };
  }, [source]);

  if (loading) return <p className="text-[12px] text-muted">Loading task dependencies…</p>;
  if (failed && !edges.length) return <ProductUnavailable>Dependency graph unavailable. {failed.error instanceof Error ? failed.error.message : "A task detail could not be read."}</ProductUnavailable>;
  if (!source) return <ProductEmpty title="No dependency edges recorded" detail="This wave has no task prerequisite relationships in the canonical task details." />;
  return <div className="rounded-lg border border-line bg-panel p-3"><p className="mb-3 text-[11px] text-muted">Edges come from each task’s canonical prerequisite list. Status remains in the ticket rows above.</p>{failed && <p role="status" className="mb-3 text-[11px] text-warn">Some task details could not be read; this graph may be incomplete.</p>}{diagram?.svg ? <div className="overflow-x-auto" dangerouslySetInnerHTML={{ __html: diagram.svg }} /> : <pre className="overflow-x-auto whitespace-pre-wrap font-mono text-[11px] text-muted">{diagram?.error ?? "Rendering dependency graph…"}{diagram?.error ? `\n\n${source}` : ""}</pre>}</div>;
}

function dependencySource(tasks: WaveTask[], edges: Array<{ from: string; to: string; title: string }>): string | null {
  if (!edges.length) return null;
  const ids = [...new Set([...tasks.map((task) => task.id), ...edges.flatMap((edge) => [edge.from, edge.to])])];
  const nodeId = new Map(ids.map((id, index) => [id, `task${index}`]));
  const title = new Map(tasks.map((task) => [task.id, task.title]));
  for (const edge of edges) title.set(edge.from, edge.title);
  const label = (id: string) => `${nodeId.get(id)}["${(title.get(id) ?? id).replaceAll("\\", "\\\\").replaceAll('"', "'")}"]`;
  return ["flowchart LR", ...ids.map(label), ...edges.map((edge) => `${nodeId.get(edge.from)} --> ${nodeId.get(edge.to)}`)].join("\n");
}

export function Epics() {
  const projectId = useProjectId();
  const epicsQ = useEpics(projectId);
  const tasksQ = useTasks(projectId);
  const runsQ = useRuns(projectId);
  const rows = useMemo(() => deriveEpics(epicsQ.data ?? [], tasksQ.data ?? [], runsQ.data ?? []), [epicsQ.data, tasksQ.data, runsQ.data]);
  const error = queryFailure(epicsQ.error, tasksQ.error, runsQ.error);
  const loading = epicsQ.isLoading || tasksQ.isLoading || runsQ.isLoading;

  return (
    <ProductPage title="Epics" eyebrow="Project outcomes" intro="Outcome groups and their delivery state. Tasks remain a drill-down, not the primary unit of progress." wide>
      {error ? <ProductUnavailable>Could not load epic progress. {error instanceof Error ? error.message : "Try refreshing this project."}</ProductUnavailable> : null}
      {loading ? <ProductLoading rows={4} /> : null}
      {!loading && !error && rows.length === 0 ? <ProductEmpty title="No epics yet" detail="Epics appear when project work is grouped under an outcome." /> : null}
      {!loading && rows.length > 0 ? <div className="border-t-2 border-ink">{rows.map((epic) => <EpicRow key={epic.id} epic={epic} />)}</div> : null}
    </ProductPage>
  );
}

function EpicRow({ epic }: { epic: ReturnType<typeof deriveEpics>[number] }) {
  const phase = epic.blocked > 0 ? "blocked" : epic.needsYou > 0 ? "waiting" : epic.running > 0 ? "building" : epic.checking > 0 ? "checking" : epic.total > 0 && epic.landed === epic.total ? "delivered" : "planned";
  return (
    <ProductRow
      meta={epic.id}
      title={epic.title}
      detail={`${epic.landed} delivered · ${epic.running + epic.checking} moving${epic.needsYou ? ` · ${epic.needsYou} waiting on you` : ""}${epic.blocked ? ` · ${epic.blocked} blocked` : ""} · ${epic.total} tasks`}
      status={<ProductStatus tone={phaseTone(phase)}>{phase === "building" ? "Building" : phase === "checking" ? "Checking" : phase === "delivered" ? "Delivered" : phase === "waiting" ? "Needs your decision" : phase}</ProductStatus>}
    />
  );
}

function deriveEpics(epics: EpicSummary[], tasks: TaskCapsule[], runs: RunSummary[]) {
  const known = new Map(epics.map((epic) => [epic.id, epic]));
  const ids = new Set([...known.keys(), ...tasks.map((task) => task.epicId)]);
  const runsByTask = new Map(runs.map((run) => [run.taskId, run]));
  return [...ids].map((id) => {
    const tasksForEpic = tasks.filter((task) => task.epicId === id);
    const syntheticWave: WaveSummary = {
      id,
      title: known.get(id)?.title ?? tasksForEpic[0]?.epicTitle ?? id,
      status: "",
      memberIds: tasksForEpic.map((task) => task.id),
      members: [],
      counts: {},
      authorization: { state: "disarmed", stale: false, action: "" },
      brief: { schema: "tusker.wave-brief/v1", waveId: id, title: id, waveHref: "", sectionOrder: ["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"], outcome: { summary: "", fullyDrained: false, counts: {}, tasks: [] }, seeIt: [], landed: [], reworkParked: [], humanAction: [], documentation: [] },
    };
    const visual = deriveWaveTasks(syntheticWave, tasksForEpic, [...runsByTask.values()]);
    return {
      id,
      title: known.get(id)?.title ?? tasksForEpic[0]?.epicTitle ?? id,
      total: visual.length,
      landed: visual.filter((task) => task.bucket === "delivered").length,
      running: visual.filter((task) => task.bucket === "running").length,
      checking: visual.filter((task) => task.bucket === "checking").length,
      needsYou: visual.filter((task) => task.bucket === "needsYou").length,
      blocked: visual.filter((task) => task.bucket === "blocked").length,
    };
  }).sort((left, right) => right.blocked + right.needsYou - (left.blocked + left.needsYou) || left.title.localeCompare(right.title));
}

export function Trains() {
  const projectId = useProjectId();
  const wavesQ = useWaves(projectId);
  const tasksQ = useTasks(projectId);
  const runsQ = useRuns(projectId);
  const operationsQ = useFactoryOperations(projectId);
  const rows = useMemo(
    () => (wavesQ.data ?? []).map((wave) => ({ wave, tasks: deriveWaveTasks(wave, tasksQ.data ?? [], runsQ.data ?? []) })),
    [wavesQ.data, tasksQ.data, runsQ.data],
  );
  const error = queryFailure(wavesQ.error, tasksQ.error, runsQ.error, operationsQ.error);
  const loading = wavesQ.isLoading || tasksQ.isLoading || runsQ.isLoading || operationsQ.isLoading;

  return (
    <ProductPage title="Trains" eyebrow="Integration and promotion" intro="Delivery boundaries approaching integration. This view reports only authorization and completion evidence that the current API actually supplies." wide>
      <ProductUnavailable>
        Promotion-specific readiness is unavailable: the current API does not expose the full gate, scheduled departure, or per-wave promotion receipt. Authorization and task-drain state are shown below.
      </ProductUnavailable>
      {error ? <ProductUnavailable>Could not load integration state. {error instanceof Error ? error.message : "Try refreshing this project."}</ProductUnavailable> : null}
      {loading ? <div className="mt-8"><ProductLoading rows={3} /></div> : null}
      {!loading && !error && rows.length === 0 ? <ProductEmpty title="No delivery trains" detail="Authorized waves will appear here once they have work to integrate." /> : null}
      {!loading && rows.length > 0 ? (
        <ProductSection title="Delivery boundaries" count={rows.length} className="mt-10">
          <div className="border-t-2 border-ink">
            {rows.map(({ wave, tasks }) => <TrainRow key={wave.id} wave={wave} tasks={tasks} />)}
          </div>
        </ProductSection>
      ) : null}
      {operationsQ.data ? (
        <ProductSection title="Current operating policy">
          <div className="grid gap-px border border-line bg-line md:grid-cols-3">
            <PolicyCell label="Promotion mode" value={operationsQ.data.project.promotionMode.mode || "Not reported"} />
            <PolicyCell label="Project capacity" value={`${operationsQ.data.capacity.project.active} active of ${operationsQ.data.capacity.project.limit}`} />
            <PolicyCell label="Global capacity" value={`${operationsQ.data.capacity.global.active} active of ${operationsQ.data.capacity.global.limit}`} />
          </div>
        </ProductSection>
      ) : null}
    </ProductPage>
  );
}

function TrainRow({ wave, tasks }: { wave: WaveSummary; tasks: WaveTask[] }) {
  const fullyDrained = wave.brief.outcome.fullyDrained || (tasks.length > 0 && tasks.every((task) => ["delivered", "checking", "blocked", "needsYou"].includes(task.bucket)));
  const state = wave.authorization.stale || wave.authorization.state === "stale"
    ? "Review again"
    : wave.authorization.state === "paused"
      ? "Paused"
      : wave.authorization.state !== "armed"
        ? "Not authorized"
        : fullyDrained
          ? "Ready to integrate"
          : "Building";
  return (
    <ProductRow
      meta={wave.id}
      title={wave.title}
      detail={`${tasks.filter((task) => task.bucket === "delivered").length} landed · ${tasks.filter((task) => task.bucket === "running" || task.bucket === "checking").length} still moving · ${tasks.filter((task) => task.bucket === "blocked" || task.bucket === "needsYou").length} held`}
      status={<ProductStatus tone={state === "Review again" || state === "Paused" ? "warn" : phaseTone(state)}>{state}</ProductStatus>}
      action={state === "Ready to integrate" ? <GitMerge size={16} className="text-info" /> : state === "Paused" ? <Pause size={16} className="text-warn" /> : state === "Review again" ? <ShieldAlert size={16} className="text-warn" /> : <Network size={16} className="text-faint" />}
    />
  );
}

function PolicyCell({ label, value }: { label: string; value: string }) {
  return <div className="bg-surface px-4 py-4"><ProductLabel>{label}</ProductLabel><div className="mt-2 text-[14px] font-medium text-ink">{value}</div></div>;
}
