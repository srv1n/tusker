import { cn } from "@/lib/cn";
import { Chip } from "@/components/ui/primitives";
import {
  gateKindLabel,
  gateKindTone,
  outcomeLabelOf,
  outcomeToneOf,
  priorityTone,
  proofTone,
  readinessLabelOf,
  readinessToneOf,
  riskTone,
  statusLabelOf,
  statusToneOf,
} from "@/components/ui/tone";
import type {
  GateKind,
  Priority,
  ProofStatus,
  Risk,
  RunOutcome,
  Runner,
} from "@/types/domain";

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

export function ReadinessChip({ readiness }: { readiness: string }) {
  return (
    <Chip tone={readinessToneOf(readiness)} variant="soft">
      {readinessLabelOf(readiness)}
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
 * (SRV-T-0016: outcome is an open enum, not a closed switch).
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
