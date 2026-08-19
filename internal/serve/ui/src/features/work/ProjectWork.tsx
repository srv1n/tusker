/*
  Project Work browser (packet §4.4) — epics & tasks in two presentations behind
  one filter bar: a board grouped by status and a sortable table grouped by epic.
*/

import { useEffect, useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Flag, LayoutGrid, Table2, X } from "lucide-react";
import { Button, SegmentedControl, Select } from "@/components/ui/controls";
import { Mono } from "@/components/ui/primitives";
import { EmptyState, QueryBoundary } from "@/components/ui/states";
import { statusLabel } from "@/components/ui/tone";
import { useConfirm } from "@/components/ui/action-feedback";
import { api } from "@/lib/api";
import { useEpics, useProjects, useReviewBatch, useRuns, useTasks } from "@/lib/queries";
import type { Risk, TaskCapsule } from "@/types/domain";
import {
  applyFilters,
  EMPTY_FILTERS,
  epicsInTasks,
  filtersActive,
  isBatchSelectable,
  RISK_VALUES,
  projectLiveExecution,
  STATUS_COLUMNS,
  type RiskFilter,
  type StatusFilter,
  type WorkFilters,
  type WorkView,
} from "@/features/work/work-utils";
import { useSelection } from "@/features/work/selection";
import { FilterPill } from "@/features/work/WorkParts";
import { WorkBoard } from "@/features/work/WorkBoard";
import { WorkTable } from "@/features/work/WorkTable";
import { BatchBar, type BatchAction, type BatchItemResult, type BatchProgress, WaveReviewGroups } from "@/features/work/WaveReview";

const route = getRouteApi("/p/$projectId/work");

/**
 * Mirror the invalidations the per-task hooks fire on settle (see
 * invalidateOperatorState in lib/queries.ts) so the board reflects a batch of
 * closes/lands the moment it finishes. Prefix keys match every project variant.
 */
const BATCH_INVALIDATE_KEYS = [
  "projects",
  "needs",
  "tasks",
  "runs",
  "waves",
  "review",
  "gates",
  "evidence",
  "decisions",
  "epics",
  "feedback",
  "daemon",
];

function invalidateWorkQueries(qc: ReturnType<typeof useQueryClient>) {
  for (const key of BATCH_INVALIDATE_KEYS) void qc.invalidateQueries({ queryKey: [key] });
}

const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

function FilterBar({
  tasks,
  filters,
  setFilters,
  resultCount,
}: {
  tasks: TaskCapsule[];
  filters: WorkFilters;
  setFilters: (f: WorkFilters) => void;
  resultCount: number;
}) {
  const epics = epicsInTasks(tasks);
  const toggleEpic = (id: string) =>
    setFilters({
      ...filters,
      epics: filters.epics.includes(id)
        ? filters.epics.filter((e) => e !== id)
        : [...filters.epics, id],
    });

  return (
    <div className="mb-5 flex flex-wrap items-center gap-x-2 gap-y-2.5 border-b border-line pb-4">
      {/* Epic pills */}
      <div className="flex flex-wrap items-center gap-1.5">
        <FilterPill active={filters.epics.length === 0} onClick={() => setFilters({ ...filters, epics: [] })}>
          All epics
        </FilterPill>
        {epics.map((e) => (
          <FilterPill key={e.id} active={filters.epics.includes(e.id)} onClick={() => toggleEpic(e.id)}>
            {e.id}
          </FilterPill>
        ))}
      </div>

      <div className="ml-auto flex flex-wrap items-center gap-2">
        <Select
          aria-label="Choose active or discarded work"
          value={filters.visibility}
          onChange={(e) => setFilters({ ...filters, visibility: e.target.value as WorkFilters["visibility"] })}
        >
          <option value="active">Active work</option>
          <option value="discarded">Discarded history</option>
        </Select>

        <Select
          aria-label="Filter by status"
          value={filters.status}
          onChange={(e) => setFilters({ ...filters, status: e.target.value as StatusFilter })}
        >
          <option value="all">All statuses</option>
          {STATUS_COLUMNS.map((s) => (
            <option key={s} value={s}>
              {statusLabel[s]}
            </option>
          ))}
        </Select>

        <Select
          aria-label="Filter by risk"
          value={filters.risk}
          onChange={(e) => setFilters({ ...filters, risk: e.target.value as RiskFilter })}
        >
          <option value="all">All risk</option>
          {RISK_VALUES.map((r: Risk) => (
            <option key={r} value={r}>
              {cap(r)} risk
            </option>
          ))}
        </Select>

        <Button
          size="sm"
          variant={filters.gateOnly ? "primary" : "default"}
          onClick={() => setFilters({ ...filters, gateOnly: !filters.gateOnly })}
          aria-pressed={filters.gateOnly}
        >
          <Flag size={12} strokeWidth={2.25} />
          Human gate
        </Button>

        <Mono className="pl-1 text-[11px] text-faint">{resultCount} shown</Mono>

        {filtersActive(filters) && (
          <Button size="sm" variant="ghost" onClick={() => setFilters(EMPTY_FILTERS)}>
            <X size={12} strokeWidth={2.25} />
            Clear
          </Button>
        )}
      </div>
    </div>
  );
}

