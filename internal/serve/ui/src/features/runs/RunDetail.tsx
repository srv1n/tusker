import { useEffect, useRef, useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import type { RunDetail as RunDetailData } from "@/types/domain";
import {
  interruptedRunReadbackComplete,
  useDaemon,
  useInterrupt,
  useAttempt,
  useRedrive,
  useRun,
  useRunAction,
  useTasks,
} from "@/lib/queries";
import { QueryBoundary, Skeleton, SkeletonRows } from "@/components/ui/states";
import { SectionLabel } from "@/components/ui/page";
import { Mono } from "@/components/ui/primitives";
import { RunHeader } from "@/features/runs/detail/RunHeader";
import { RunStats } from "@/features/runs/detail/RunStats";
import { AttemptTimeline } from "@/features/runs/detail/AttemptTimeline";
import { EventTail } from "@/features/runs/detail/EventTail";
import { SessionRecoveryControls } from "@/features/runs/detail/SessionRecoveryControls";
import { waitingForDaemonReason } from "@/features/runs/detail/helpers";
import { createRunActionLock } from "@/features/runs/detail/actionLock";
import { useConfirm } from "@/components/ui/action-feedback";
import { relativeTime } from "@/lib/time";
import { ApiError } from "@/lib/api";
import type { RunAction } from "@/types/domain";

const route = getRouteApi("/p/$projectId/runs/$taskId");
const ACTION_READBACK_TIMEOUT_MS = 60_000;

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
  const [pendingAction, setPendingAction] = useState<RunAction | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const actionSequence = useRef(0);
  const actionSettledSequence = useRef(0);
  const run = useRun(taskId, interrupt.data?.ok === true, projectId, pendingAction !== null);
  const tasks = useTasks(projectId);
  const daemon = useDaemon();
  const redrive = useRedrive(taskId, projectId);
  const runAction = useRunAction(taskId, projectId);
  const confirm = useConfirm();
  const [interruptConfirming, setInterruptConfirming] = useState(false);
  const runActionLock = useRef(createRunActionLock()).current;
  const awaitingInterruptReadback =
    interrupt.data?.ok === true && !interruptedRunReadbackComplete(run.data);
  const interruptBusy =
    interruptConfirming || interrupt.isPending || awaitingInterruptReadback;
  const actionBusy = interruptBusy || redrive.isPending || pendingAction !== null || runAction.isPending;

  useEffect(() => {
    if (!pendingAction) return;
    const sequence = actionSequence.current;
    const timeout = window.setTimeout(() => {
      if (actionSequence.current !== sequence) return;
      setPendingAction(null);
      setActionError("Result unknown — refresh the run before trying another action.");
    }, ACTION_READBACK_TIMEOUT_MS);
    return () => window.clearTimeout(timeout);
  }, [pendingAction]);

  const settleAction = (action: RunAction, sequence: number) => {
    void run.refetch().then((result) => {
      if (actionSequence.current !== sequence) return;
      if (result.isError || !result.data) {
        setActionError("Result unknown — refresh the run before trying another action.");
        return;
      }
      if (result.data.controls?.pending?.action === action || result.data.actionReadback?.action === action && result.data.actionReadback.state === "pending") return;
      setPendingAction(null);
    }).catch(() => {
      if (actionSequence.current === sequence) setActionError("Action submitted, but canonical run readback is unavailable; keeping it pending.");
    });
  };

  const onSessionAction = async (action: Exclude<RunAction, "reconnect">) => {
    if (actionBusy) return;
    if (action === "start_fresh" || action === "stop") {
      const confirmed = await confirm({
        title: action === "stop" ? `Stop ${taskId}` : `Start a fresh session for ${taskId}`,
        body: action === "stop"
          ? "Persists a durable stop intent and waits for canonical owner/process settlement before recovery is enabled."
          : "This creates a different native session after the current owner settles. Existing history, authority, and budgets stay attached to the job.",
        confirmLabel: action === "stop" ? "Stop run" : "Start fresh",
        tone: "danger",
      });
      if (!confirmed || actionBusy) return;
    }
    const sequence = ++actionSequence.current;
    setPendingAction(action);
    setActionError(null);
    runAction.mutate(action, {
      onError: (error) => {
        if (actionSequence.current !== sequence) return;
        setActionError(error instanceof Error ? error.message : String(error));
        if (error instanceof ApiError) setPendingAction(null);
      },
      onSuccess: (result) => {
        if (actionSequence.current === sequence) actionSettledSequence.current = sequence;
        if (result.refused || (!result.ok && !result.pending)) {
          setPendingAction(null);
          setActionError(result.reason || "The server refused this action.");
          return;
        }
        settleAction(action, sequence);
      },
    });
  };

  useEffect(() => {
    if (!pendingAction || run.isFetching || runAction.isPending) return;
    if (pendingAction === "reconnect" || actionSettledSequence.current !== actionSequence.current) return;
    const readbackPending = run.data?.controls?.pending?.action === pendingAction ||
      (run.data?.actionReadback?.action === pendingAction && run.data.actionReadback.state === "pending");
    if (run.data && !readbackPending) {
      actionSettledSequence.current = 0;
      setPendingAction(null);
    }
  }, [pendingAction, run.data, run.dataUpdatedAt, run.isFetching]);

  const onReconnect = () => {
    if (actionBusy) return;
    const sequence = ++actionSequence.current;
    setPendingAction("reconnect");
    setActionError(null);
    void run.refetch().then((result) => {
      if (actionSequence.current !== sequence) return;
      if (result.isError || !result.data) setActionError("Result unknown — refresh the run before trying another action.");
      else setPendingAction(null);
    }).catch((error) => {
      if (actionSequence.current === sequence) setActionError(error instanceof Error ? error.message : String(error));
    });
  };

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
        {/* Runs live on the overview; the back-link goes there. */}
        <Link
          to="/p/$projectId"
          params={{ projectId }}
          className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11.5px] text-faint transition-colors hover:text-ink"
        >
          <ArrowLeft size={13} strokeWidth={2} /> Overview
        </Link>

        <QueryBoundary q={run} loading={<RunDetailSkeleton />}>
          {(data: RunDetailData) => (
            <RunDetailContent
              data={data}
              projectId={projectId}
              capsule={tasks.data?.find((t) => t.id === data.taskId)}
              daemon={daemon.data}
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
              interruptBusy={interruptBusy}
              pendingAction={pendingAction}
              actionError={actionError}
              onReconnect={onReconnect}
              onAction={onSessionAction}
            />
          )}
        </QueryBoundary>
      </div>
    </div>
  );
}

