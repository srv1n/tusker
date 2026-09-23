import { Pause, Play, RotateCcw } from "lucide-react";
import { ActionResultLine } from "@/components/ui/action-feedback";
import { useRecovery, useWaveControl, useWaveReview } from "@/lib/queries";
import { cn } from "@/lib/cn";
import { RECOVERY_ACTION_LABEL, RECOVERY_EXPLANATION, RECOVERY_STATE_LABEL } from "@/lib/recovery";
import { ProductLoading, ProductSection, ProductStatus, ProductUnavailable } from "@/features/product/shared";
import { HumanActionCard } from "@/features/human-action/HumanActionCard";
import { usableWaveReview, waveReviewStage } from "./integrationModel";
import type { DirectStartBlocker, WaveReview, WaveReviewMember } from "@/types/domain";

const WAVE_CONTROL_LABEL: Record<string, string> = { "wave start": "Run wave", "wave pause": "Pause", "wave resume": "Resume" };
const WAVE_CONTROL_PENDING: Record<string, string> = { "wave start": "Starting wave…", "wave pause": "Pausing…", "wave resume": "Resuming…" };
const WAVE_CONTROL_ACTION: Record<string, "start" | "pause" | "resume"> = { "wave start": "start", "wave pause": "pause", "wave resume": "resume" };
const WAVE_CONTROL_ICON: Record<string, typeof Play> = { "wave start": Play, "wave pause": Pause, "wave resume": RotateCcw };
const WAVE_CONTROL_STYLE: Record<string, string> = { "wave start": "bg-pass text-surface hover:opacity-90", "wave pause": "border border-line bg-raised text-ink hover:border-ink/40 hover:bg-hover", "wave resume": "bg-ink text-surface hover:opacity-90" };

type WaveStatus = { label: string; tone: "pass" | "info" | "warn" | "fail" | "neutral"; hint?: string };
type IssueGroup = { key: string; title: string; explanation: string; next: string; affected?: boolean; blockers: DirectStartBlocker[] };

const normalBlocker = (blocker: DirectStartBlocker) => blocker.code === "DEPENDENCY_WAITING" || blocker.code === "HUMAN_GATE_OPEN";
const setupBlocker = (blocker: DirectStartBlocker) => blocker.code === "RUNTIME_UNAVAILABLE" || blocker.code.startsWith("ROUTE_");

function verificationIssue(blocker: DirectStartBlocker): Omit<IssueGroup, "blockers"> {
  if (blocker.code === "STRICT_PROOF_MISSING") return { key: "verification-missing", title: "Verification has not been recorded", explanation: "Required verification is still missing.", next: "An agent needs to run and record the required check." };
  if (blocker.code === "STRICT_PROOF_FAILED") return { key: "verification-failed", title: "Verification failed", explanation: "A required check did not pass.", next: "An agent needs to repair the failure and run the check again." };
  if (blocker.code === "STRICT_PROOF_STALE") return { key: "verification-stale", title: "Previous verification no longer applies", explanation: "The work changed after its recorded verification.", next: "An agent needs to run the current verification again." };
  const reason = blocker.reason.toLowerCase();
  if (reason.includes("fail")) return { key: "verification-failed", title: "Verification failed", explanation: "A required check did not pass.", next: "An agent needs to repair the failure and run the check again." };
  if (reason.includes("stale")) return { key: "verification-stale", title: "Previous verification no longer applies", explanation: "The work changed after its recorded verification.", next: "An agent needs to run the current verification again." };
  return { key: "verification-missing", title: "Verification has not been recorded", explanation: "Required verification is still missing.", next: "An agent needs to run and record the required check." };
}

