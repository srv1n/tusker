import { useEffect, useRef, useState, type FormEvent, type ReactNode, type Ref } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Play, X } from "lucide-react";
import { Link } from "@tanstack/react-router";
import type { RunDetail, TaskDetail, WaveReviewMember } from "@/types/domain";
import { api } from "@/lib/api";
import { duration } from "@/lib/time";
import { ActionResultLine } from "@/components/ui/action-feedback";
import { useRecovery, useTaskStart } from "@/lib/queries";
import { RECOVERY_ACTION_LABEL } from "@/lib/recovery";
import { AgentAccessApprovalList, HumanActionCard } from "@/features/human-action/HumanActionCard";
import { AgentCoordinationSummary, taskRunBlocker, TaskContractDisclosure, TaskRouting, tierLabel } from "@/features/product/TaskScreens";
import { isSafeHref } from "@/features/editor/sanitize";
import { DISPLAY_STATE_LABEL } from "../flow/flowGraph";
import {
  acceptedDelivery,
  actualStage,
  currentAttemptRecord,
  failingRows,
  historicalAttemptRecords,
  identityDisplay,
  identitySummary,
  laneLabel,
  latestRunEvent,
  outcomeLabel,
  observedRunIdentity,
  proofInvalidationSummary,
  proofStatusForCurrentAttempt,
  resolveVisibleRun,
  resolveVisibleTask,
  stageFromWaveReviewMember,
  stagePhrase,
  visibleTaskIntent,
  type InspectorExecutionIdentity,
} from "./inspectorLogic";
import "./inspector.css";

export type { InspectorExecutionIdentity };

export interface TaskInspectorProps {
  task: TaskDetail | null;
  run: RunDetail | null;
  selectedTaskId: string | null;
  loading: boolean;
  error?: string;
  executionIdentity?: InspectorExecutionIdentity;
  /** Authoritative wave state when the drawer is opened from a wave. */
  reviewMember?: WaveReviewMember;
  onClose: () => void;
  onOpenTask: (id: string) => void;
}

const STAGE_TONE: Record<string, string> = {
  neutral: "border-line bg-panel/60 text-muted",
  info: "border-info/30 bg-info-soft text-info",
  pass: "border-pass/30 bg-pass-soft text-pass",
  warn: "border-warn/30 bg-warn-soft text-warn",
  fail: "border-fail/30 bg-fail-soft text-fail",
};

