/*
  Table presentation — one sortable @tanstack/react-table grouped into epic
  sections (packet §4.4: "Epics get header rows with rollup counts"). Sorting is
  headless (getSortedRowModel); rows render as links so a click opens the task's
  contract. Default sort is by epic, which yields the design's grouped list;
  sorting another column flattens into a plain sorted table.
*/

import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type Row,
  type SortingState,
} from "@tanstack/react-table";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { PriorityChip, RiskChip, StatusChip } from "@/components/ui/chips";
import { relativeTime } from "@/lib/time";
import type { EpicSummary, TaskCapsule } from "@/types/domain";
import {
  PRIORITY_RANK,
  RISK_RANK,
  STATUS_RANK,
  isBatchSelectable,
  resolveRollup,
  selectableIds,
} from "@/features/work/work-utils";
import type { Selection } from "@/features/work/selection";
import { GateMarker, SortIcon } from "@/features/work/WorkParts";

/** Shared 7-track grid so header and every row column-align. */
const GRID =
  "grid grid-cols-[minmax(190px,1fr)_112px_88px_80px_60px_76px_92px] items-center gap-3";

/** Fixed-width checkbox gutter kept identical across header, epic rows, and task rows. */
const GUTTER = "flex w-6 flex-none items-center justify-center";
const CHECK =
  "h-3.5 w-3.5 flex-none cursor-pointer accent-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40 disabled:cursor-not-allowed disabled:opacity-40";

/** Per-task checkbox. Sits in a gutter beside (not inside) the row Link, so a
 *  click never navigates. */
function LeafCheck({
  id,
  selection,
  disabled,
  label,
}: {
  id: string;
  selection: Selection;
  disabled?: boolean;
  label: string;
}) {
  return (
    <input
      type="checkbox"
      checked={selection.isSelected(id)}
      disabled={disabled}
      aria-label={label}
      onChange={() => selection.toggle(id)}
      onClick={(e) => e.stopPropagation()}
      className={CHECK}
    />
  );
}

/** Select-all-in-group checkbox; indeterminate when only some of `ids` are on. */
function GroupCheck({
  ids,
  selection,
  disabled,
  label,
}: {
  ids: string[];
  selection: Selection;
  disabled?: boolean;
  label: string;
}) {
  const ref = useRef<HTMLInputElement>(null);
  const selectedCount = ids.reduce((n, id) => n + (selection.isSelected(id) ? 1 : 0), 0);
  const all = ids.length > 0 && selectedCount === ids.length;
  const some = selectedCount > 0 && !all;
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = some;
  }, [some]);
  if (ids.length === 0) return null;
  return (
    <input
      ref={ref}
      type="checkbox"
      checked={all}
      disabled={disabled}
      aria-label={label}
      onChange={() => selection.setMany(ids, !all)}
      className={CHECK}
    />
  );
}

const columnHelper = createColumnHelper<TaskCapsule>();

const columns = [
  columnHelper.accessor("id", { header: "Task" }),
  columnHelper.accessor("status", {
    header: "Status",
    sortingFn: (a, b) => STATUS_RANK[a.original.status] - STATUS_RANK[b.original.status],
  }),
  columnHelper.accessor("priority", {
    header: "Priority",
    sortingFn: (a, b) => PRIORITY_RANK[a.original.priority] - PRIORITY_RANK[b.original.priority],
  }),
  columnHelper.accessor("risk", {
    header: "Risk",
    sortingFn: (a, b) => RISK_RANK[a.original.risk] - RISK_RANK[b.original.risk],
  }),
  columnHelper.accessor("hasGate", {
    id: "gate",
    header: "Gate",
    sortingFn: (a, b) => Number(b.original.hasGate) - Number(a.original.hasGate),
  }),
  columnHelper.accessor("epicId", { id: "epic", header: "Epic" }),
  columnHelper.accessor("updatedAt", {
    id: "updated",
    header: "Updated",
    sortingFn: (a, b) =>
      new Date(a.original.updatedAt).getTime() - new Date(b.original.updatedAt).getTime(),
  }),
];

function alignFor(columnId: string): string {
  if (columnId === "updated") return "justify-end text-right";
  if (columnId === "gate") return "justify-center";
  return "justify-start";
}

function TaskRow({
  task,
  projectId,
  grouped,
  selection,
  disabled,
}: {
  task: TaskCapsule;
  projectId: string;
  grouped: boolean;
  selection: Selection;
  disabled: boolean;
}) {
  return (
    <div className="flex items-center gap-2">
      <div className={GUTTER}>
        {isBatchSelectable(task) ? (
          <LeafCheck
            id={task.id}
            selection={selection}
            disabled={disabled}
            label={`Select ${task.id}`}
          />
        ) : null}
      </div>
      <Link
        to="/p/$projectId/docs"
        params={{ projectId }}
        search={{ path: task.id }}
        className={cn(
          GRID,
          "group min-w-0 flex-1 rounded-md border-b border-line-soft px-2.5 py-2 transition-colors last:border-b-0 hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent/30",
        )}
      >
        <div className="flex min-w-0 items-baseline gap-2">
          <Mono className="flex-none text-[10.5px] text-faint">{task.id}</Mono>
          <span className="truncate text-[13.5px] font-medium text-ink">{task.title}</span>
        </div>
        <div className="flex justify-start">
          <StatusChip status={task.status} />
        </div>
        <div className="flex justify-start">
          <PriorityChip priority={task.priority} />
        </div>
        <div className="flex justify-start">
          <RiskChip risk={task.risk} />
        </div>
        <div className="flex justify-center">
          {task.hasGate ? <GateMarker /> : <span className="text-fainter">·</span>}
        </div>
        <div className="flex min-w-0 justify-start">
          {grouped ? null : <Mono className="truncate text-[10.5px] text-faint">{task.epicId}</Mono>}
        </div>
        <div className="flex justify-end">
          <Mono className="text-[10.5px] text-faint">{relativeTime(task.updatedAt)}</Mono>
        </div>
      </Link>
    </div>
  );
}