export function ProjectWork() {
  const { projectId } = route.useParams();
  const tasksQ = useTasks(projectId);
  const reviewBatchQ = useReviewBatch(projectId);
  const runsQ = useRuns(projectId);
  const epicsQ = useEpics(projectId);
  const projectsQ = useProjects();
  const qc = useQueryClient();
  const confirm = useConfirm();
  const selection = useSelection();

  const [view, setView] = useState<WorkView>("board");
  const [filters, setFilters] = useState<WorkFilters>(EMPTY_FILTERS);
  const [progress, setProgress] = useState<BatchProgress | null>(null);
  const [results, setResults] = useState<{ action: BatchAction; items: BatchItemResult[] } | null>(
    null,
  );

  // Selection and any lingering results are project-scoped; reset when the
  // operator switches projects so a stale wave can't be acted on elsewhere.
  const { clear: clearSelection } = selection;
  useEffect(() => {
    clearSelection();
    setResults(null);
  }, [projectId, clearSelection]);

  const running = progress !== null;

  const runBatch = async (action: BatchAction, ids: string[]) => {
    setResults(null);
    setProgress({ action, done: 0, total: ids.length });
    const items: BatchItemResult[] = [];
    for (const id of ids) {
      try {
        const res =
          action === "close" ? await api.closeTask(id, {}, projectId) : await api.landTask(id, {}, projectId);
        items.push({ taskId: id, ok: res.ok && !res.refused, reason: res.reason });
      } catch (err) {
        items.push({
          taskId: id,
          ok: false,
          reason: err instanceof Error ? err.message : "request failed",
        });
      }
      setProgress({ action, done: items.length, total: ids.length });
    }
    invalidateWorkQueries(qc);
    clearSelection();
    setProgress(null);
    setResults({ action, items });
  };

  const projectName = projectsQ.data?.find((p) => p.id === projectId)?.name ?? projectId;
  const epics = epicsQ.data ?? [];

  return (
    <div className="tk-scroll h-full overflow-y-auto">
      <div className="mx-auto w-full max-w-[1240px] px-4 pb-20 pt-[30px] sm:px-8 lg:px-11">
        <div className="mb-1.5 font-mono text-[11px] text-faint">◇ {projectName}</div>
        <header className="mb-5 flex items-end justify-between gap-4">
          <h1 className="font-serif text-[30px] font-semibold tracking-[-0.02em] text-ink">Work</h1>
          <SegmentedControl
            value={view}
            onChange={setView}
            options={[
              {
                value: "board",
                label: (
                  <span className="inline-flex items-center gap-1.5">
                    <LayoutGrid size={13} strokeWidth={2} />
                    Board
                  </span>
                ),
              },
              {
                value: "table",
                label: (
                  <span className="inline-flex items-center gap-1.5">
                    <Table2 size={13} strokeWidth={2} />
                    Table
                  </span>
                ),
              },
            ]}
          />
        </header>

        <QueryBoundary q={tasksQ}>
          {(durableTasks) => {
            const allTasks = projectLiveExecution(durableTasks, runsQ.data ?? []);
            const filtered = applyFilters(allTasks, filters);
            // Keep wave selections across filters; each action below narrows
            // the shared selection to the canonical status it can mutate.
            const selectableSet = new Set(
              allTasks.filter(isBatchSelectable).map((t) => t.id),
            );
            const selectedTasks = [...selection.selectedIds]
              .map((id) => allTasks.find((task) => task.id === id))
              .filter((task): task is TaskCapsule => task !== undefined && selectableSet.has(task.id));
            const activeIds = selectedTasks.map((task) => task.id);
            const closeIds = selectedTasks.filter((task) => task.status === "review").map((task) => task.id);
            const landIds = selectedTasks.filter((task) => task.status === "done").map((task) => task.id);
            return (
              <>
                <WaveReviewGroups
                  batch={reviewBatchQ.data ?? { waves: [], unwaved: [] }}
                  disabled={running}
                  onSelectWave={(wave) => {
                    clearSelection();
                    selection.setMany(wave.members.filter(isBatchSelectable).map((task) => task.id), true);
                  }}
                />
                <FilterBar
                  tasks={allTasks}
                  filters={filters}
                  setFilters={setFilters}
                  resultCount={filtered.length}
                />
                {allTasks.length === 0 ? (
                  <EmptyState
                    title="No tasks yet"
                    hint="Tasks appear here once epics are broken down into task contracts."
                  />
                ) : filtered.length === 0 ? (
                  <EmptyState
                    icon={<Flag size={22} strokeWidth={1.5} />}
                    title="No tasks match these filters"
                    hint="Loosen a filter to see more of the backlog."
                    action={
                      <Button size="sm" onClick={() => setFilters(EMPTY_FILTERS)}>
                        Clear filters
                      </Button>
                    }
                  />
                ) : view === "board" && filters.visibility === "active" ? (
                  <WorkBoard
                    tasks={filtered}
                    projectId={projectId}
                    selection={selection}
                    disabled={running}
                  />
                ) : (
                  <WorkTable
                    tasks={filtered}
                    projectId={projectId}
                    epics={epics}
                    selection={selection}
                    disabled={running}
                  />
                )}
                <BatchBar
                  activeIds={activeIds}
                  closeIds={closeIds}
                  landIds={landIds}
                  progress={progress}
                  results={results}
                  disabled={running}
                  confirm={confirm}
                  onRun={runBatch}
                  onClearSelection={clearSelection}
                  onDismissResults={() => setResults(null)}
                />
              </>
            );
          }}
        </QueryBoundary>
      </div>
    </div>
  );
}