/** One card per concrete failure: the named task or lane and its supplied recovery, never a generic wave fallback. */
function specificIssue(blocker: DirectStartBlocker, member?: WaveReviewMember): Omit<IssueGroup, "blockers"> {
  const subject = member?.title || blocker.taskId || "";
  const suffix = subject ? ` — ${subject}` : "";
  if (blocker.code === "OUTCOME_UNKNOWN") {
    return { key: `unknown:${blocker.taskId ?? "wave"}`, title: `${RECOVERY_STATE_LABEL}${suffix}`, explanation: RECOVERY_EXPLANATION, next: RECOVERY_ACTION_LABEL, affected: false };
  }
  if (blocker.code === "RUNTIME_FAILED") {
    const lane = member?.lane === "review" ? "Review" : "Implementation";
    return { key: `failure:${blocker.code}:${blocker.taskId ?? "wave"}`, title: `${lane} failed${suffix}`, explanation: blocker.reason, next: blocker.action, affected: false };
  }
  if (blocker.code === "REVIEW_SNAPSHOT_STALE") {
    return { key: `failure:${blocker.code}:${blocker.taskId ?? "wave"}`, title: `Review no longer applies${suffix}`, explanation: blocker.reason, next: blocker.action, affected: false };
  }
  if (blocker.code.startsWith("MATERIAL_") || blocker.code.startsWith("WAVE_")) {
    return { key: `material:${blocker.code}:${blocker.taskId ?? "wave"}`, title: "The wave definition needs repair", explanation: blocker.reason, next: blocker.action, affected: false };
  }
  return { key: `blocker:${blocker.code}:${blocker.taskId ?? "wave"}`, title: subject ? `${subject} needs attention` : "This wave needs attention", explanation: blocker.reason, next: blocker.action, affected: false };
}

/** Groups diagnostics by the user-visible remedy; raw records stay disclosed. */
export function summarizeIssues(blockers: DirectStartBlocker[], members: WaveReviewMember[] = []): IssueGroup[] {
  const memberById = new Map(members.map((member) => [member.taskId, member]));
  const groups = new Map<string, IssueGroup>();
  for (const blocker of blockers.filter((item) => !normalBlocker(item))) {
    let summary: Omit<IssueGroup, "blockers">;
    if (setupBlocker(blocker)) summary = { key: "setup", title: "Execution setup needs attention", explanation: "Tusker cannot currently reach the configured execution service or route.", next: "Restore the service or route configuration, then try again." };
    else if (blocker.code.startsWith("STRICT_PROOF_")) summary = verificationIssue(blocker);
    else if (blocker.code === "CONTRACT_FINGERPRINT_STALE" || blocker.code === "DEPENDENCY_CONTRACT_INVALID") summary = { key: "records", title: "Work records need reconciling", explanation: "Recorded work no longer matches the current task or prerequisite.", next: "An agent needs to reconcile the records and run any required checks." };
    else if (blocker.code === "ACTIVE_OWNER") summary = { key: "owner", title: "Another agent owns this work", explanation: "This task is assigned outside this wave’s current execution scope.", next: "Wait for that attempt to finish or release the task." };
    else summary = specificIssue(blocker, memberById.get(blocker.taskId ?? ""));
    const existing = groups.get(summary.key);
    if (existing) existing.blockers.push(blocker);
    else groups.set(summary.key, { ...summary, blockers: [blocker] });
  }
  return [...groups.values()];
}

type WaveCount = { label: string; tone: string };

/** Glanceable member counts; color only supplements the text labels. */
export function waveCounts(review: WaveReview): WaveCount[] {
  const members = review.members;
  const accepted = members.filter((member) => member.phase === "completed" || member.state === "completed").length;
  const executing = members.filter((member) => member.phase === "executing" || member.state === "running").length;
  const reviewing = members.filter((member) => member.phase === "reviewing" || member.phase === "awaiting_review").length;
  const unknown = members.filter((member) => member.phase === "outcome_unknown").length;
  const failed = members.filter((member) => member.phase !== "outcome_unknown" && (member.phase === "failed" || member.phase === "proof_blocked" || member.state === "blocked")).length;
  const waiting = Math.max(0, members.length - accepted - executing - reviewing - unknown - failed);
  const counts: WaveCount[] = [{ label: `${accepted}/${members.length} accepted`, tone: "text-pass" }];
  if (executing) counts.push({ label: `${executing} executing`, tone: "text-info" });
  if (reviewing) counts.push({ label: `${reviewing} in review`, tone: "text-accent" });
  if (waiting) counts.push({ label: `${waiting} waiting`, tone: "text-muted" });
  if (unknown) counts.push({ label: `${unknown} needs recovery`, tone: "text-warn" });
  if (failed) counts.push({ label: `${failed} failed`, tone: "text-fail" });
  return counts;
}

