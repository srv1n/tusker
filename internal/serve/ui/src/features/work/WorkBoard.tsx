/*
  Board presentation — tasks grouped into status columns (packet §4.4). Each
  card is a link into the task's contract document.
*/

import { useEffect, useRef } from "react";
import { Link } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { CapsuleChips } from "@/components/ui/capsule";
import { ReadinessChip } from "@/components/ui/chips";
import { statusLabel, statusTone, tone } from "@/components/ui/tone";
import type { TaskCapsule, TaskStatus } from "@/types/domain";
import { STATUS_COLUMNS, isBatchSelectable, selectableIds } from "@/features/work/work-utils";
import type { Selection } from "@/features/work/selection";
import { GateMarker } from "@/features/work/WorkParts";

const CHECK =
  "h-3.5 w-3.5 flex-none cursor-pointer accent-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40 disabled:cursor-not-allowed disabled:opacity-40";

/** Per-task checkbox; a sibling of the card Link so a click never navigates. */
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

/** Select-all-in-column checkbox; indeterminate when only some are on. */
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

function TaskCard({
  task,
  projectId,
  selection,
  disabled,
}: {
  task: TaskCapsule;
  projectId: string;
  selection: Selection;
  disabled: boolean;
}) {
  const selectable = isBatchSelectable(task);
  return (
    <div className="flex items-start gap-2">
      {selectable && (
        <div className="pt-3">
          <LeafCheck
            id={task.id}
            selection={selection}
            disabled={disabled}
            label={`Select ${task.id}`}
          />
        </div>
      )}
      <Link
        to="/p/$projectId/docs"
        params={{ projectId }}
        search={{ path: task.id }}
        className="group block flex-1 rounded-[9px] border border-line bg-raised p-3 transition-colors hover:border-muted/40 hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30"
      >
        <div className="mb-2 flex items-center justify-between gap-2">
          <Mono className="text-[10px] text-faint">{task.id}</Mono>
          {task.hasGate && <GateMarker />}
        </div>
        <div className="mb-2.5 line-clamp-2 text-[13.5px] font-medium leading-[1.3] text-ink">
          {task.title}
        </div>
        <div className="flex items-center gap-1.5">
          <CapsuleChips capsule={task} show={["priority", "risk"]} />
          {task.readiness !== "ready" && <ReadinessChip readiness={task.readiness} />}
          <Mono className="ml-auto text-[10px] text-faint">{task.epicId}</Mono>
        </div>
      </Link>
    </div>
  );
}

function Column({
  status,
  tasks,
  projectId,
  selection,
  disabled,
}: {
  status: TaskStatus;
  tasks: TaskCapsule[];
  projectId: string;
  selection: Selection;
  disabled: boolean;
}) {
  const colSelectableIds = selectableIds(tasks);
  return (
    <div>
      <div className="mb-3 flex items-center gap-2 px-0.5">
        <span className={cn("h-2 w-2 flex-none rounded-sm", tone[statusTone[status]].dot)} />
        <span className="text-[12.5px] font-semibold text-ink">{statusLabel[status]}</span>
        <Mono className="text-[11px] text-faint">{tasks.length}</Mono>
        {colSelectableIds.length > 0 && (
          <span className="ml-auto flex items-center">
            <GroupCheck
              ids={colSelectableIds}
              selection={selection}
              disabled={disabled}
              label={`Select all ${statusLabel[status]} tasks`}
            />
          </span>
        )}
      </div>
      <div className="flex flex-col gap-2.5">
        {tasks.length === 0 ? (
          <div className="rounded-lg border border-dashed border-line px-3 py-5 text-center font-mono text-[10.5px] text-fainter">
            empty
          </div>
        ) : (
          tasks.map((t) => (
            <TaskCard
              key={t.id}
              task={t}
              projectId={projectId}
              selection={selection}
              disabled={disabled}
            />
          ))
        )}
      </div>
    </div>
  );
}

export function WorkBoard({
  tasks,
  projectId,
  selection,
  disabled = false,
}: {
  tasks: TaskCapsule[];
  projectId: string;
  selection: Selection;
  disabled?: boolean;
}) {
  const byStatus = new Map<TaskStatus, TaskCapsule[]>();
  for (const s of STATUS_COLUMNS) byStatus.set(s, []);
  for (const t of tasks) byStatus.get(t.status)?.push(t);

  return (
    <div className="grid animate-rise grid-cols-2 items-start gap-3 sm:grid-cols-3 xl:grid-cols-6">
      {STATUS_COLUMNS.map((status) => (
        <Column
          key={status}
          status={status}
          tasks={byStatus.get(status) ?? []}
          projectId={projectId}
          selection={selection}
          disabled={disabled}
        />
      ))}
    </div>
  );
}