function EpicHeader({
  title,
  epicId,
  epicsById,
  tasks,
  selection,
  disabled,
}: {
  title: string;
  epicId: string;
  epicsById: Map<string, EpicSummary>;
  tasks: TaskCapsule[];
  selection: Selection;
  disabled: boolean;
}) {
  const roll = resolveRollup(epicId, epicsById, tasks);
  return (
    <div className="mb-1 flex items-center gap-2 border-b border-line pb-2 pt-6 first:pt-1">
      <div className={GUTTER}>
        <GroupCheck
          ids={selectableIds(tasks)}
          selection={selection}
          disabled={disabled}
          label={`Select all reviewable tasks in ${epicId}`}
        />
      </div>
      <span className="text-faint" aria-hidden>
        ◇
      </span>
      <span className="text-[15px] font-semibold tracking-[-0.01em] text-ink">{title}</span>
      <Mono className="text-[10.5px] text-fainter">{epicId}</Mono>
      <span className="ml-auto font-mono text-[11px] text-faint">
        {roll.done}/{roll.total} done
        {roll.inProgress > 0 ? ` · ${roll.inProgress} active` : ""}
      </span>
    </div>
  );
}

export function WorkTable({
  tasks,
  projectId,
  epics,
  selection,
  disabled = false,
}: {
  tasks: TaskCapsule[];
  projectId: string;
  epics: EpicSummary[];
  selection: Selection;
  disabled?: boolean;
}) {
  const [sorting, setSorting] = useState<SortingState>([{ id: "epic", desc: false }]);

  const table = useReactTable({
    data: tasks,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });

  const epicsById = useMemo(
    () => new Map<string, EpicSummary>(epics.map((e) => [e.id, e])),
    [epics],
  );

  const rows = table.getSortedRowModel().rows;
  const primary = sorting[0]?.id;
  const grouped = !primary || primary === "epic";

  // When grouped by epic, cluster the (already-sorted) rows into contiguous
  // epic buckets so each epic gets one rollup header.
  const buckets = useMemo(() => {
    if (!grouped) return [] as Array<{ epicId: string; rows: Row<TaskCapsule>[] }>;
    const out: Array<{ epicId: string; rows: Row<TaskCapsule>[] }> = [];
    for (const r of rows) {
      const last = out[out.length - 1];
      if (last && last.epicId === r.original.epicId) last.rows.push(r);
      else out.push({ epicId: r.original.epicId, rows: [r] });
    }
    return out;
  }, [rows, grouped]);

  const headers = table.getHeaderGroups()[0]?.headers ?? [];
  const allSelectableIds = useMemo(() => selectableIds(tasks), [tasks]);

  return (
    <div className="tk-scroll animate-rise overflow-x-auto">
      <div className="min-w-[864px]">
        {/* Sortable header */}
        <div className="flex items-center gap-2 border-b border-line pb-2">
          <div className={GUTTER}>
            <GroupCheck
              ids={allSelectableIds}
              selection={selection}
              disabled={disabled}
              label="Select all reviewable tasks"
            />
          </div>
          <div className={cn(GRID, "min-w-0 flex-1 px-2.5")}>
            {headers.map((header) => {
              const id = header.column.id;
              const dir = header.column.getIsSorted();
              return (
                <div key={header.id} className={cn("flex min-w-0 items-center", alignFor(id))}>
                  <button
                    type="button"
                    onClick={header.column.getToggleSortingHandler()}
                    className={cn(
                      "inline-flex items-center gap-1 font-mono text-[9.5px] font-medium uppercase tracking-[0.14em] transition-colors",
                      dir ? "text-muted" : "text-fainter hover:text-muted",
                    )}
                  >
                    {flexRender(header.column.columnDef.header, header.getContext())}
                    <SortIcon dir={dir} />
                  </button>
                </div>
              );
            })}
          </div>
        </div>

        {/* Body */}
        <div className="pt-1">
          {grouped
            ? buckets.map((b) => {
                const bucketTasks = b.rows.map((r) => r.original);
                return (
                  <Fragment key={b.epicId}>
                    <EpicHeader
                      epicId={b.epicId}
                      title={epicsById.get(b.epicId)?.title ?? bucketTasks[0]?.epicTitle ?? b.epicId}
                      epicsById={epicsById}
                      tasks={bucketTasks}
                      selection={selection}
                      disabled={disabled}
                    />
                    {b.rows.map((r) => (
                      <TaskRow
                        key={r.original.id}
                        task={r.original}
                        projectId={projectId}
                        grouped
                        selection={selection}
                        disabled={disabled}
                      />
                    ))}
                  </Fragment>
                );
              })
            : rows.map((r) => (
                <TaskRow
                  key={r.original.id}
                  task={r.original}
                  projectId={projectId}
                  grouped={false}
                  selection={selection}
                  disabled={disabled}
                />
              ))}
        </div>
      </div>
    </div>
  );
}