/** Effective project capacity, only when it is the thing constraining eligible work. */
export function capacityConstraint(review: WaveReview): string | undefined {
  for (const member of review.members) {
    const match = member.phase === "capacity_wait" ? member.waitingReason?.match(/project capacity \d+\/(\d+)/) : undefined;
    if (match) {
      const limit = Number(match[1]);
      return `${limit} task${limit === 1 ? "" : "s"} at a time`;
    }
  }
  return undefined;
}

/** The review emits one task-start control for every independently eligible member. */
export function hasIndependentEligibleWork(review: WaveReview): boolean {
  return review.controls.some((control) => control.action === "task start" && control.enabled);
}

/** An armed wave with only terminal failures can be replayed through Start. */
export function canRetryWave(review: WaveReview): boolean {
  if (waveReviewStage(review) !== "failed" || review.authorization !== "authorized") return false;
  if (review.members.some((member) => member.phase === "executing" || member.phase === "reviewing" || member.state === "running")) return false;
  if (!review.members.some((member) => member.recovery?.action === "retry_task" && member.recovery.enabled)) return false;
  return !review.blockers.some((blocker) => !blocker.taskId || ["WAVE_TERMINAL", "ROUTE_INVALID", "DEPENDENCY_CONTRACT_INVALID", "CONTRACT_FINGERPRINT_STALE", "ACTIVE_OWNER", "OUTCOME_UNKNOWN"].includes(blocker.code));
}

/** One truthful primary state; blockers remain separate from normal scheduling. */
export function summarizeWave(review: WaveReview): WaveStatus {
	if (review.blockers.some((blocker) => blocker.code === "OUTCOME_UNKNOWN")) return { label: RECOVERY_STATE_LABEL, tone: "warn", hint: RECOVERY_EXPLANATION };
  switch (waveReviewStage(review)) {
    case "completed": return { label: "Completed", tone: "pass" };
    case "cancelled": return { label: "Cancelled", tone: "neutral", hint: "This wave ended without delivery." };
    case "paused": return { label: "Paused", tone: "neutral" };
    case "ready": return { label: "Ready", tone: "pass" };
    case "queued": return { label: "Queued", tone: "info" };
    case "executing": return { label: "Executing", tone: "info" };
    case "awaiting_review": return { label: "Awaiting review", tone: "info" };
    case "reviewing": return { label: "Reviewing", tone: "info" };
    case "failed": return { label: "Failed", tone: "fail" };
    case "blocked": return review.humanActions?.length ? { label: "Waiting for you", tone: "warn" } : review.blockers.some(setupBlocker) ? { label: "Waiting for setup", tone: "warn" } : { label: "Blocked", tone: "fail" };
    default: return { label: "Status unavailable", tone: "warn", hint: "The authoritative wave status is stale. Refresh before acting." };
  }
}

/** One short line, only where the action’s behavior is not obvious. */
function controlHint(action: string, review: WaveReview): string | undefined {
	if (action === "wave pause") return "Running. Pause stops new tasks; active tasks may finish.";
  if (action === "wave resume") return "Restores the saved authorization; it does not retry failed tasks.";
  if (action === "wave start" && canRetryWave(review)) return "Previous attempts failed. Retry requeues eligible work.";
  if (action === "wave start" && review.humanActions?.length) return "Eligible tasks can still start.";
  return undefined;
}

/** Raw internals (fingerprints, codes) stay out of this surface entirely; Diagnostics and `tusker wave review --json` carry them. */

