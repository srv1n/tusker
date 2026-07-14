/*
  Project Work browser (packet §4.4) — epics & tasks in two presentations behind
  one filter bar: a board grouped by status and a sortable table grouped by epic.
*/

import { useEffect, useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Flag, GitMerge, LayoutGrid, Loader2, Table2, X } from "lucide-react";
import { Button, IconButton, SegmentedControl, Select } from "@/components/ui/controls";
import { Mono } from "@/components/ui/primitives";
import { EmptyState, QueryBoundary } from "@/components/ui/states";
import { statusLabel } from "@/components/ui/tone";
import { useConfirm } from "@/components/ui/action-feedback";
import { api } from "@/lib/api";
import { useEpics, useProjects, useRuns, useTasks } from "@/lib/queries";
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

const route = getRouteApi("/p/$projectId/work");

type BatchAction = "close" | "land";
interface BatchItemResult {
  taskId: string;
  ok: boolean;
  reason: string;
}
interface BatchProgress {
  action: BatchAction;
  done: number;
  total: number;
}

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

/**
 * Floating batch bar for wave-boundary review. Fans the existing per-task
 * mutations out over the selection (sequentially, to spare the daemon), then
 * shows a compact pass/fail summary. `/api/review/batch` is still a stub, so
 * this is deliberately client-side fan-out over api.closeTask / api.landTask.
 */
function BatchBar({
  activeIds,
  progress,
  results,
  disabled,
  confirm,
  onRun,
  onClearSelection,
  onDismissResults,
}: {
  activeIds: string[];
  progress: BatchProgress | null;
  results: { action: BatchAction; items: BatchItemResult[] } | null;
  disabled: boolean;
  confirm: ReturnType<typeof useConfirm>;
  onRun: (action: BatchAction, ids: string[]) => void;
  onClearSelection: () => void;
  onDismissResults: () => void;
}) {
  if (results) {
    const failed = results.items.filter((i) => !i.ok);
    const passed = results.items.length - failed.length;
    const verb = results.action === "close" ? "accepted" : "landed";
    return (
      <div className="fixed bottom-6 left-1/2 z-40 w-[min(92vw,460px)] -translate-x-1/2">
        <div className="animate-rise rounded-xl border border-line bg-raised p-3 shadow-lg">
          <div className="mb-2 flex items-center justify-between gap-3">
            <div className="text-[13px] font-semibold text-ink">
              {passed} {verb}
              {failed.length > 0 ? ` · ${failed.length} failed` : ""}
            </div>
            <IconButton onClick={onDismissResults} aria-label="Dismiss batch results">
              <X size={14} />
            </IconButton>
          </div>
          {failed.length === 0 ? (
            <div className="text-[12px] text-pass">All selected tasks {verb} cleanly.</div>
          ) : (
            <ul className="tk-scroll max-h-40 space-y-1 overflow-y-auto">
              {failed.map((f) => (
                <li key={f.taskId} className="flex items-start gap-2 text-[12px] leading-snug">
                  <Mono className="flex-none text-[10.5px] text-faint">{f.taskId}</Mono>
                  <span className="text-fail">{f.reason}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    );
  }

  if (progress) {
    const label = progress.action === "close" ? "Accepting" : "Landing";
    return (
      <div className="fixed bottom-6 left-1/2 z-40 -translate-x-1/2">
        <div className="animate-rise flex items-center gap-2.5 rounded-xl border border-line bg-raised px-3.5 py-2.5 shadow-lg">
          <Loader2 size={14} className="animate-spin text-accent" />
          <Mono className="text-[12px] text-ink-soft">
            {label} {progress.done}/{progress.total}…
          </Mono>
        </div>
      </div>
    );
  }

  if (activeIds.length === 0) return null;
  const count = activeIds.length;
  const plural = count === 1 ? "" : "s";

  const acceptClose = async () => {
    const ok = await confirm({
      title: "Accept & close selected",
      body: `Accept and close ${count} completed task${plural}. This records acceptance and cannot be undone.`,
      confirmLabel: "Accept & close",
    });
    if (ok) onRun("close", activeIds);
  };

  const land = async () => {
    const ok = await confirm({
      title: "Land selected tasks",
      body: `Land ${count} task${plural} onto the base branch. Landing is irreversible.`,
      confirmLabel: `Land ${count}`,
      tone: "danger",
      typeToConfirm: "land",
    });
    if (ok) onRun("land", activeIds);
  };

  return (
    <div className="fixed bottom-6 left-1/2 z-40 -translate-x-1/2">
      <div className="animate-rise flex max-w-[calc(100vw-1.5rem)] flex-wrap items-center justify-center gap-2.5 rounded-xl border border-line bg-raised px-3 py-2 shadow-lg">
        <Mono className="pl-1 text-[12px] text-ink-soft">
          {count} selected
        </Mono>
        <span className="h-4 w-px flex-none bg-line" />
        <Button size="sm" variant="default" disabled={disabled} onClick={acceptClose}>
          <Check size={13} strokeWidth={2.25} />
          Accept &amp; close
        </Button>
        <Button size="sm" variant="danger" disabled={disabled} onClick={land}>
          <GitMerge size={13} strokeWidth={2.25} />
          Land selected
        </Button>
        <IconButton onClick={onClearSelection} aria-label="Clear selection" disabled={disabled}>
          <X size={14} />
        </IconButton>
      </div>
    </div>
  );
}

export function ProjectWork() {
  const { projectId } = route.useParams();
  const tasksQ = useTasks(projectId);
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
          action === "close" ? await api.closeTask(id, {}) : await api.landTask(id, {});
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
            // The batch acts on every selected task still in a landable state,
            // even one currently filtered out of view (a wave can span epics).
            const selectableSet = new Set(
              allTasks.filter(isBatchSelectable).map((t) => t.id),
            );
            const activeIds = [...selection.selectedIds].filter((id) => selectableSet.has(id));
            return (
              <>
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
