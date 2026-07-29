import { useMemo } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowRight, FileCheck2, GitMerge, Network, Pause, ShieldAlert } from "lucide-react";
import { useEpics, useFactoryOperations, useRuns, useTasks, useWaves } from "@/lib/queries";
import type { EpicSummary, RunSummary, TaskCapsule, WaveSummary } from "@/types/domain";
import {
  V2Empty,
  V2Label,
  V2Loading,
  V2Page,
  V2PhaseStrip,
  V2Row,
  V2Section,
  V2Status,
  V2Unavailable,
  phaseTone,
} from "./shared";

type V2RouteParams = { projectId?: string; waveId?: string };
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
  return (useParams({ strict: false }) as V2RouteParams).projectId ?? "";
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

function queryFailure(...errors: Array<unknown>): unknown {
  return errors.find(Boolean);
}

export function WavesV2() {
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
    <V2Page title="Waves" eyebrow="Deliveries" intro="Active and completed delivery boundaries, ordered by what is moving or needs attention." wide>
      {error ? <V2Unavailable>Could not load the delivery projection. {error instanceof Error ? error.message : "Try refreshing this project."}</V2Unavailable> : null}
      {loading ? <V2Loading rows={4} /> : null}
      {!loading && !error && rows.length === 0 ? <V2Empty title="No waves yet" detail="Reviewed plans will appear here once they are authorized as delivery boundaries." /> : null}
      {!loading && rows.length > 0 ? (
        <div className="border-t-2 border-ink">
          {rows.map(({ wave, tasks }) => {
            const phase = wavePhase(wave, tasks);
            const landed = tasks.filter((task) => task.bucket === "delivered").length;
            const moving = tasks.filter((task) => task.bucket === "running" || task.bucket === "checking").length;
            const attention = tasks.filter((task) => task.bucket === "needsYou" || task.bucket === "blocked").length;
            return (
              <V2Row
                key={wave.id}
                meta={wave.id}
                title={wave.title}
                detail={`${tasks.length} task${tasks.length === 1 ? "" : "s"} · ${landed} landed${moving ? ` · ${moving} moving` : ""}${attention ? ` · ${attention} need attention` : ""}`}
                status={<V2Status tone={phaseTone(phase)}>{phase === "building" ? "Building" : phase === "checking" ? "Checking" : phase === "blocked" ? "Needs attention" : phase}</V2Status>}
                action={<Link to="/p/$projectId/waves/$waveId" params={{ projectId, waveId: wave.id }} className="text-[12px] font-medium text-info">Open <ArrowRight className="ml-1 inline" size={14} /></Link>}
              />
            );
          })}
        </div>
      ) : null}
    </V2Page>
  );
}