/** One reading of the wave review for every control surface. */
function useWaveAuthority(projectId: string, waveId: string) {
  const review = useWaveReview(waveId, projectId);
  const control = useWaveControl(projectId, waveId);
  const data = usableWaveReview(review.data, review.error);
  const stage = data ? waveReviewStage(data) : null;
  const retryable = data ? canRetryWave(data) : false;
  const hasUnknownRecovery = Boolean(data?.blockers.some((blocker) => blocker.code === "OUTCOME_UNKNOWN"));
  const enabled = retryable
    ? { action: "wave start" as const, enabled: true, scope: waveId }
    : (data?.controls ?? []).find((candidate) => candidate.enabled && candidate.action !== "task start" && WAVE_CONTROL_ACTION[candidate.action] && (candidate.action !== "wave start" || (!hasUnknownRecovery && (!data?.humanActions?.length || hasIndependentEligibleWork(data)))));
  const status = data ? summarizeWave(data) : null;
  const issues = data ? summarizeIssues(data.blockers, data.members) : [];
  const capacity = data ? capacityConstraint(data) : undefined;
  const startRefused = !enabled && data && (stage === "blocked" || stage === "failed")
    ? data.controls.find((candidate) => candidate.action === "wave start" && !candidate.enabled)
    : undefined;
  const startReason = startRefused ? (issues[0]?.title ?? startRefused.reason ?? "Work needs attention before this wave can start.") : undefined;
  const hint = data && enabled ? controlHint(enabled.action, data) : status?.hint;
  return { review, control, data, stage, retryable, enabled, status, issues, capacity, startRefused, startReason, hint };
}

function WaveControlButton({ wave, waveId }: { wave: ReturnType<typeof useWaveAuthority>; waveId: string }) {
  const { enabled, retryable, control, startRefused, startReason } = wave;
  if (enabled) {
    const Icon = WAVE_CONTROL_ICON[enabled.action];
    return <button type="button" data-wave-control={enabled.action} aria-label={`${retryable ? "Retry wave" : WAVE_CONTROL_LABEL[enabled.action]} ${waveId}`} disabled={control.isPending} onClick={() => control.mutate(WAVE_CONTROL_ACTION[enabled.action])} className={cn("inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[12px] font-semibold disabled:opacity-50", WAVE_CONTROL_STYLE[enabled.action])}><Icon size={13} aria-hidden="true" />{control.isPending ? (retryable ? "Retrying…" : WAVE_CONTROL_PENDING[enabled.action]) : (retryable ? "Retry wave" : WAVE_CONTROL_LABEL[enabled.action])}</button>;
  }
  if (startRefused) return <button type="button" disabled data-wave-control-refused="wave start" aria-label={`Run wave ${waveId}`} title={startReason} className="inline-flex cursor-not-allowed items-center gap-1.5 rounded-md border border-line bg-panel px-3 py-1.5 text-[12px] font-semibold text-muted"><Play size={13} aria-hidden="true" />Run wave</button>;
  return null;
}

