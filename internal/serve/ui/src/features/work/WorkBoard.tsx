/*
  Board presentation — tasks grouped into status columns (packet §4.4). Each
  card is a link into the task's contract document.
*/

import { Link } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { CapsuleChips } from "@/components/ui/capsule";
import { ReadinessChip } from "@/components/ui/chips";
import { statusLabel, statusTone, tone } from "@/components/ui/tone";
import type { TaskCapsule, TaskStatus } from "@/types/domain";
import { STATUS_COLUMNS } from "@/features/work/work-utils";
import { GateMarker } from "@/features/work/WorkParts";

function TaskCard({ task, projectId }: { task: TaskCapsule; projectId: string }) {
  return (
    <Link
      to="/p/$projectId/docs"
      params={{ projectId }}
      search={{ path: task.id }}
      className="group block rounded-[9px] border border-line bg-raised p-3 transition-colors hover:border-muted/40 hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30"
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
  );
}

function Column({
  status,
  tasks,
  projectId,
}: {
  status: TaskStatus;
  tasks: TaskCapsule[];
  projectId: string;
}) {
  return (
    <div>
      <div className="mb-3 flex items-center gap-2 px-0.5">
        <span className={cn("h-2 w-2 flex-none rounded-sm", tone[statusTone[status]].dot)} />
        <span className="text-[12.5px] font-semibold text-ink">{statusLabel[status]}</span>
        <Mono className="text-[11px] text-faint">{tasks.length}</Mono>
      </div>
      <div className="flex flex-col gap-2.5">
        {tasks.length === 0 ? (
          <div className="rounded-lg border border-dashed border-line px-3 py-5 text-center font-mono text-[10.5px] text-fainter">
            empty
          </div>
        ) : (
          tasks.map((t) => <TaskCard key={t.id} task={t} projectId={projectId} />)
        )}
      </div>
    </div>
  );
}

export function WorkBoard({ tasks, projectId }: { tasks: TaskCapsule[]; projectId: string }) {
  const byStatus = new Map<TaskStatus, TaskCapsule[]>();
  for (const s of STATUS_COLUMNS) byStatus.set(s, []);
  for (const t of tasks) byStatus.get(t.status)?.push(t);

  return (
    <div className="grid animate-rise grid-cols-2 items-start gap-3 sm:grid-cols-3 xl:grid-cols-6">
      {STATUS_COLUMNS.map((status) => (
        <Column key={status} status={status} tasks={byStatus.get(status) ?? []} projectId={projectId} />
      ))}
    </div>
  );
}
