import type { RunAction, RunActionCapability, RunDetail } from "@/types/domain";
import { Button } from "@/components/ui/controls";
import { SectionLabel } from "@/components/ui/page";
import { Mono } from "@/components/ui/primitives";
import { cn } from "@/lib/cn";
import { isInterruptibleRun } from "@/features/runs/detail/helpers";

const ACTION_LABEL: Record<RunAction, string> = {
  reconnect: "Reconnect",
  continue: "Continue existing session",
  recover_context: "Recover from saved context",
  pause: "Pause",
  stop: "Stop",
  start_fresh: "Start fresh",
};

const ACTION_ORDER: RunAction[] = ["reconnect", "continue", "recover_context", "pause", "stop", "start_fresh"];

export function SessionRecoveryControls({
  run,
  pendingAction,
  actionError,
  busy,
  onReconnect,
  onAction,
}: {
  run: RunDetail;
  pendingAction?: RunAction | null;
  actionError?: string | null;
  busy?: boolean;
  onReconnect: () => void;
  onAction: (action: Exclude<RunAction, "reconnect">) => void;
}) {
  const capabilities = ACTION_ORDER.map((action) => capabilityFor(action, run));
  const checkpoint = run.controls?.checkpoint ?? run.checkpoint;
  const pending = pendingAction ?? run.controls?.pending?.action;
  return (
    <section className="mb-6 rounded-[10px] border border-line bg-raised p-4" data-run-session-controls>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <SectionLabel>Session recovery</SectionLabel>
          <p className="mt-1 text-[12px] text-muted">Reconnect observes the recorded owner. Continue and recovery create an explicitly recorded next attempt.</p>
        </div>
        {pending && <Mono className="text-[10.5px] text-info">{ACTION_LABEL[pending as RunAction] ?? pending} pending · awaiting canonical readback</Mono>}
      </div>

      <div className="mt-4 flex flex-wrap gap-2">
        {capabilities.map((capability) => {
          const action = capability.action as RunAction;
          const actionPending = pending === action;
          const disabled = Boolean(pending) || Boolean(busy) || !capability.available;
          const handler = action === "reconnect" ? onReconnect : () => onAction(action as Exclude<RunAction, "reconnect">);
          return (
            <Button
              key={action}
              type="button"
              variant={action === "start_fresh" || action === "stop" ? "danger" : action === "reconnect" ? "ghost" : "default"}
              disabled={disabled}
              title={capability.reason ?? ACTION_LABEL[action]}
              aria-label={ACTION_LABEL[action]}
              onClick={handler}
            >
              {actionPending ? `${ACTION_LABEL[action]}…` : ACTION_LABEL[action]}
            </Button>
          );
        })}
      </div>

      <div className="mt-3 grid gap-1.5 text-[10.5px] text-muted sm:grid-cols-2 lg:grid-cols-3">
        {capabilities.map((capability) => (
          <div key={capability.action} className={cn("rounded border border-line-soft px-2 py-1.5", capability.available ? "text-ink-soft" : "text-faint")}>
            <span className="font-medium">{ACTION_LABEL[capability.action as RunAction] ?? capability.action}</span>{" "}
            <span>{capability.available ? capability.provider ? `supported by ${capability.provider}` : "available" : capability.reason ?? "unsupported by the current provider"}</span>
          </div>
        ))}
      </div>

      {checkpoint && (checkpoint.completedAction || checkpoint.unresolvedAction || checkpoint.operation) && (
        <div className="mt-3 border-t border-line-soft pt-3 text-[11px] text-muted">
          <span className="text-faint">checkpoint · </span>
          {checkpoint.completedAction && <span>completed {checkpoint.completedAction}</span>}
          {checkpoint.unresolvedAction && <span>{checkpoint.completedAction ? " · " : ""}unresolved {checkpoint.unresolvedAction}</span>}
          {checkpoint.operation && <span>{checkpoint.completedAction || checkpoint.unresolvedAction ? " · " : ""}{checkpoint.operation}</span>}
          {checkpoint.reason && <span className="text-warn"> · {checkpoint.reason}</span>}
        </div>
      )}
      {actionError && <p className="mt-3 text-[11px] text-warn">{actionError}</p>}
      {run.controls?.readback?.reason && <p className="mt-3 text-[11px] text-muted">Last control readback: {run.controls.readback.reason}</p>}
      <p className="mt-3 text-[10.5px] text-faint">Start fresh creates a different native session after the current owner settles; existing history and authority remain attached to this job.</p>
    </section>
  );
}

function capabilityFor(action: RunAction, run: RunDetail): RunActionCapability {
  const source = run.controls?.capabilities ?? run.capabilities ?? [];
  const advertised = source.find((item) => item.action === action);
  if (advertised) return advertised;

  // Compatibility fallback for the pre-control API. Reconnect is observation;
  // native continue follows the existing resume capability, while context and
  // fresh actions use conservative settled-state gates until richer readback
  // is available.
  if (action === "reconnect") {
    return { action, available: Boolean(run.activeAttemptId || !run.terminal), reason: "Read the current canonical attempt; reconnect launches nothing." };
  }
  if (action === "continue") {
    return { action, available: run.resume?.supported === true, reason: run.resume?.reason ?? "Native continuation is unavailable for this session." };
  }
  if (action === "stop") {
    const available = isInterruptibleRun(run);
    return {
      action,
      available,
      reason: available
        ? "Persists a durable stop intent and waits for owner/process settlement."
        : "Stop requires a matching owner or queued process in canonical readback.",
    };
  }
  if (action === "pause") return { action, available: false, reason: "Pause is unavailable until the provider acknowledges a durable pause capability." };
  if (action === "recover_context") {
    const available = run.outcome === "outcome-unknown" && !isInterruptibleRun(run);
    return {
      action,
      available,
      reason: available
        ? "Creates a fresh native session while preserving the uncertain attempt for audit."
        : "Saved-context recovery requires an unknown settled outcome and current authorization.",
    };
  }
  const available = !isInterruptibleRun(run);
  return {
    action,
    available,
    reason: available
      ? "Creates a different native session after settlement; history and authority remain attached."
      : "Start fresh requires a settled run and canonical owner/process readback.",
  };
}
