import { Pause, Play, RotateCcw } from "lucide-react";
import { ActionResultLine } from "@/components/ui/action-feedback";
import { useWaveControl, useWaveReview } from "@/lib/queries";
import { cn } from "@/lib/cn";
import { ProductLoading, ProductSection, ProductStatus, ProductUnavailable } from "@/features/product/shared";
import { HumanActionCard } from "@/features/human-action/HumanActionCard";
import { usableWaveReview, waveReviewStage } from "./integrationModel";
import type { DirectStartBlocker, WaveReview, WaveReviewMember } from "@/types/domain";

const WAVE_CONTROL_LABEL: Record<string, string> = { "wave start": "Start wave", "wave pause": "Pause", "wave resume": "Resume" };
const WAVE_CONTROL_PENDING: Record<string, string> = { "wave start": "Starting wave…", "wave pause": "Pausing…", "wave resume": "Resuming…" };
const WAVE_CONTROL_ACTION: Record<string, "start" | "pause" | "resume"> = { "wave start": "start", "wave pause": "pause", "wave resume": "resume" };
const WAVE_CONTROL_ICON: Record<string, typeof Play> = { "wave start": Play, "wave pause": Pause, "wave resume": RotateCcw };

type WaveStatus = { label: string; explanation: string; tone: "pass" | "info" | "warn" | "fail" | "neutral" };
type IssueGroup = { key: string; title: string; explanation: string; next: string; blockers: DirectStartBlocker[] };

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

/** Groups diagnostics by the user-visible remedy; raw records stay disclosed. */
export function summarizeIssues(blockers: DirectStartBlocker[]): IssueGroup[] {
  const groups = new Map<string, IssueGroup>();
  for (const blocker of blockers.filter((item) => !normalBlocker(item))) {
    let summary: Omit<IssueGroup, "blockers">;
    if (setupBlocker(blocker)) summary = { key: "setup", title: "Execution setup needs attention", explanation: "Tusker cannot currently reach the configured execution service or route.", next: "Restore the service or route configuration, then try again." };
    else if (blocker.code.startsWith("STRICT_PROOF_")) summary = verificationIssue(blocker);
    else if (blocker.code === "CONTRACT_FINGERPRINT_STALE" || blocker.code === "DEPENDENCY_CONTRACT_INVALID") summary = { key: "records", title: "Work records need reconciling", explanation: "Recorded work no longer matches the current task or prerequisite.", next: "An agent needs to reconcile the records and run any required checks." };
    else if (blocker.code === "ACTIVE_OWNER") summary = { key: "owner", title: "Another agent owns this work", explanation: "This task is assigned outside this wave’s current execution scope.", next: "Wait for that attempt to finish or release the task." };
    else summary = { key: "wave-setup", title: "Wave setup needs attention", explanation: "The wave’s saved work definition is incomplete or inconsistent.", next: "An agent needs to repair the wave setup before work can start." };
    const existing = groups.get(summary.key);
    if (existing) existing.blockers.push(blocker);
    else groups.set(summary.key, { ...summary, blockers: [blocker] });
  }
  return [...groups.values()];
}

function progressText(review: WaveReview): string {
  const total = review.members.length;
  const accepted = review.members.filter((member) => member.state === "completed").length;
  const running = review.members.filter((member) => member.state === "running" || member.phase === "executing" || member.phase === "reviewing").length;
  const waiting = Math.max(0, total - accepted - running);
  return [`${accepted} of ${total} tasks accepted`, running ? `${running} running` : "", waiting ? `${waiting} waiting` : ""].filter(Boolean).join(" · ");
}

/** The review emits one task-start control for every independently eligible member. */
export function hasIndependentEligibleWork(review: WaveReview): boolean {
  return review.controls.some((control) => control.action === "task start" && control.enabled);
}

/** One truthful primary state; blockers remain separate from normal scheduling. */
export function summarizeWave(review: WaveReview): WaveStatus {
  switch (waveReviewStage(review)) {
    case "completed": return { label: "Completed", explanation: "Required implementation, review and verification are recorded.", tone: "pass" };
    case "cancelled": return { label: "Cancelled", explanation: "This wave ended without delivery.", tone: "neutral" };
    case "paused": return { label: "Paused", explanation: "New work is stopped. Existing attempts may finish.", tone: "neutral" };
    case "ready": return { label: "Ready to start", explanation: "Work is prepared; nothing is running yet.", tone: "pass" };
    case "queued": return { label: "Queued", explanation: "Authorized work is waiting for the next eligible task or runtime pickup.", tone: "info" };
    case "executing": return { label: "Executing", explanation: "Agents are actively progressing the work.", tone: "info" };
    case "awaiting_review": return { label: "Awaiting review", explanation: "Implementation is finished. Independent review is next.", tone: "info" };
    case "reviewing": return { label: "Reviewing", explanation: "An independent reviewer is assessing the work.", tone: "info" };
    case "failed": return { label: "Failed", explanation: "A task attempt failed. See the affected task and diagnostic below.", tone: "fail" };
    case "blocked": return review.humanActions?.length ? { label: "Waiting for you", explanation: hasIndependentEligibleWork(review) ? "A specific decision is needed, though other eligible tasks can still start." : "A specific decision is needed before this work can continue.", tone: "warn" } : review.blockers.some(setupBlocker) ? { label: "Waiting for setup", explanation: "Execution setup needs attention before agents can progress the work.", tone: "warn" } : { label: "Blocked", explanation: "Work needs attention. See the specific issue below.", tone: "fail" };
    default: return { label: "Status unavailable", explanation: "The authoritative wave status is stale. Refresh before acting.", tone: "warn" };
  }
}