export function WaveAuthorityControls({ projectId, waveId, compact }: { projectId: string; waveId: string; compact?: boolean }) {
  const wave = useWaveAuthority(projectId, waveId);
  const { review, control, data, stage, retryable, enabled, status, issues, capacity, startRefused, startReason, hint } = wave;
  return <section className={cn("text-left", compact ? "max-w-[24rem]" : "w-full rounded-xl border border-line bg-raised p-4 sm:p-5")} data-wave-authority data-wave-state={data?.state ?? (review.error ? "unavailable" : "loading")} data-wave-recovery={retryable ? "retryable" : undefined}>
    {review.isLoading ? <p role="status" className="text-[12px] text-faint">Loading wave status…</p> : null}
    {review.error ? <p role="alert" className="text-[12px] leading-5 text-fail">Wave status is unavailable. {review.error instanceof Error ? review.error.message : "Try refreshing."}</p> : null}
    {data && status ? <><div className="flex flex-wrap items-center gap-x-3 gap-y-2">
      {stage !== "ready" ? <ProductStatus tone={status.tone}>{status.label}</ProductStatus> : null}
      <span className="flex flex-wrap items-center gap-x-2 text-[12px] font-medium" data-wave-counts>{waveCounts(data).map((count) => <span key={count.label} className={count.tone}>{count.label}</span>)}</span>
      {capacity ? <span className="text-[12px] text-muted" data-wave-capacity>{capacity} · <a className="underline decoration-line underline-offset-2 hover:text-ink" href={`/p/${projectId}/settings`}>Settings</a></span> : null}
    </div>
      {data.humanActions?.length ? <section className="mt-4 space-y-3" aria-label="Your action">{data.humanActions.map(({ taskId, taskTitle, action }) => <HumanActionCard key={action.gateId} action={action} taskId={taskId} taskTitle={taskTitle} projectId={projectId} blockedTaskIds={action.blockedTaskIds} continueOnApproval={data.authorization === "authorized"} compact />)}</section> : null}
      {hint ? <p className="mt-2 text-[12px] leading-5 text-muted">{hint}</p> : null}
      {enabled || startRefused ? <div className="mt-3 flex justify-end"><span className="inline-flex items-center gap-2"><WaveControlButton wave={wave} waveId={waveId} />{startRefused ? <span className="text-[12px] text-muted">{startReason}</span> : null}</span></div> : null}
      {issues.length ? <section className="mt-4 space-y-2" aria-label="What needs attention">{issues.map((issue) => <WaveIssue key={issue.key} issue={issue} projectId={projectId} member={data.members.find((member) => member.taskId === issue.blockers[0]?.taskId)} />)}</section> : null}
      <div aria-live="polite" className="mt-3"><ActionResultLine pending={control.isPending} error={control.error} /></div>
    </> : null}
  </section>;
}

/** The wave's one primary action for a toolbar; a refusal reason lives in its tooltip. */
export function WavePrimaryAction({ projectId, waveId }: { projectId: string; waveId: string }) {
  const wave = useWaveAuthority(projectId, waveId);
  return <span className="inline-flex items-center gap-2" data-wave-authority data-wave-state={wave.data?.state ?? (wave.review.error ? "unavailable" : "loading")}>
    <span aria-live="polite"><ActionResultLine pending={wave.control.isPending} error={wave.control.error} /></span>
    <WaveControlButton wave={wave} waveId={waveId} />
  </span>;
}

/** Status chip, accepted count, and auto-run state on one row. */
export function WaveMeta({ projectId, waveId }: { projectId: string; waveId: string }) {
  const { data, status, stage } = useWaveAuthority(projectId, waveId);
  if (!data || !status) return null;
  const accepted = data.members.filter((member) => member.phase === "completed" || member.state === "completed").length;
  const total = data.members.length;
  return <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-muted" data-wave-counts>
    {stage !== "ready" ? <ProductStatus tone={status.tone}>{status.label}</ProductStatus> : null}
    <span>{accepted} of {total} accepted</span>
    <span className="h-1 w-24 overflow-hidden rounded-full bg-panel" aria-hidden="true"><span className="block h-full bg-pass" style={{ width: `${total ? (accepted / total) * 100 : 0}%` }} /></span>
    <span>{data.authorization === "authorized" ? "Auto-run on" : "Auto-run off"}</span>
  </div>;
}