export function WaveDetailV2({ waveId: requestedWaveId }: { waveId?: string } = {}) {
  const params = useParams({ strict: false }) as V2RouteParams;
  const projectId = params.projectId;
  const wavesQ = useWaves(projectId);
  const tasksQ = useTasks(projectId);
  const runsQ = useRuns(projectId);
  const wave = (wavesQ.data ?? []).find((item) => item.id === (requestedWaveId ?? params.waveId));
  const tasks = useMemo(() => wave ? deriveWaveTasks(wave, tasksQ.data ?? [], runsQ.data ?? []) : [], [wave, tasksQ.data, runsQ.data]);
  const grouped = useMemo(() => new Map(bucketOrder.map((bucket) => [bucket, tasks.filter((task) => task.bucket === bucket)])), [tasks]);
  const error = queryFailure(wavesQ.error, tasksQ.error, runsQ.error);

  if (wavesQ.isLoading || tasksQ.isLoading || runsQ.isLoading) {
    return <V2Page title="Wave"><V2Loading rows={5} /></V2Page>;
  }
  if (error) {
    return <V2Page title="Wave"><V2Unavailable>Could not load this wave. {error instanceof Error ? error.message : "Try refreshing this project."}</V2Unavailable></V2Page>;
  }
  if (!wave) {
    return <V2Page title="Wave"><V2Empty title="No wave selected" detail="Choose a delivery boundary from Waves to inspect its current outcome." /></V2Page>;
  }

  const phase = wavePhase(wave, tasks);
  const summary = `${tasks.filter((task) => task.bucket === "delivered").length} landed · ${tasks.filter((task) => task.bucket === "running").length} running · ${tasks.filter((task) => task.bucket === "needsYou").length} waiting on you · ${tasks.filter((task) => task.bucket === "blocked").length} blocked`;

  return (
    <V2Page title={wave.title} eyebrow={`Wave ${wave.id}`} intro={wave.brief.outcome.summary || summary} wide>
      <div className="mb-10 grid gap-6 lg:grid-cols-[minmax(0,1fr)_280px]">
        <div>
          <V2PhaseStrip current={phase} />
          <p className="mt-3 font-mono text-[11px] text-faint">{summary} · {tasks.length} tasks</p>
        </div>
        <div className="border-t-2 border-ink pt-3">
          <V2Label>Authorization</V2Label>
          <div className="mt-2 flex items-center justify-between gap-3">
            <V2Status tone={wave.authorization.state === "armed" ? "pass" : phaseTone(wave.authorization.state)}>{friendlyAuthorization(wave)}</V2Status>
            <span className="text-right text-[12px] text-muted">{wave.authorization.action || "No action recorded"}</span>
          </div>
        </div>
      </div>

      {bucketOrder.map((bucket) => {
        const items = grouped.get(bucket) ?? [];
        // Empty technical states do not get a permanent panel; the delivery
        // summary above remains the complete count source.
        if (items.length === 0) return null;
        const copy = bucketCopy[bucket];
        return (
          <V2Section key={bucket} title={copy.title} count={items.length}>
            <div className="border-t border-line">
              {items.map((task) => (
                <V2Row
                  key={task.id}
                  meta={task.id}
                  title={task.title}
                  detail={task.run?.error || task.run?.outcome === "running" ? task.run?.error ?? "Work is executing." : task.hasGate ? "A human decision is required before this branch can continue." : task.readiness.replaceAll("_", " ")}
                  status={<V2Status tone={phaseTone(task.bucket === "needsYou" ? "waiting" : task.bucket)}>{task.bucket === "needsYou" ? "Waiting on you" : task.bucket === "checking" ? "Checking" : task.bucket === "ready" ? "Ready" : task.bucket === "delivered" ? "Landed" : task.bucket}</V2Status>}
                />
              ))}
            </div>
          </V2Section>
        );
      })}

      <V2Section title="Outcome and proof" count={wave.brief.seeIt.length}>
        {wave.brief.seeIt.length === 0 ? <V2Empty title="No accepted artifacts yet" detail="Accepted proof appears here as tasks complete their review." /> : (
          <div className="border-t border-line">
            {wave.brief.seeIt.map((artifact) => (
              <V2Row key={`${artifact.taskId}-${artifact.evidenceRef}`} meta={artifact.kind} title={artifact.summary} detail={artifact.acceptanceIds.length ? `Covers ${artifact.acceptanceIds.join(", ")}` : "Accepted evidence"} status={<V2Status tone="pass">Verified</V2Status>} action={<FileCheck2 size={16} className="text-pass" />} />
            ))}
          </div>
        )}
      </V2Section>
    </V2Page>
  );
}

export function EpicsV2() {
  const projectId = useProjectId();
  const epicsQ = useEpics(projectId);
  const tasksQ = useTasks(projectId);
  const runsQ = useRuns(projectId);
  const rows = useMemo(() => deriveEpics(epicsQ.data ?? [], tasksQ.data ?? [], runsQ.data ?? []), [epicsQ.data, tasksQ.data, runsQ.data]);
  const error = queryFailure(epicsQ.error, tasksQ.error, runsQ.error);
  const loading = epicsQ.isLoading || tasksQ.isLoading || runsQ.isLoading;

  return (
    <V2Page title="Epics" eyebrow="Project outcomes" intro="Outcome groups and their delivery state. Tasks remain a drill-down, not the primary unit of progress." wide>
      {error ? <V2Unavailable>Could not load epic progress. {error instanceof Error ? error.message : "Try refreshing this project."}</V2Unavailable> : null}
      {loading ? <V2Loading rows={4} /> : null}
      {!loading && !error && rows.length === 0 ? <V2Empty title="No epics yet" detail="Epics appear when project work is grouped under an outcome." /> : null}
      {!loading && rows.length > 0 ? <div className="border-t-2 border-ink">{rows.map((epic) => <EpicRow key={epic.id} epic={epic} />)}</div> : null}
    </V2Page>
  );
}