function controlScope(action: string): string {
  if (action === "wave start") return "This starts only the currently eligible tasks in this wave. Tasks waiting on predecessors start later.";
  if (action === "wave pause") return "Pause stops new tasks from starting; active attempts can finish.";
  return "This restores the wave’s saved authorization; it does not change its task scope.";
}

function TechnicalDetails({ review, projectId }: { review: WaveReview; projectId: string }) {
  return <details className="mt-4 text-[11px] text-faint"><summary className="cursor-pointer font-medium text-muted hover:text-ink">Technical details</summary><div className="mt-2 space-y-2 rounded-md border border-line bg-surface p-3"><p className="break-all font-mono">Material fingerprint: {review.materialFingerprint}</p>{review.blockers.length ? <ul className="space-y-2">{review.blockers.map((blocker, index) => <li key={`${blocker.code}-${blocker.taskId ?? ""}-${index}`}><span className="font-mono font-semibold">{blocker.code}</span>{" · "}{blocker.reason}{blocker.taskId ? <a className="ml-1 underline" href={`/p/${projectId}/tasks/${blocker.taskId}`}>Open task</a> : null}</li>)}</ul> : <p>No current diagnostic records.</p>}</div></details>;
}

export function WaveAuthorityControls({ projectId, waveId, compact }: { projectId: string; waveId: string; compact?: boolean }) {
  const review = useWaveReview(waveId, projectId);
  const control = useWaveControl(projectId, waveId);
  const data = usableWaveReview(review.data, review.error);
  const enabled = (data?.controls ?? []).find((candidate) => candidate.enabled && candidate.action !== "task start" && WAVE_CONTROL_ACTION[candidate.action] && (candidate.action !== "wave start" || !data?.humanActions?.length || hasIndependentEligibleWork(data)));
  const Icon = enabled ? WAVE_CONTROL_ICON[enabled.action] : Play;
  const status = data ? summarizeWave(data) : null;
  const issues = data ? summarizeIssues(data.blockers) : [];
  return <section className={cn("text-left", compact ? "max-w-[24rem]" : "w-full rounded-xl border border-line bg-raised p-4 sm:p-5")} data-wave-authority data-wave-state={data?.state ?? (review.error ? "unavailable" : "loading")}>
    {review.isLoading ? <p role="status" className="text-[12px] text-faint">Loading wave status…</p> : null}
    {review.error ? <p role="alert" className="text-[12px] leading-5 text-fail">Wave status is unavailable. {review.error instanceof Error ? review.error.message : "Try refreshing."}</p> : null}
    {data && status ? <><div className="flex flex-wrap items-center gap-2"><ProductStatus tone={status.tone}>{status.label}</ProductStatus><span className="text-[12px] text-muted">{progressText(data)}</span></div><p className="mt-2 max-w-2xl text-[13px] leading-5 text-ink-soft">{status.explanation}</p>
      {data.humanActions?.length ? <section className="mt-4 space-y-3" aria-label="Your action">{data.humanActions.map(({ taskId, taskTitle, action }) => <HumanActionCard key={action.gateId} action={action} taskId={taskId} taskTitle={taskTitle} projectId={projectId} blockedTaskIds={action.blockedTaskIds} continueOnApproval={data.authorization === "authorized"} compact />)}</section> : null}
      {enabled ? <div className="mt-4"><p className="mb-2 text-[12px] leading-5 text-muted">{controlScope(enabled.action)}</p><button type="button" data-wave-control={enabled.action} aria-label={`${WAVE_CONTROL_LABEL[enabled.action]} ${waveId}`} disabled={control.isPending} onClick={() => control.mutate(WAVE_CONTROL_ACTION[enabled.action])} className="inline-flex items-center gap-1.5 rounded-md bg-ink px-3 py-2 text-[12px] font-semibold text-surface disabled:opacity-50"><Icon size={13} aria-hidden="true" />{control.isPending ? WAVE_CONTROL_PENDING[enabled.action] : WAVE_CONTROL_LABEL[enabled.action]}</button></div> : null}
      {issues.length ? <section className="mt-4 space-y-2" aria-label="What needs attention">{issues.map((issue) => <article key={issue.key} className="rounded-lg border border-warn/30 bg-warn-soft px-3 py-2.5 text-[12px] leading-5 text-warn"><p className="font-semibold text-ink">{issue.title}</p><p>{issue.explanation} {issue.blockers.length > 1 ? `${issue.blockers.length} tasks are affected.` : "One task is affected."}</p><p className="font-medium">Next: {issue.next}</p></article>)}</section> : null}
      <div aria-live="polite" className="mt-3"><ActionResultLine pending={control.isPending} error={control.error} /></div><TechnicalDetails review={data} projectId={projectId} />
    </> : null}
  </section>;
}