/** At most one line of what is holding the wave; the detail stays one click down. */
export function WaveCallout({ projectId, waveId, waitSummary }: { projectId: string; waveId: string; waitSummary?: { title: string; body: string; hint?: string } }) {
  const { review, data, issues, capacity, startRefused, startReason, hint } = useWaveAuthority(projectId, waveId);
  if (review.error) return <p role="alert" className="border-b border-line px-5 py-2 text-[12.5px] text-fail">Wave status is unavailable. {review.error instanceof Error ? review.error.message : "Try refreshing."}</p>;
  if (!data) return null;
  const human = data.humanActions ?? [];
  const facts = [
    ...(human.length ? [`Waiting for you: ${human.map((item) => item.taskTitle || item.taskId).join(", ")}`] : []),
    ...(waitSummary ? [waitSummary.title] : []),
    ...issues.map((issue) => issue.title),
    ...(startRefused && !issues.length && startReason ? [startReason] : []),
    ...(capacity ? [`Limited to ${capacity}`] : []),
  ];
  if (!facts.length) return null;
  return <details className="border-b border-line bg-panel/40 px-5 py-2 text-[12.5px] text-ink" data-wave-callout>
    <summary className="cursor-pointer marker:text-faint">{facts.join(". ")}. <span className="text-muted underline underline-offset-2">Details</span></summary>
    <div className="mt-3 max-w-3xl space-y-3 pb-2">
      {waitSummary ? <p className="leading-5 text-muted">{waitSummary.body}{waitSummary.hint ? ` ${waitSummary.hint}` : ""}</p> : null}
      {hint ? <p className="leading-5 text-muted">{hint}</p> : null}
      {human.map(({ taskId, taskTitle, action }) => <HumanActionCard key={action.gateId} action={action} taskId={taskId} taskTitle={taskTitle} projectId={projectId} blockedTaskIds={action.blockedTaskIds} continueOnApproval={data.authorization === "authorized"} compact />)}
      {issues.map((issue) => <WaveIssue key={issue.key} issue={issue} projectId={projectId} member={data.members.find((member) => member.taskId === issue.blockers[0]?.taskId)} />)}
      {capacity ? <a className="block text-muted underline underline-offset-2 hover:text-ink" href={`/p/${projectId}/settings`}>Capacity settings</a> : null}
    </div>
  </details>;
}

function WaveIssue({ issue, projectId, member }: { issue: IssueGroup; projectId: string; member?: WaveReviewMember }) {
  const taskId = issue.blockers.length === 1 ? issue.blockers[0].taskId : undefined;
  const recovery = useRecovery(taskId ?? "", projectId);
  const canRecover = Boolean(taskId && issue.blockers[0].code === "OUTCOME_UNKNOWN" && member?.recovery?.action === "recover_unknown");
  return <article className="rounded-lg border border-warn/30 bg-warn-soft px-3 py-2.5 text-[12px] leading-5 text-warn">
    <p className="font-semibold text-ink">{issue.title}</p>
    <p>{issue.explanation}{issue.affected === false ? "" : issue.blockers.length > 1 ? ` ${issue.blockers.length} tasks are affected.` : " One task is affected."}</p>
    <div className="mt-1 flex flex-wrap items-center gap-2">
      {canRecover ? <><button type="button" className="rounded-md bg-ink px-3 py-1.5 font-semibold text-surface disabled:opacity-50" disabled={recovery.isPending || !member?.recovery?.enabled} title={member?.recovery?.reason} onClick={() => recovery.mutate("recover_unknown")}>{recovery.isPending ? "Verifying…" : RECOVERY_ACTION_LABEL}</button>{member?.recovery?.reason && !member.recovery.enabled ? <span>{member.recovery.reason}</span> : null}</> : <span className="font-medium">Next: {issue.next}</span>}
      {taskId ? <a className="underline underline-offset-2 hover:text-ink" href={`/p/${projectId}/tasks/${taskId}`}>Open task</a> : null}
    </div>
    <ActionResultLine className="mt-2" pending={recovery.isPending} error={recovery.error} result={recovery.data} />
  </article>;
}

function memberPhase(member: WaveReviewMember, opts?: { authorized?: boolean; titleFor?: (taskId: string) => string | undefined }): string {
  if (member.completionReported && member.state !== "completed") return "Implementation reported complete; acceptance is not yet recorded.";
  if (member.phase === "completed" || member.state === "completed") return "Accepted";
  if (member.phase === "proof_blocked") return `Verification required${member.waitingReason ? `: ${member.waitingReason}` : ""}`;
  if (member.phase === "outcome_unknown") return RECOVERY_STATE_LABEL;
  if (member.phase === "failed") return `Failed${member.waitingReason ? `: ${member.waitingReason}` : ""}`;
  if (member.phase === "blocked" || member.state === "blocked") return `Needs attention${member.waitingReason ? `: ${member.waitingReason}` : ""}`;
  if (member.phase === "executing" || member.state === "running") return "Executing";
  if (member.phase === "reviewing") return "Reviewing";
  if (member.phase === "awaiting_review" || member.waitingReason?.includes("independent review")) return "Awaiting review";
  if (member.phase === "paused") return `Paused${member.waitingReason ? `: ${member.waitingReason}` : ""}`;
  if (member.phase === "capacity_wait") return "Ready — waiting for an execution slot";
  if (member.phase === "rework") return `Fixing review findings${member.waitingReason ? `: ${member.waitingReason}` : ""}`;
  if (member.phase === "queued") return "Queued";
  if (member.waitingReason?.toLowerCase().includes("rework")) return "Fixing review findings";
  if (member.waitingReason?.startsWith("waiting for dependency ")) {
    const depId = member.waitingReason.replace("waiting for dependency ", "");
    return `Waiting for ${opts?.titleFor?.(depId) ?? depId} to complete`;
  }
  if (member.waitingReason?.includes("human gate")) return "Waiting for you";
  return member.state === "ready" ? (opts?.authorized ? "Ready — starting automatically" : "Ready") : "Queued";
}