function EpicRow({ epic }: { epic: ReturnType<typeof deriveEpics>[number] }) {
  const phase = epic.blocked > 0 ? "blocked" : epic.needsYou > 0 ? "waiting" : epic.running > 0 ? "building" : epic.checking > 0 ? "checking" : epic.total > 0 && epic.landed === epic.total ? "delivered" : "planned";
  return (
    <V2Row
      meta={epic.id}
      title={epic.title}
      detail={`${epic.landed} delivered · ${epic.running + epic.checking} moving${epic.needsYou ? ` · ${epic.needsYou} waiting on you` : ""}${epic.blocked ? ` · ${epic.blocked} blocked` : ""} · ${epic.total} tasks`}
      status={<V2Status tone={phaseTone(phase)}>{phase === "building" ? "Building" : phase === "checking" ? "Checking" : phase === "delivered" ? "Delivered" : phase === "waiting" ? "Needs your decision" : phase}</V2Status>}
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

export function TrainsV2() {
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
    <V2Page title="Trains" eyebrow="Integration and promotion" intro="Delivery boundaries approaching integration. This view reports only authorization and completion evidence that the current API actually supplies." wide>
      <V2Unavailable>
        Promotion-specific readiness is unavailable: the current API does not expose the full gate, scheduled departure, or per-wave promotion receipt. Authorization and task-drain state are shown below.
      </V2Unavailable>
      {error ? <V2Unavailable>Could not load integration state. {error instanceof Error ? error.message : "Try refreshing this project."}</V2Unavailable> : null}
      {loading ? <div className="mt-8"><V2Loading rows={3} /></div> : null}
      {!loading && !error && rows.length === 0 ? <V2Empty title="No delivery trains" detail="Authorized waves will appear here once they have work to integrate." /> : null}
      {!loading && rows.length > 0 ? (
        <V2Section title="Delivery boundaries" count={rows.length} className="mt-10">
          <div className="border-t-2 border-ink">
            {rows.map(({ wave, tasks }) => <TrainRow key={wave.id} wave={wave} tasks={tasks} />)}
          </div>
        </V2Section>
      ) : null}
      {operationsQ.data ? (
        <V2Section title="Current operating policy">
          <div className="grid gap-px border border-line bg-line md:grid-cols-3">
            <PolicyCell label="Promotion mode" value={operationsQ.data.project.promotionMode.mode || "Not reported"} />
            <PolicyCell label="Project capacity" value={`${operationsQ.data.capacity.project.active} active of ${operationsQ.data.capacity.project.limit}`} />
            <PolicyCell label="Global capacity" value={`${operationsQ.data.capacity.global.active} active of ${operationsQ.data.capacity.global.limit}`} />
          </div>
        </V2Section>
      ) : null}
    </V2Page>
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
    <V2Row
      meta={wave.id}
      title={wave.title}
      detail={`${tasks.filter((task) => task.bucket === "delivered").length} landed · ${tasks.filter((task) => task.bucket === "running" || task.bucket === "checking").length} still moving · ${tasks.filter((task) => task.bucket === "blocked" || task.bucket === "needsYou").length} held`}
      status={<V2Status tone={state === "Review again" || state === "Paused" ? "warn" : phaseTone(state)}>{state}</V2Status>}
      action={state === "Ready to integrate" ? <GitMerge size={16} className="text-info" /> : state === "Paused" ? <Pause size={16} className="text-warn" /> : state === "Review again" ? <ShieldAlert size={16} className="text-warn" /> : <Network size={16} className="text-faint" />}
    />
  );
}

function PolicyCell({ label, value }: { label: string; value: string }) {
  return <div className="bg-surface px-4 py-4"><V2Label>{label}</V2Label><div className="mt-2 text-[14px] font-medium text-ink">{value}</div></div>;
}