function memberPhase(member: WaveReviewMember): string {
  if (member.completionReported && member.state !== "completed") return "Implementation reported complete; acceptance is not yet recorded.";
  if (member.phase === "completed" || member.state === "completed") return "Accepted";
  if (member.phase === "proof_blocked") return `Verification required${member.waitingReason ? `: ${member.waitingReason}` : ""}`;
  if (member.phase === "failed" || member.state === "blocked") return `Failed${member.waitingReason ? `: ${member.waitingReason}` : ""}`;
  if (member.phase === "executing" || member.state === "running") return "Executing";
  if (member.phase === "reviewing") return "Reviewing";
  if (member.phase === "awaiting_review" || member.waitingReason?.includes("independent review")) return "Awaiting review";
  if (member.phase === "paused") return `Paused${member.waitingReason ? `: ${member.waitingReason}` : ""}`;
  if (member.phase === "capacity_wait") return member.waitingReason ? `Waiting: ${member.waitingReason}` : "Waiting for an execution slot";
  if (member.phase === "rework") return `Fixing review findings${member.waitingReason ? `: ${member.waitingReason}` : ""}`;
  if (member.phase === "queued") return "Queued";
  if (member.waitingReason?.toLowerCase().includes("rework")) return "Fixing review findings";
  if (member.waitingReason?.startsWith("waiting for dependency ")) return `Waiting for ${member.waitingReason.replace("waiting for dependency ", "")} to complete; that task’s owner acts next.`;
  if (member.waitingReason?.includes("human gate")) return "Waiting for you";
  return member.state === "ready" ? "Ready" : "Queued";
}

export function WaveInstructions({ member }: { member: WaveReviewMember }) {
  const detail = [member.instructions, ...(member.acceptance?.length ? [`Acceptance: ${member.acceptance.join(" · ")}`] : []), ...(member.verification?.length ? [`Verification: ${member.verification.join(" · ")}`] : [])].filter(Boolean).join("\n\n");
  if (!detail.trim() && !member.executeRoute && !member.reviewRoute) return null;
  return <details className="mt-2 rounded-md border border-line bg-panel/60 px-3 py-2" data-wave-instructions={member.taskId}><summary className="cursor-pointer text-[11.5px] font-medium text-muted hover:text-ink">Task details</summary><div className="mt-2 whitespace-pre-wrap text-[12px] leading-5 text-ink">{detail}{member.executeRoute || member.reviewRoute ? <p className="mt-3 text-muted">Execution: {member.executeRoute || "Not configured"} · Review: {member.reviewRoute || "Not configured"}</p> : null}</div></details>;
}

export function WaveMemberList({ review, projectId }: { review: WaveReview; projectId?: string }) {
  return <div className="divide-y divide-line" data-wave-members>{review.members.map((member) => <article key={member.taskId} className="py-3 first:pt-0 last:pb-0" data-wave-member={member.taskId}><div className="flex flex-wrap items-center justify-between gap-2"><p className="text-[12.5px] font-semibold text-ink">{projectId ? <a href={`/p/${projectId}/tasks/${member.taskId}`} className="hover:underline">{member.title}</a> : member.title}</p><span className="text-[11.5px] text-muted">{memberPhase(member)}</span></div><WaveInstructions member={member} /></article>)}</div>;
}

export function WaveReviewDetail({ projectId, waveId, showControls = true, showDependencies = true }: { projectId: string; waveId: string; showControls?: boolean; showDependencies?: boolean }) {
  const review = useWaveReview(waveId, projectId);
  if (review.isLoading) return <ProductLoading rows={3} />;
  if (review.error) return <ProductUnavailable>Work details are unavailable. {review.error instanceof Error ? review.error.message : "Try refreshing this project."}</ProductUnavailable>;
  const data = review.data;
  if (!data) return null;
  return <ProductSection title="Tasks"><div data-wave-review={data.waveId}>{showControls ? <WaveAuthorityControls projectId={projectId} waveId={waveId} /> : null}{showControls && data.outcome ? <p className="mb-4 text-[12.5px] leading-5 text-ink">{data.outcome}</p> : null}<WaveMemberList review={data} projectId={projectId} />{showDependencies ? <details className="mt-4 text-[11px] text-faint"><summary className="cursor-pointer">Dependency view</summary><ol className="mt-2 space-y-1">{data.frontiers.map((frontier, index) => <li key={index} className="rounded-md bg-surface px-3 py-1.5 text-ink"><span className="mr-2 font-mono text-[10px] text-faint">{index + 1}</span>{frontier.join(" → ")}</li>)}</ol></details> : null}</div></ProductSection>;
}
