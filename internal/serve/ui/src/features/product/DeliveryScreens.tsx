import { useMemo } from "react";
import { useParams } from "@tanstack/react-router";
import { TaskStateBadge } from "@/components/ui/chips";
import { useEpics, useFactoryOperations, useTasks, useWaves } from "@/lib/queries";
import type { EpicSummary, TaskCapsule } from "@/types/domain";
import {
  ProductEmpty,
  ProductLabel,
  ProductLoading,
  ProductPage,
  ProductRow,
  ProductSection,
  ProductUnavailable,
} from "./shared";

type ProductRouteParams = { projectId?: string };

function useProjectId(): string {
  return (useParams({ strict: false }) as ProductRouteParams).projectId ?? "";
}

function queryFailure(...errors: Array<unknown>): unknown {
  return errors.find(Boolean);
}

export function Epics() {
  const projectId = useProjectId();
  const epicsQ = useEpics(projectId);
  const tasksQ = useTasks(projectId);
  const rows = useMemo(() => deriveEpics(epicsQ.data ?? [], tasksQ.data ?? []), [epicsQ.data, tasksQ.data]);
  const error = queryFailure(epicsQ.error, tasksQ.error);
  const loading = epicsQ.isLoading || tasksQ.isLoading;

  return (
    <ProductPage title="Epics" eyebrow="Project outcomes" intro="Outcome groups and their delivery state. Tasks remain a drill-down, not the primary unit of progress." wide>
      {error ? <ProductUnavailable>Could not load epic progress. {error instanceof Error ? error.message : "Try refreshing this project."}</ProductUnavailable> : null}
      {loading ? <ProductLoading rows={4} /> : null}
      {!loading && !error && rows.length === 0 ? <ProductEmpty title="No epics yet" detail="Epics appear when project work is grouped under an outcome." /> : null}
      {!loading && rows.length > 0 ? <div className="border-t-2 border-ink">{rows.map((epic) => <ProductRow key={epic.id} meta={epic.id} title={epic.title} detail={`${epic.counts} · ${epic.total} tasks`} />)}</div> : null}
    </ProductPage>
  );
}

/** Per-epic task counts by each task's displayed state label. */
function deriveEpics(epics: EpicSummary[], tasks: TaskCapsule[]) {
  const known = new Map(epics.map((epic) => [epic.id, epic]));
  const ids = new Set([...known.keys(), ...tasks.map((task) => task.epicId)]);
  return [...ids].map((id) => {
    const tasksForEpic = tasks.filter((task) => task.epicId === id);
    const byLabel = new Map<string, number>();
    for (const task of tasksForEpic) byLabel.set(task.state.label, (byLabel.get(task.state.label) ?? 0) + 1);
    return {
      id,
      title: known.get(id)?.title ?? tasksForEpic[0]?.epicTitle ?? id,
      total: tasksForEpic.length,
      counts: [...byLabel].map(([label, count]) => `${count} ${label.toLowerCase()}`).join(" · ") || "No tasks",
      open: tasksForEpic.filter((task) => task.state.next_actor === "you").length,
    };
  }).sort((left, right) => right.open - left.open || left.title.localeCompare(right.title));
}

export function Trains() {
  const projectId = useProjectId();
  const wavesQ = useWaves(projectId);
  const operationsQ = useFactoryOperations(projectId);
  const rows = wavesQ.data ?? [];
  const error = queryFailure(wavesQ.error, operationsQ.error);
  const loading = wavesQ.isLoading || operationsQ.isLoading;

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
            {rows.map((wave) => <ProductRow key={wave.id} meta={wave.id} title={wave.title} detail={wave.state.reason} status={<TaskStateBadge state={wave.state} />} />)}
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

function PolicyCell({ label, value }: { label: string; value: string }) {
  return <div className="bg-surface px-4 py-4"><ProductLabel>{label}</ProductLabel><div className="mt-2 text-[14px] font-medium text-ink">{value}</div></div>;
}
