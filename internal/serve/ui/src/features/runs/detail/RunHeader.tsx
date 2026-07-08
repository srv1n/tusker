import type { RunDetail, TaskCapsule } from "@/types/domain";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { Button } from "@/components/ui/controls";
import { OutcomeChip, RunnerBadge } from "@/components/ui/chips";
import { CapsuleChips } from "@/components/ui/capsule";
import { LivenessIndicator } from "@/components/ui/liveness";
import { isLiveHeaderRun } from "@/features/runs/detail/helpers";

/**
 * Run-detail header (design §07): task id + serif title, the task capsule chips,
 * a runner/model/lane/state meta line with liveness, and the run actions.
 * Interrupt is destructive (danger) and only enabled while the run is active;
 * Retry uses the design's emphasized solid-dark treatment (primary).
 */
export function RunHeader({
  run,
  capsule,
  onInterrupt,
  onRetry,
}: {
  run: RunDetail;
  capsule?: TaskCapsule;
  onInterrupt: () => void;
  onRetry: () => void;
}) {
  const live = isLiveHeaderRun(run);
  const active = live || run.outcome === "retry-queued";
  return (
    <header className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div className="min-w-0">
        <Mono className="text-[11.5px] text-faint">{run.taskId}</Mono>
        <h1 className="mt-1 font-serif text-[26px] font-semibold leading-[1.05] tracking-[-0.02em] text-ink sm:text-[28px]">
          {run.taskTitle}
        </h1>

        {capsule && (
          <div className="mt-3">
            <CapsuleChips capsule={capsule} show={["status", "priority", "risk"]} />
          </div>
        )}

        <div className="mt-3 flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-[11px]">
          <RunnerBadge runner={run.runner} />
          <Mono className="text-muted">{run.model}</Mono>
          <span className="text-fainter">·</span>
          <Mono className="text-muted">lane {run.lane}</Mono>
          <span className="text-fainter">·</span>
          <OutcomeChip outcome={run.outcome} />
          {live && (
            <>
              <LivenessIndicator liveness={run.liveness} sinceSec={run.sinceLastEventSec} />
              <span className="text-fainter">·</span>
            </>
          )}
          <Mono className={cn(run.leaseState === "expired" ? "text-fail" : "text-muted")}>
            lease {run.leaseState}
          </Mono>
        </div>
      </div>

      <div className="flex flex-none items-center gap-2.5">
        <Button variant="danger" onClick={onInterrupt} disabled={!active}>
          Interrupt
        </Button>
        <Button variant="primary" onClick={onRetry}>
          Retry
        </Button>
      </div>
    </header>
  );
}
