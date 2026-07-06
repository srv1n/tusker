import { Link } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { PriorityChip, ReadinessChip, RiskChip, StatusChip } from "@/components/ui/chips";
import type { TaskCapsule } from "@/types/domain";

/** Mono task/decision id that links into its detail. */
export function TaskRef({
  id,
  projectId,
  className,
}: {
  id: string;
  projectId: string;
  className?: string;
}) {
  return (
    <Link
      to="/p/$projectId/runs/$taskId"
      params={{ projectId, taskId: id }}
      className={cn("font-mono text-[12px] tabular text-muted hover:text-info hover:underline", className)}
    >
      {id}
    </Link>
  );
}

/** The capsule chip set (packet §8.2): status · priority · risk · readiness. */
export function CapsuleChips({
  capsule,
  show = ["status", "priority", "risk"],
}: {
  capsule: Pick<TaskCapsule, "status" | "priority" | "risk" | "readiness">;
  show?: Array<"status" | "priority" | "risk" | "readiness">;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {show.includes("status") && <StatusChip status={capsule.status} />}
      {show.includes("readiness") && <ReadinessChip readiness={capsule.readiness} />}
      {show.includes("priority") && <PriorityChip priority={capsule.priority} />}
      {show.includes("risk") && <RiskChip risk={capsule.risk} />}
    </div>
  );
}

/** Small "blocks N" indicator used to rank needs / show gate weight. */
export function BlockingBadge({ count }: { count: number }) {
  if (count <= 0) return null;
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-warn-soft px-2 py-0.5 text-[11px] font-medium text-warn">
      blocks <Mono className="font-semibold">{count}</Mono>
    </span>
  );
}