function RunDetailContent({
  data,
  projectId,
  capsule,
  daemon,
  onInterrupt,
  onRetry,
  retry,
  interrupt,
  interruptBusy,
  pendingAction,
  actionError,
  onReconnect,
  onAction,
}: {
  data: RunDetailData;
  projectId: string;
  capsule?: Parameters<typeof RunHeader>[0]["capsule"];
  daemon?: Parameters<typeof waitingForDaemonReason>[1];
  onInterrupt: () => void;
  onRetry: () => void;
  retry: Parameters<typeof RunHeader>[0]["retry"];
  interrupt: Parameters<typeof RunHeader>[0]["interrupt"];
  interruptBusy: boolean;
  pendingAction: RunAction | null;
  actionError: string | null;
  onReconnect: () => void;
  onAction: (action: Exclude<RunAction, "reconnect">) => void;
}) {
  const [selectedAttemptId, setSelectedAttemptId] = useState<string>();
  const attempt = data.attempts.find((item) => (item.id ?? String(item.n)) === selectedAttemptId) ?? data.attempts.at(-1);
  const attemptId = attempt ? (attempt.id ?? String(attempt.n)) : undefined;
  const currentAttemptId = data.activeAttemptId ?? (data.attempts.at(-1) ? (data.attempts.at(-1)!.id ?? String(data.attempts.at(-1)!.n)) : undefined);
  const historical = Boolean(attemptId && currentAttemptId && attemptId !== currentAttemptId);
  const selectedAttemptQuery = useAttempt(historical ? attemptId : undefined, projectId);
  const events = historical ? (selectedAttemptQuery.data?.events ?? []) : data.events;
  const activity = historical ? undefined : data.activity ?? (hasDirectActivity(data) ? {
    captureState: data.activityCaptureState,
    captureReason: data.activityCaptureReason,
    heartbeatAt: data.lastHeartbeatAt,
    messageAt: data.lastMessageAt,
    toolAt: data.lastToolProgressAt,
    messageAgeSec: data.messageAgeSec,
    toolAgeSec: data.toolProgressAgeSec,
  } : undefined);
  const daemonWaitReason = waitingForDaemonReason(data, daemon);

  useEffect(() => {
    if (selectedAttemptId && data.attempts.some((item) => (item.id ?? String(item.n)) === selectedAttemptId)) return;
    setSelectedAttemptId(data.activeAttemptId ?? data.attempts.at(-1)?.id ?? (data.attempts.at(-1) ? String(data.attempts.at(-1)!.n) : undefined));
  }, [data.activeAttemptId, data.attempts, selectedAttemptId]);

  return (
    <div className="animate-rise">
      <RunHeader
        run={data}
        capsule={capsule}
        onInterrupt={onInterrupt}
        onRetry={onRetry}
        retry={retry}
        interrupt={interrupt}
        waitingForDaemonReason={daemonWaitReason}
      />

      <SessionRecoveryControls
        run={data}
        pendingAction={pendingAction}
        actionError={actionError}
        busy={interruptBusy}
        onReconnect={onReconnect}
        onAction={onAction}
      />

      <RunStats run={data} waitingForDaemon={daemonWaitReason !== null} />

      <OperatorFacts run={data} />
      <Delivery run={data} />

      <div className="grid grid-cols-1 items-start gap-8 lg:grid-cols-[300px_1fr]">
        <div className="min-w-0">
          <SectionLabel className="mb-3">Attempts</SectionLabel>
          <AttemptTimeline attempts={data.attempts} selectedAttemptId={attemptId} onSelect={(next) => setSelectedAttemptId(next.id ?? String(next.n))} />
          <WorkspacePath path={data.workspacePath} />
        </div>
        <EventTail
          events={events}
          liveness={data.liveness}
          sinceLastEventSec={data.sinceLastEventSec}
          waitingForDaemonReason={daemonWaitReason}
          activity={activity}
          historicalAttempt={historical}
        />
      </div>
    </div>
  );
}

