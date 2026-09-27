import { cn } from "@/lib/cn";
import { Chip } from "@/components/ui/primitives";
import {
  gateKindLabel,
  gateKindTone,
  outcomeLabelOf,
  outcomeToneOf,
  priorityTone,
  proofTone,
  riskTone,
  statusLabelOf,
  statusToneOf,
  taskStateTone,
} from "@/components/ui/tone";
import type {
  GateKind,
  Priority,
  ProofStatus,
  Risk,
  RunOutcome,
  Runner,
  TaskState,
} from "@/types/domain";

/**
 * The one task/wave state display. Label and reason come from the server's
 * state record; `withReason` shows the reason beside the label, otherwise it
 * is the tooltip.
 */
export function TaskStateBadge({ state, withReason = false, className }: { state: TaskState; withReason?: boolean; className?: string }) {
  const chip = (
    <Chip tone={taskStateTone[state.state] ?? "neutral"} variant="soft" className={withReason ? undefined : className}>
      <span data-task-state={state.state} title={state.reason || undefined}>{state.label}</span>
    </Chip>
  );
  if (!withReason || !state.reason) return chip;
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-2", className)}>
      {chip}
      <span className="min-w-0 truncate text-[12px] text-muted" title={state.reason}>{state.reason}</span>
    </span>
  );
}

export function StatusChip({ status }: { status: string }) {
  return (
    <Chip tone={statusToneOf(status)} variant="soft">
      {statusLabelOf(status)}
    </Chip>
  );
}

export function RiskChip({ risk }: { risk: Risk }) {
  return (
    <Chip tone={riskTone[risk]} variant="outline" mono>
      {risk}
    </Chip>
  );
}

export function PriorityChip({ priority }: { priority: Priority }) {
  return (
    <Chip tone={priorityTone[priority]} variant="outline" mono>
      {priority}
    </Chip>
  );
}

export function GateKindChip({ kind }: { kind: GateKind }) {
  return (
    <Chip tone={gateKindTone[kind]} variant="soft">
      {gateKindLabel[kind]}
    </Chip>
  );
}

/**
 * Renders any run outcome — known or one the API adds later. Both the tone and
 * the label resolve generically so a new outcome value never renders blank
 * Outcome is an open enum, not a closed switch.
 */
export function OutcomeChip({ outcome }: { outcome: RunOutcome }) {
  return (
    <Chip tone={outcomeToneOf(outcome)} variant="soft">
      {outcomeLabelOf(outcome)}
    </Chip>
  );
}

export function ProofChip({ proof }: { proof: ProofStatus }) {
  const label = proof === "pass" ? "pass" : proof === "fail" ? "fail" : "pending";
  return (
    <Chip tone={proofTone[proof]} variant="soft" mono>
      {label}
    </Chip>
  );
}

/** Runner identity — codex vs claude, kept visually distinct and quiet. */
export function RunnerBadge({ runner }: { runner: Runner }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md border border-line px-1.5 py-0.5 text-[11px] font-medium",
        runner === "codex" ? "text-accent" : "text-info",
      )}
    >
      <span className={cn("h-1.5 w-1.5 rounded-full", runner === "codex" ? "bg-accent" : "bg-info")} />
      {runner}
    </span>
  );
}
