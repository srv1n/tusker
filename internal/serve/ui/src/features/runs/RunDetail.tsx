import { useRef, useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import type { RunDetail as RunDetailData } from "@/types/domain";
import {
  interruptedRunReadbackComplete,
  useDaemon,
  useInterrupt,
  useRedrive,
  useRun,
  useTasks,
} from "@/lib/queries";
import { QueryBoundary, Skeleton, SkeletonRows } from "@/components/ui/states";
import { SectionLabel } from "@/components/ui/page";
import { RunHeader } from "@/features/runs/detail/RunHeader";
import { RunStats } from "@/features/runs/detail/RunStats";
import { AttemptTimeline } from "@/features/runs/detail/AttemptTimeline";
import { EventTail } from "@/features/runs/detail/EventTail";
import { waitingForDaemonReason } from "@/features/runs/detail/helpers";
import { createRunActionLock } from "@/features/runs/detail/actionLock";
import { useConfirm } from "@/components/ui/action-feedback";

const route = getRouteApi("/p/$projectId/runs/$taskId");

/**
 * Run Detail (packet §4.3) — one run. Header carries the task capsule + run
 * state + actions; the body pairs an attempts timeline with a live event tail.
 * Only some tasks have run-detail fixtures; the rest resolve to the daemon/error
 * state via <QueryBoundary>, which is the intended graceful path.
 */
export function RunDetail() {
  const { projectId, taskId } = route.useParams();
  return <TaskRunDetail key={taskId} projectId={projectId} taskId={taskId} />;
}

function TaskRunDetail({ projectId, taskId }: { projectId: string; taskId: string }) {
  const interrupt = useInterrupt(taskId, projectId);
  const run = useRun(taskId, interrupt.data?.ok === true, projectId);
  const tasks = useTasks(projectId);
  const daemon = useDaemon();
  const redrive = useRedrive(taskId, projectId);
  const confirm = useConfirm();
  const [interruptConfirming, setInterruptConfirming] = useState(false);
  const runActionLock = useRef(createRunActionLock()).current;
  const awaitingInterruptReadback =
    interrupt.data?.ok === true && !interruptedRunReadbackComplete(run.data);
  const interruptBusy =
    interruptConfirming || interrupt.isPending || awaitingInterruptReadback;
  const actionBusy = interruptBusy || redrive.isPending;

  const onInterrupt = async () => {
    if (actionBusy || !runActionLock.tryAcquire("interrupt")) return;
    let submitted = false;
    setInterruptConfirming(true);
    try {
      const ok = await confirm({
        title: `Interrupt ${taskId}`,
        body: "Stops the current runner or cancels queued execution. Redrive stays disabled until canonical lease and process readback confirms the stop.",
        confirmLabel: "Interrupt run",
        tone: "danger",
      });
      if (ok) {
        submitted = true;
        interrupt.reset();
        interrupt.mutate(undefined, {
          onSettled: () => {
            runActionLock.release("interrupt");
          },
        });
      }
    } finally {
      if (!submitted) runActionLock.release("interrupt");
      setInterruptConfirming(false);
    }
  };

  const onRedrive = () => {
    if (actionBusy || !runActionLock.tryAcquire("redrive")) return;
    interrupt.reset();
    redrive.mutate(undefined, {
      onSettled: () => {
        runActionLock.release("redrive");
      },
    });
  };

  return (
    <div className="tk-scroll h-full overflow-y-auto">
      <div className="mx-auto w-full max-w-[1040px] px-6 pb-20 pt-6 sm:px-11">
        <Link
          to="/p/$projectId/runs"
          params={{ projectId }}
          className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11.5px] text-faint transition-colors hover:text-ink"
        >
          <ArrowLeft size={13} strokeWidth={2} /> Runs
        </Link>

        <QueryBoundary q={run} loading={<RunDetailSkeleton />}>
          {(data: RunDetailData) => {
            const capsule = tasks.data?.find((t) => t.id === data.taskId);
            const daemonWaitReason = waitingForDaemonReason(data, daemon.data);
            return (
              <div className="animate-rise">
                <RunHeader
                  run={data}
                  capsule={capsule}
                  onInterrupt={onInterrupt}
                  onRetry={onRedrive}
                  retry={{ pending: redrive.isPending, result: redrive.data ?? null, error: redrive.error }}
                  interrupt={{
                    confirming: interruptConfirming,
                    pending: interrupt.isPending,
                    awaitingReadback: awaitingInterruptReadback,
                    result: interrupt.data ?? null,
                    error: interrupt.error,
                  }}
                  waitingForDaemonReason={daemonWaitReason}
                />

                <RunStats run={data} waitingForDaemon={daemonWaitReason !== null} />

                <div className="grid grid-cols-1 items-start gap-8 lg:grid-cols-[300px_1fr]">
                  <div className="min-w-0">
                    <SectionLabel className="mb-3">Attempts</SectionLabel>
                    <AttemptTimeline attempts={data.attempts} />
                    <WorkspacePath path={data.workspacePath} />
                  </div>
                  <EventTail
                    events={data.events}
                    liveness={data.liveness}
                    sinceLastEventSec={data.sinceLastEventSec}
                    waitingForDaemonReason={daemonWaitReason}
                  />
                </div>
              </div>
            );
          }}
        </QueryBoundary>
      </div>
    </div>
  );
}

/** Workspace path, mono + link-styled; clicking copies the absolute path. */
function WorkspacePath({ path }: { path: string }) {
  return (
    <div className="mt-2 border-t border-line-soft pt-3">
      <div className="font-mono text-[10.5px] text-faint">workspace</div>
      <button
        type="button"
        title="Copy workspace path"
        onClick={() => void navigator.clipboard?.writeText(path)}
        className="mt-1 block break-all text-left font-mono text-[10.5px] leading-relaxed text-info hover:underline"
      >
        {path}
      </button>
    </div>
  );
}

/** Layout-matched loading state (skeletons, never spinners — packet §5). */
function RunDetailSkeleton() {
  return (
    <div>
      <Skeleton className="h-4 w-40" />
      <Skeleton className="mt-3 h-8 w-2/3" />
      <Skeleton className="mt-4 h-6 w-64" />
      <div className="mt-6 grid grid-cols-2 gap-px overflow-hidden rounded-[10px] border border-line bg-line sm:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="bg-raised px-4 py-3.5">
            <Skeleton className="h-2.5 w-12" />
            <Skeleton className="mt-2 h-6 w-16" />
          </div>
        ))}
      </div>
      <div className="mt-6 grid grid-cols-1 gap-8 lg:grid-cols-[300px_1fr]">
        <SkeletonRows rows={3} />
        <Skeleton className="h-[360px] w-full rounded-[10px]" />
      </div>
    </div>
  );
}