function hasDirectActivity(run: RunDetailData): boolean {
  return Boolean(
    run.activityCaptureState ||
    run.activityCaptureReason ||
    run.lastMessageAt ||
    run.lastToolProgressAt ||
    run.messageAgeSec !== undefined ||
    run.toolProgressAgeSec !== undefined,
  );
}

function OperatorFacts({ run }: { run: RunDetailData }) {
  const command = run.resume?.command;
  return (
    <section className="mb-6 rounded-[10px] border border-line bg-raised p-4" data-run-operator-facts>
      <SectionLabel className="mb-3">Ownership &amp; resume</SectionLabel>
      <dl className="grid gap-3 text-[11px] sm:grid-cols-2 lg:grid-cols-4">
        <div><dt className="text-faint">authorized by</dt><dd className="font-mono text-ink">{run.authorization ? `${run.authorization.source} · ${run.authorization.actor} · ${relativeTime(run.authorization.created_at)}` : "authorization unavailable"}</dd></div>
        <div><dt className="text-faint">repository</dt><dd className="break-all font-mono text-ink">{run.identity?.registered_repo_path ?? run.identity?.repo_root ?? "registered repository unavailable"}</dd></div>
        <div><dt className="text-faint">workspace mode</dt><dd className="font-mono text-ink">{run.identity?.workspace_mode ?? run.workspaceMode ?? "unknown"}</dd></div>
        <div><dt className="text-faint">session</dt><dd className="break-all font-mono text-ink">{run.session?.session_ref ?? "session unavailable"}</dd></div>
      </dl>
      {command ? (
        <button type="button" onClick={() => void navigator.clipboard?.writeText(command)} className="mt-3 break-all rounded border border-line px-2 py-1 font-mono text-[10.5px] text-info" title="Copy resume command">{command}</button>
      ) : <p className="mt-3 text-[11px] text-faint">Resume unavailable: {run.resume?.reason ?? "session metadata is missing"}</p>}
    </section>
  );
}

function Delivery({ run }: { run: RunDetailData }) {
  const delivery = run.delivery;
  return (
    <section className="mb-6 rounded-[10px] border border-line bg-raised p-4" data-run-delivery>
      <SectionLabel className="mb-3">Delivery</SectionLabel>
      {!delivery?.summary && !delivery?.artifact ? <p className="text-[12px] text-faint">No deliverable recorded.</p> : (
        <><p className="text-[12px] text-ink">{delivery.summary || delivery.artifact}</p>{delivery.artifact && <Mono className="mt-2 block break-all text-[10.5px] text-info">{delivery.artifact}</Mono>}</>
      )}
      <div className="mt-3 border-t border-line-soft pt-3 text-[11px]"><span className="text-faint">acceptance verification · </span><span className="text-ink">{delivery?.verification || "not recorded"}</span><Mono className="ml-2 text-faint">proof {delivery?.proofStatus ?? "pending"}</Mono></div>
      {(run.outcome === "failed" || run.outcome === "interrupted") && <p className="mt-3 line-clamp-3 text-[11px] text-fail">{run.error || `${run.outcome}; retry or reclaim depends on current ownership policy`}</p>}
    </section>
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