export function WaveInstructions({ member }: { member: WaveReviewMember }) {
  const detail = [member.instructions, ...(member.acceptance?.length ? [`Acceptance: ${member.acceptance.join(" · ")}`] : []), ...(member.verification?.length ? [`Verification: ${member.verification.join(" · ")}`] : [])].filter(Boolean).join("\n\n");
  if (!detail.trim() && !member.executeRoute && !member.reviewRoute) return null;
  return <details className="mt-2 rounded-md border border-line bg-panel/60 px-3 py-2" data-wave-instructions={member.taskId}><summary className="cursor-pointer text-[11.5px] font-medium text-muted hover:text-ink">Task details</summary><div className="mt-2 whitespace-pre-wrap text-[12px] leading-5 text-ink">{detail}{member.executeRoute || member.reviewRoute ? <p className="mt-3 text-muted">Execution: {member.executeRoute || "Not configured"} · Review: {member.reviewRoute || "Not configured"}</p> : null}</div></details>;
}

export function WaveMemberList({ review, projectId }: { review: WaveReview; projectId?: string }) {
  const titles = new Map(review.members.map((member) => [member.taskId, member.title]));
  const opts = { authorized: review.authorization === "authorized", titleFor: (taskId: string) => titles.get(taskId) };
  return <div className="divide-y divide-line" data-wave-members>{review.members.map((member) => <article key={member.taskId} className="py-3 first:pt-0 last:pb-0" data-wave-member={member.taskId}><div className="flex flex-wrap items-center justify-between gap-2"><p className="text-[12.5px] font-semibold text-ink">{projectId ? <a href={`/p/${projectId}/tasks/${member.taskId}`} className="hover:underline">{member.title}</a> : member.title}</p><span className="text-[11.5px] text-muted">{memberPhase(member, opts)}</span></div><WaveInstructions member={member} /></article>)}</div>;
}

export function WaveReviewDetail({ projectId, waveId, showControls = true, showDependencies = true }: { projectId: string; waveId: string; showControls?: boolean; showDependencies?: boolean }) {
  const review = useWaveReview(waveId, projectId);
  if (review.isLoading) return <ProductLoading rows={3} />;
  if (review.error) return <ProductUnavailable>Work details are unavailable. {review.error instanceof Error ? review.error.message : "Try refreshing this project."}</ProductUnavailable>;
  const data = review.data;
  if (!data) return null;
  return <ProductSection title="Tasks"><div data-wave-review={data.waveId}>{showControls ? <WaveAuthorityControls projectId={projectId} waveId={waveId} /> : null}{showControls && data.outcome ? <p className="mb-4 text-[12.5px] leading-5 text-ink">{data.outcome}</p> : null}<WaveMemberList review={data} projectId={projectId} />{showDependencies ? <details className="mt-4 text-[11px] text-faint"><summary className="cursor-pointer">Dependency view</summary><ol className="mt-2 space-y-1">{data.frontiers.map((frontier, index) => <li key={index} className="rounded-md bg-surface px-3 py-1.5 text-ink"><span className="mr-2 font-mono text-[10px] text-faint">{index + 1}</span>{frontier.join(" → ")}</li>)}</ol></details> : null}</div></ProductSection>;
}