function StageChip({ label, tone }: { label: string; tone: keyof typeof STAGE_TONE }) {
  return (
    <span
      data-testid="inspector-stage"
      className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-[11px] font-medium whitespace-nowrap ${STAGE_TONE[tone] ?? STAGE_TONE.neutral}`}
    >
      {label}
    </span>
  );
}

/**
 * TaskInspector — contextual task inspection in a temporary side panel.
 *
 * Contract notes for the integration owner:
 * - Mount only while a task is selected (selectedTaskId != null). The panel
 *   reports close via onClose; the host owns prior focus, the originating
 *   node/card, and the graph transform, and must restore focus on close.
 * - Fetch per selection and pass the payload through unchanged: when the
 *   payload id does not match selectedTaskId (rapid A -> B, late A
 *   response) the panel renders loading, never the other task's data.
 * - executionIdentity must be observed, stage-specific facts. Unobserved or
 *   partial identity renders as Unavailable — a past worker's model is never
 *   shown as the current reviewer's model.
 */
export function TaskInspector({
  task,
  run,
  selectedTaskId,
  loading,
  error,
  executionIdentity,
  reviewMember,
  onClose,
  onOpenTask,
}: TaskInspectorProps) {
  const panelRef = useRef<HTMLElement>(null);

  useEffect(() => {
    panelRef.current?.focus();
  }, [selectedTaskId]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation();
        onClose();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  if (!selectedTaskId) return null;

  const visible = resolveVisibleTask(task, selectedTaskId);
  const visibleRun = resolveVisibleRun(run, selectedTaskId);

  if (error && !visible) {
    return (
      <div className="wux-inspector" role="dialog" aria-modal="false" aria-label="Task details unavailable">
        <InspectorHeader eyebrow={selectedTaskId} title="Task details unavailable" onClose={onClose} />
        <div className="wux-inspector-body">
          <p role="alert" data-testid="inspector-error" className="rounded-lg border border-fail/30 bg-fail-soft px-4 py-3 text-[12.5px] leading-relaxed text-fail">
            Could not load this task: {error}
          </p>
          <button type="button" onClick={onClose} className="wux-inspector-action">
            Back to work
          </button>
        </div>
      </div>
    );
  }

  if (loading || !visible) {
    return (
      <div className="wux-inspector" role="dialog" aria-modal="false" aria-label="Loading task details">
        <InspectorHeader eyebrow={selectedTaskId} title="Loading task…" onClose={onClose} />
        <div className="wux-inspector-body" aria-label="Loading" data-testid="inspector-loading">
          <div className="wux-inspector-skeleton" />
          <div className="wux-inspector-skeleton" />
          <div className="wux-inspector-skeleton wux-inspector-skeleton-short" />
        </div>
      </div>
    );
  }

  return <ReadyInspector task={visible} run={visibleRun} executionIdentity={executionIdentity} reviewMember={reviewMember} panelRef={panelRef} onClose={onClose} onOpenTask={onOpenTask} />;
}

function InspectorHeader({
  eyebrow,
  title,
  onClose,
  onOpen,
}: {
  eyebrow: string;
  title: string;
  onClose: () => void;
  onOpen?: () => void;
}) {
  return (
    <div className="wux-inspector-head">
      <div className="flex w-full items-center gap-2">
        <div data-testid="inspector-header-id" className="min-w-0 flex-1 break-words font-mono text-[11px] text-faint">
          {eyebrow}
        </div>
        {onOpen ? <button type="button" data-testid="inspector-open-task" onClick={onOpen} className="rounded-md px-2 py-1 text-[12px] text-muted hover:bg-hover hover:text-ink">Open page</button> : null}
        <button
          type="button"
          onClick={onClose}
          aria-label="Close task panel"
          data-testid="inspector-close"
          className="wux-inspector-close"
        >
          <X size={16} aria-hidden="true" />
        </button>
      </div>
      <h2 data-testid="inspector-header-title" className="w-full break-words text-[17px] font-semibold leading-snug tracking-[-0.015em] text-ink">
        {title}
      </h2>
    </div>
  );
}

function Section({ title, summary, testId, children }: { title: string; summary?: string; testId?: string; children: ReactNode }) {
  return (
    <details data-testid={testId}>
      <summary><span>{title}</span>{summary ? <span className="ml-2 font-normal text-muted">{summary}</span> : null}</summary>
      <div className="mt-3 space-y-3">{children}</div>
    </details>
  );
}

function ReadyInspector({
  task,
  run,
  executionIdentity,
  reviewMember,
  panelRef,
  onClose,
  onOpenTask,
}: {
  task: TaskDetail;
  run: RunDetail | null;
  executionIdentity?: InspectorExecutionIdentity;
  reviewMember?: WaveReviewMember;
  panelRef: Ref<HTMLElement>;
  onClose: () => void;
  onOpenTask: (id: string) => void;
}) {
  const stage = stageFromWaveReviewMember(reviewMember) ?? actualStage(task, run);
  const taskIntent = visibleTaskIntent(task);
  const identity = identityDisplay(executionIdentity ?? observedRunIdentity(run));
  const failures = failingRows(task);
  const accepted = acceptedDelivery(run);
  const humanActions = [...(task.humanActions ?? []), ...(task.humanAction ? [task.humanAction] : [])].filter((action, index, all) => all.findIndex((candidate) => candidate.gateId === action.gateId) === index);
  const taskStart = useTaskStart(task.id, task.projectId);
  const recovery = useRecovery(task.id, task.projectId);
  const currentStatus = task.rawStatus ?? task.status;
  const runBlocker = taskRunBlocker(task);
  const runnable = !runBlocker;
  const directiveQueued = task.runDirective?.state === "queued";
  const lastEvent = latestRunEvent(run);
  const currentAttempt = currentAttemptRecord(run);
  const staleProofCount = task.verification.filter((row) => proofStatusForCurrentAttempt(row.result, run) === "not-current").length + task.acceptance.filter((row) => proofStatusForCurrentAttempt(row.proof, run) === "not-current").length;
  const previousAttempts = historicalAttemptRecords(run);
  const attemptLane = currentAttempt?.lane ?? run?.lane ?? "execute";
  const attemptDescription = run
    ? currentAttempt
      ? `${outcomeLabel({ lane: attemptLane, outcome: currentAttempt.outcome })} · attempt ${currentAttempt.n}`
      : `${outcomeLabel(run)} · attempt ${run.attemptCount}`
    : "Unavailable — no runtime record";
  const reviewMemberStopsStart = Boolean(reviewMember && (
    reviewMember.phase === "proof_blocked" ||
    reviewMember.phase === "failed" ||
    reviewMember.state === "blocked" ||
    reviewMember.state === "cancelled" ||
    reviewMember.phase === "completed" ||
    reviewMember.state === "completed"
  ));
  // A named human action owns this work. Keep the human control visible, but
  // never render a disabled LLM start affordance beside it.
  const showStart = humanActions.length === 0 && !reviewMemberStopsStart && !stage.live && !["done", "review", "blocked"].includes(currentStatus);
	const [messageBody, setMessageBody] = useState("");
	const [messageStatus, setMessageStatus] = useState("");
	const [contactIndex, setContactIndex] = useState(0);
	const [yieldSender, setYieldSender] = useState(false);
	const [replyTo, setReplyTo] = useState<string | undefined>(() => task.humanActions?.find((action) => action.kind === "question")?.messageId);
	useEffect(() => { setReplyTo(task.humanActions?.find((action) => action.kind === "question")?.messageId); }, [task.id]);
	const replyMessage = task.messages?.find((message) => message.id === replyTo);
	const contact = task.contacts?.[contactIndex] || task.contacts?.[0];
	const senderParts = replyMessage?.sender.split(/:(.*)/s) || [];
	const recipient = replyMessage ? { address: { kind: senderParts[1] ? senderParts[0] as "task" | "execution" : "task", id: senderParts[1] || senderParts[0] } } : contact;
	const sendMessage = async (event: FormEvent) => {
		event.preventDefault();
		const projectId = task.projectId;
		if (!recipient || !projectId || !messageBody.trim()) return;
		setMessageStatus("Sending…");
		try {
			if (replyTo) {
				const latest = await api.task(task.id, projectId);
				if (!latest.humanActions?.some((action) => action.messageId === replyTo)) { setMessageStatus("Already answered. Refresh the task."); return; }
			}
			const result = await api.agentMessage({ projectId, recipientKind: recipient.address.kind, recipientId: recipient.address.id, originTaskId: task.id, body: messageBody.trim(), kind: replyTo ? "answer" : "question", replyTo, replyRequired: !replyTo, yieldSender: !replyTo && yieldSender });
			if (!result.ok || result.refused) { setMessageStatus(result.reason || "Could not save message."); return; }
			setMessageBody("");
			setReplyTo(undefined);
			setYieldSender(false);
			setMessageStatus(replyTo ? "Answer saved for delivery." : "Question saved for delivery.");
		} catch (error) {
			setMessageStatus(error instanceof Error ? error.message : "Could not save question.");
		}
	};

  const proofRows = [...task.acceptance.map((row) => row.proof), ...task.verification.map((row) => row.result)].map((value) => proofStatusForCurrentAttempt(value, run));
  const route = task.effectiveExecute;
  const routingSummary = [tierLabel(task.authoredWorkLevel ?? route?.work_level).split(" · ")[0], [route?.harness, route?.model].filter(Boolean).join(" ")].filter(Boolean).join(" · ");
  const hasDecision = humanActions.length > 0 || Boolean(task.agentAccessApprovals?.length);

  return (
    <div className="wux-inspector-scrim-wrap">
      <button type="button" aria-label="Close task panel" data-testid="inspector-scrim" className="wux-inspector-scrim" onClick={onClose} tabIndex={-1} />
      <section
        ref={panelRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="false"
        aria-label={`Task ${task.id}: ${task.title}`}
        data-testid="inspector-panel"
        data-task-id={task.id}
        data-inspector-state="ready"
        className="wux-inspector"
      >
        <InspectorHeader eyebrow={task.id} title={task.title} onClose={onClose} onOpen={() => onOpenTask(task.id)} />

        <div className="wux-inspector-body">
          <div className="flex flex-wrap items-center gap-2">
            <StageChip label={DISPLAY_STATE_LABEL[stage.state]} tone={stage.tone} />
            <span data-testid="inspector-next-action" role="status" className="min-w-0 flex-1 text-[12px] leading-5 text-muted">{stagePhrase(task, run, stage, reviewMember, hasDecision)}</span>
          </div>

          <div className="mt-3 flex flex-wrap gap-2 empty:hidden">
            {reviewMember?.phase === "failed" && reviewMember.lane === "review" ? <button type="button" className="wux-inspector-action wux-inspector-action-primary" disabled={recovery.isPending} aria-busy={recovery.isPending} onClick={() => recovery.mutate("retry_review")}>{recovery.isPending ? "Queuing review…" : "Retry review"}</button> : null}
            {reviewMember?.phase === "proof_blocked" ? <button type="button" className="wux-inspector-action wux-inspector-action-primary" disabled={recovery.isPending || reviewMember.proofInvalidation?.kind === "unavailable"} aria-busy={recovery.isPending} onClick={() => recovery.mutate("rerun_checks")} title={reviewMember.proofInvalidation?.kind === "unavailable" ? reviewMember.proofInvalidation.explanation : "Run the current verification commands"}>{recovery.isPending ? "Running checks…" : "Rerun checks"}</button> : null}
            {reviewMember?.recovery?.action === "adopt_completed" ? <button type="button" className="wux-inspector-action wux-inspector-action-primary" disabled={recovery.isPending || !reviewMember.recovery.enabled} aria-busy={recovery.isPending} onClick={() => recovery.mutate("adopt_completed")} title={reviewMember.recovery.enabled ? "Verify the work already in this project and record it against this task." : reviewMember.recovery.reason}>{recovery.isPending ? "Reconciling…" : "Reconcile completed work"}</button> : null}
            {reviewMember?.recovery?.action === "recover_unknown" ? <button type="button" className="wux-inspector-action wux-inspector-action-primary" disabled={recovery.isPending || !reviewMember.recovery.enabled} aria-busy={recovery.isPending} onClick={() => recovery.mutate("recover_unknown")} title={reviewMember.recovery.enabled ? "Inspect and preserve any existing work, verify it, then continue what remains." : reviewMember.recovery.reason}>{recovery.isPending ? "Verifying…" : RECOVERY_ACTION_LABEL}</button> : null}
            {showStart && runnable ? <button type="button" className="wux-inspector-action wux-inspector-action-primary inline-flex items-center gap-1.5" disabled={taskStart.isPending || directiveQueued} onClick={() => taskStart.mutate()} aria-label={`Run task ${task.id}`} title={runBlocker ?? "Queue this task"}><Play size={13} aria-hidden="true" />{directiveQueued ? "Queued" : taskStart.isPending ? "Queuing…" : "Run task"}</button> : null}
          </div>
          {reviewMember?.recovery?.action === "adopt_completed" ? <p data-testid="inspector-adopt-detail" className="mt-1 text-[11.5px] text-muted">{reviewMember.recovery.enabled ? "Verify the work already in this project and record it against this task." : reviewMember.recovery.reason}{" · "}Implementation source: Not recorded</p> : null}
          <ActionResultLine className="mt-2" pending={taskStart.isPending} error={taskStart.error} result={taskStart.data} />
          <ActionResultLine className="mt-2" pending={recovery.isPending} error={recovery.error} result={recovery.data} />

          {humanActions.length > 0 ? (
            <div className="mt-3" data-testid="inspector-decision">
              {humanActions.map((action) => <div key={action.gateId} className="mb-3 last:mb-0" title={`Owner: ${task.gates.find((gate) => gate.id === action.gateId)?.owner || "Human owner"}`}><HumanActionCard action={action} taskId={task.id} taskTitle={task.title} projectId={task.projectId} approvals={task.agentAccessApprovals} compact /></div>)}
            </div>
          ) : task.agentAccessApprovals?.length ? (
            <div className="mt-3" data-testid="inspector-decision">
              <AgentAccessApprovalList projectId={task.projectId} taskId={task.id} approvals={task.agentAccessApprovals} compact />
            </div>
          ) : null}
          {run?.outcome === "failed" && (
            <p data-testid="inspector-failure" className="mt-3 rounded-lg border border-fail/30 bg-fail-soft px-3 py-2 text-[12.5px] leading-relaxed text-fail">
              <strong>Last attempt failed.</strong>
              {run.error ? ` ${run.error}` : " See the run log before retrying."}
            </p>
          )}

          <div data-testid="inspector-intent" className="wux-inspector-intent mt-4" aria-label="What this task achieves">
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              components={{
                a: ({ href, children }) =>
                  typeof href === "string" && isSafeHref(href) ? (
                    <a href={href}>{children}</a>
                  ) : (
                    <span>{children}</span>
                  ),
              }}
            >
              {taskIntent.markdown}
            </ReactMarkdown>
          </div>
          {taskIntent.fromTitle ? <p className="mt-1 text-[11.5px] text-muted">Authored intent is unavailable; this uses the task title.</p> : null}

          <div className="mt-5 space-y-2">
            <Section title="Contract" summary={`${task.acceptance.length} acceptance · ${task.nonGoals.length} non-goals`}>
              <TaskContractDisclosure body={task.body} projectId={task.projectId ?? ""} open />
              {task.nonGoals.length > 0 && (
                <div data-testid="inspector-nongoals">
                  <h3 className="wux-inspector-h">Non-goals</h3>
                  <ul className="list-disc space-y-1 pl-5 text-[12.5px] leading-5 text-muted">
                    {task.nonGoals.map((goal) => <li key={goal}>{goal}</li>)}
                  </ul>
                </div>
              )}
              <dl data-testid="inspector-metadata" className="space-y-1.5 text-[12px]">
                {[
                  ["Epic", `${task.epicId} · ${task.epicTitle}`],
                  ["Priority", task.priority.toUpperCase()],
                  ["Risk", task.risk],
                  ["Readiness", task.readiness.replaceAll("_", " ")],
                  ["Updated", task.updatedAt],
                ].map(([name, value]) => (
                  <div key={name} className="flex gap-2">
                    <dt className="w-20 flex-none text-muted">{name}</dt>
                    <dd className="text-ink">{value}</dd>
                  </div>
                ))}
              </dl>
            </Section>

            {task.projectId ? <Section title="Routing" summary={routingSummary}><TaskRouting detail={task} run={run} projectId={task.projectId} /></Section> : null}

            <Section title="Proof" summary={[accepted ? "Accepted" : proofRows.length ? `${proofRows.filter((value) => value === "pass").length} of ${proofRows.length} pass` : "No checks", proofInvalidationSummary(reviewMember?.proofInvalidation), staleProofCount > 0 && `${staleProofCount} not current`].filter(Boolean).join(" · ")}>
              {reviewMember?.proofInvalidation ? <div data-testid="inspector-proof-invalidation" className="rounded-lg border border-line-soft bg-panel px-3 py-2 text-[11.5px] leading-5 text-muted"><strong className="text-ink-soft">Why current proof is invalid:</strong> {reviewMember.proofInvalidation.explanation}{reviewMember.proofInvalidation.previous ? <div><span className="font-medium text-ink-soft">Previous verified material:</span> <code className="break-all">{reviewMember.proofInvalidation.previous}</code></div> : null}{reviewMember.proofInvalidation.current ? <div><span className="font-medium text-ink-soft">Latest attempted material:</span> <code className="break-all">{reviewMember.proofInvalidation.current}</code></div> : null}<div>The complete receipt history remains under Exact verification below.</div></div> : null}
              {(failures.acceptance.length > 0 || failures.verification.length > 0) && (
                <p data-testid="inspector-failure" className="rounded-lg border border-fail/30 bg-fail-soft px-3 py-2 text-[12.5px] leading-relaxed text-fail">
                  <strong>Failing checks:</strong>{" "}
                  {[
                    failures.acceptance.length > 0 && `${failures.acceptance.length} acceptance`,
                    failures.verification.length > 0 && `${failures.verification.length} verification`,
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </p>
              )}
              {staleProofCount > 0 && (
                <p data-testid="inspector-stale-proof" className="rounded-lg border border-warn/30 bg-warn-soft px-3 py-2 text-[12.5px] leading-relaxed text-warn">
                  <strong>Previous proof is not current.</strong> Run the check again for this attempt.
                </p>
              )}
              {accepted ? <p data-testid="inspector-accepted" className="rounded-lg border border-pass/30 bg-pass-soft px-3 py-2 text-[12.5px] leading-relaxed text-ink">{accepted.summary}</p> : null}
              <div data-testid="inspector-acceptance">
                <h3 className="wux-inspector-h">Acceptance ({task.acceptance.length})</h3>
                <ul className="space-y-2">
                  {task.acceptance.map((row) => {
                    const proof = proofStatusForCurrentAttempt(row.proof, run);
                    return (
                      <li key={row.id} className="flex items-start gap-2 text-[12.5px] leading-5">
                        <span className="font-mono text-[10.5px] text-faint">{row.id}</span>
                        <span className="flex-1 text-ink">{row.text}</span>
                        <ProofChip value={proof} />
                      </li>
                    );
                  })}
                </ul>
              </div>
              <div data-testid="inspector-verification">
                <h3 className="wux-inspector-h">Exact verification ({task.verification.length})</h3>
                <ul className="space-y-3">
                  {task.verification.map((row) => (
                    <li key={row.id} className="text-[12px]">
                      <code className="block break-words font-mono text-[11px] leading-5 text-ink-soft">{row.command}</code>
                      <ProofChip value={proofStatusForCurrentAttempt(row.result, run)} />
                      {row.detail && <p className="mt-1 leading-5 text-muted">{row.detail}</p>}
                    </li>
                  ))}
                </ul>
              </div>
              {task.evidence.length > 0 ? (
                <div>
                  <h3 className="wux-inspector-h">Evidence</h3>
                  <ul className="space-y-2">
                    {task.evidence.map((item) => (
                      <li key={item.id} data-testid="inspector-evidence-item" className="rounded-lg border border-line bg-raised px-3 py-2">
                        {item.availability === "available" && item.href && isSafeHref(item.href) ? <a href={item.href} className="truncate text-[12.5px] font-medium text-ink-soft underline">Open {item.label}</a> : <div className="truncate text-[12.5px] font-medium text-ink-soft">{item.label}</div>}
                        <div className="mt-0.5 truncate font-mono text-[10.5px] text-faint">
                          {item.kind.replaceAll("_", " ")} · {item.kept ? "Kept" : item.availability === "expired" ? `Evidence expired${item.expiredAt ? ` ${item.expiredAt}` : ""}` : item.availability} · {item.ref}
                        </div>
                      </li>
                    ))}
                  </ul>
                  <p className="mt-2 text-[11.5px] leading-5 text-faint">Appearing here is not acceptance.</p>
                </div>
              ) : null}
            </Section>

            {task.projectId && run ? <Link data-testid="inspector-open-run" to="/p/$projectId/runs/$taskId" params={{ projectId: task.projectId, taskId: task.id }} className="block text-[12.5px] text-info underline">Open run logs and details <span aria-hidden="true">→</span></Link> : null}
            <Section title="Activity" summary={attemptDescription}>
              <h3 className="wux-inspector-h">{stage.live ? "Current attempt" : "Latest attempt"}</h3>
              <dl className="space-y-2 text-[12.5px] leading-5">
                <div className="flex gap-2">
                  <dt className="w-24 flex-none text-muted">Profile / model</dt>
                  <dd data-testid="inspector-attempt-profile" className="min-w-0 break-words text-ink">{identity.state === "identity" && run ? run.runnerProfile || identity.provider : "Unavailable"} · {identity.state === "identity" && run ? run.model || identity.model : "Unavailable"}</dd>
                </div>
                <div className="flex gap-2">
                  <dt className="w-24 flex-none text-muted">Identity</dt>
                  <dd data-testid="inspector-identity" className="min-w-0 break-words text-ink">{identitySummary(identity)}</dd>
                </div>
                <div className="flex gap-2">
                  <dt className="w-24 flex-none text-muted">Attempt</dt>
                  <dd data-testid="inspector-run" className="min-w-0 break-words text-muted">
                    {attemptDescription}
                    {currentAttempt?.id ? <span data-testid="inspector-current-attempt-id"> · {currentAttempt.id}</span> : null}
                  </dd>
                </div>
                <div className="flex gap-2">
                  <dt className="w-24 flex-none text-muted">Last activity</dt>
                  <dd data-testid="inspector-event" className="min-w-0 break-words text-muted">
                    {lastEvent ? <><time dateTime={lastEvent.ts}>{lastEvent.ts}</time>{run ? <> · <span data-testid="inspector-staleness">{run.liveness === "fresh" ? "Fresh activity" : run.liveness === "stale" ? "No recent activity" : "Process not observed"}</span> · {Number.isFinite(run.sinceLastEventSec) ? `${duration(Math.max(0, run.sinceLastEventSec))} ago` : "age unavailable"}</> : null}<div className="text-ink-soft">{lastEvent.text}</div></> : <span data-testid="inspector-staleness">Unavailable — no activity recorded</span>}
                  </dd>
                </div>
              </dl>
              {previousAttempts.length > 0 ? <details data-testid="inspector-history"><summary>Previous attempts ({previousAttempts.length})</summary><ul className="mt-2 space-y-1.5 text-[11.5px] text-muted">{previousAttempts.map((history) => <li key={history.id ?? `${history.lane ?? "execute"}:${history.n}:${history.startedAt}`} className="break-words"><span data-testid="inspector-historical-attempt">{laneLabel(history.lane)} · attempt {history.n} · {history.outcome.replaceAll("-", " ")}{history.id ? ` · ${history.id}` : ""}</span>{history.runner ? ` · ${history.runner}` : ""}</li>)}</ul></details> : null}
              <details data-testid="agent-coordination">
                <summary>Agent coordination</summary>
                <div className="mt-3"><AgentCoordinationSummary task={task} run={run} />
                {!task.contacts?.length && <p className="mt-2 text-[12.5px] text-muted">No contacts recorded.</p>}
                {task.messages?.length ? <ol className="mt-3 space-y-2">{task.messages.map((message) => <li key={message.id} className="rounded-lg border border-line bg-raised px-3 py-2.5"><div className="text-[12.5px] text-ink">{message.body}</div><div className="mt-1 font-mono text-[10px] text-faint">{message.kind.replaceAll("_", " ")} · {message.sender} · {message.transportState} · {message.state}{message.yieldSender ? " · sender yields" : ""}</div>{message.kind === "question" && message.recipient.kind === "task" && message.recipient.id === task.id && !task.messages?.some((answer) => answer.replyTo === message.id) ? <button type="button" onClick={() => setReplyTo(message.id)} className="mt-2 text-[11.5px] underline">Reply</button> : null}</li>)}</ol> : null}
                <form className="mt-3 space-y-2" onSubmit={sendMessage}>
                  {!replyTo && (task.contacts?.length || 0) > 1 ? <select aria-label="Message recipient" value={contactIndex} onChange={(event) => setContactIndex(Number(event.target.value))} className="w-full rounded-lg border border-line bg-panel px-3 py-2 text-[12.5px] text-ink">{task.contacts?.map((item, index) => <option key={`${item.role}:${item.name || index}`} value={index}>{item.role === "peer" ? item.name || "Peer" : item.role}</option>)}</select> : null}
                  <label className="block text-[11.5px] text-muted" htmlFor={`agent-message-${task.id}`}>{replyTo ? `Reply to ${replyMessage?.sender}` : `Ask ${contact?.role === "peer" ? contact.name || "peer" : contact?.role || "contact"}`}</label>
                  <textarea id={`agent-message-${task.id}`} value={messageBody} onChange={(event) => setMessageBody(event.target.value)} disabled={!recipient || !task.projectId} rows={3} maxLength={32768} className="w-full rounded-lg border border-line bg-panel px-3 py-2 text-[12.5px] text-ink" />
                  {!replyTo && <label className="flex items-center gap-2 text-[11.5px] text-muted"><input type="checkbox" checked={yieldSender} onChange={(event) => setYieldSender(event.target.checked)} />Pause sender until this question is answered</label>}
                  <button type="submit" disabled={!recipient || !task.projectId || !messageBody.trim()} className="wux-inspector-action">{replyTo ? "Send answer" : "Send question"}</button>
                  {replyTo ? <button type="button" onClick={() => setReplyTo(undefined)} className="wux-inspector-action">Cancel reply</button> : null}
                  {messageStatus ? <p role="status" className="text-[11.5px] text-muted">{messageStatus}</p> : null}
                </form></div>
              </details>
            </Section>
          </div>
        </div>
      </section>
    </div>
  );
}

function ProofChip({ value }: { value: string }) {
  return (
    <span className={`inline-flex rounded-full border px-2 py-0.5 text-[10.5px] font-medium ${value === "pass" ? "border-pass/30 bg-pass-soft text-pass" : value === "fail" ? "border-fail/30 bg-fail-soft text-fail" : "border-line bg-panel/60 text-muted"}`}>
      {value === "not-current" ? "not current" : value}
    </span>
  );
}
