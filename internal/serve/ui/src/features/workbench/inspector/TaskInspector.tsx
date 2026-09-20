import { useEffect, useRef, useState, type FormEvent, type Ref } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Play, X } from "lucide-react";
import type { RunDetail, TaskDetail, WaveReviewMember } from "@/types/domain";
import { api } from "@/lib/api";
import { duration } from "@/lib/time";
import { ActionResultLine } from "@/components/ui/action-feedback";
import { statusLabelOf } from "@/components/ui/tone";
import { useRecovery, useTaskStart } from "@/lib/queries";
import { AgentAccessApprovalList, HumanActionCard } from "@/features/human-action/HumanActionCard";
import { AgentCoordinationSummary, taskRunBlocker, TaskContractDisclosure, TaskRouting } from "@/features/product/TaskScreens";
import { isSafeHref } from "@/features/editor/sanitize";
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
  nextActionForStage,
  outcomeLabel,
  observedRunIdentity,
  proofStatusForCurrentAttempt,
  resolveVisibleRun,
  resolveVisibleTask,
  stageFromWaveReviewMember,
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
}: {
  eyebrow: string;
  title: string;
  onClose: () => void;
}) {
  return (
    <div className="wux-inspector-head">
      <div className="min-w-0 flex-1">
        <div data-testid="inspector-header-id" className="break-words font-mono text-[10.5px] font-semibold uppercase tracking-[0.14em] text-faint">
          {eyebrow}
        </div>
        <h2 data-testid="inspector-header-title" className="mt-1 break-words text-[17px] font-semibold leading-snug tracking-[-0.015em] text-ink">
          {title}
        </h2>
      </div>
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
  const blockers = task.deps.filter((dep) => dep.status !== "done");
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
  const showStart = !reviewMemberStopsStart && !stage.live && !["done", "review", "blocked"].includes(currentStatus);
	const [messageBody, setMessageBody] = useState("");
	const [messageStatus, setMessageStatus] = useState("");
	const [contactIndex, setContactIndex] = useState(0);
	const [yieldSender, setYieldSender] = useState(false);
	const [replyTo, setReplyTo] = useState<string | undefined>();
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
			await api.agentMessage({ projectId, recipientKind: recipient.address.kind, recipientId: recipient.address.id, originTaskId: task.id, body: messageBody.trim(), kind: replyTo ? "answer" : "question", replyTo, replyRequired: !replyTo, yieldSender: !replyTo && yieldSender });
			setMessageBody("");
			setReplyTo(undefined);
			setYieldSender(false);
			setMessageStatus(replyTo ? "Answer saved for delivery." : "Question saved for delivery.");
		} catch (error) {
			setMessageStatus(error instanceof Error ? error.message : "Could not save question.");
		}
	};

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
        <InspectorHeader eyebrow={task.id} title={task.title} onClose={onClose} />

        <div className="wux-inspector-body">
          <div className="flex flex-wrap items-center gap-2">
            <StageChip label={stage.label} tone={stage.tone} />
            {reviewMember?.phase === "failed" && reviewMember.lane === "review" ? <button type="button" className="wux-inspector-action wux-inspector-action-primary ml-auto" disabled={recovery.isPending} aria-busy={recovery.isPending} onClick={() => recovery.mutate("retry_review")}>{recovery.isPending ? "Queuing review…" : "Retry review"}</button> : null}
            {reviewMember?.phase === "proof_blocked" ? <button type="button" className="wux-inspector-action wux-inspector-action-primary ml-auto" disabled={recovery.isPending || reviewMember.proofInvalidation?.kind === "unavailable"} aria-busy={recovery.isPending} onClick={() => recovery.mutate("rerun_checks")} title={reviewMember.proofInvalidation?.kind === "unavailable" ? reviewMember.proofInvalidation.explanation : "Run the current verification commands"}>{recovery.isPending ? "Running checks…" : "Rerun checks"}</button> : null}
            {showStart ? <button type="button" className="wux-inspector-action wux-inspector-action-primary ml-auto inline-flex items-center gap-1.5" disabled={!runnable || taskStart.isPending || directiveQueued} onClick={() => taskStart.mutate()} aria-label={`Start task ${task.id}`} title={runBlocker ?? "Authorize this exact task for the configured runtime"}><Play size={13} aria-hidden="true" />{directiveQueued ? "Authorized — waiting for runtime" : taskStart.isPending ? "Starting…" : "Start task"}</button> : null}
          </div>
          <p data-testid="inspector-next-action" role="status" className="mt-2 text-[12px] leading-5 text-muted">{reviewMember?.proofInvalidation?.explanation ?? (reviewMember?.phase === "failed" && reviewMember.waitingReason ? reviewMember.waitingReason : reviewMember?.phase === "proof_blocked" ? "Run the required verification again before this task can be accepted." : nextActionForStage(task, run, stage))}</p>
          {reviewMember?.proofInvalidation ? <div data-testid="inspector-proof-invalidation" className="mt-2 rounded-lg border border-line-soft bg-panel px-3 py-2 text-[11.5px] leading-5 text-muted"><strong className="text-ink-soft">Why current proof is invalid:</strong> {reviewMember.proofInvalidation.explanation}{reviewMember.proofInvalidation.previous ? <div><span className="font-medium text-ink-soft">Previous verified material:</span> <code className="break-all">{reviewMember.proofInvalidation.previous}</code></div> : null}{reviewMember.proofInvalidation.current ? <div><span className="font-medium text-ink-soft">Latest attempted material:</span> <code className="break-all">{reviewMember.proofInvalidation.current}</code></div> : null}<div>The complete receipt history remains under Exact verification below.</div></div> : null}
          {runBlocker && showStart ? <p role="status" className="mt-1 text-[11.5px] text-muted">{runBlocker}</p> : null}
          <ActionResultLine className="mt-2" pending={taskStart.isPending} error={taskStart.error} result={taskStart.data} />
          <ActionResultLine className="mt-2" pending={recovery.isPending} error={recovery.error} result={recovery.data} />

          {humanActions.length > 0 ? (
            <div className="mt-4" data-testid="inspector-decision">
              {humanActions.map((action) => <div key={action.gateId} className="mb-3 last:mb-0"><p className="mb-2 font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Owner: {task.gates.find((gate) => gate.id === action.gateId)?.owner || "Human owner"}</p><HumanActionCard action={action} taskId={task.id} taskTitle={task.title} projectId={task.projectId} approvals={task.agentAccessApprovals} compact /></div>)}
            </div>
          ) : task.agentAccessApprovals?.length ? (
            <div className="mt-4" data-testid="inspector-decision">
              <AgentAccessApprovalList projectId={task.projectId} taskId={task.id} approvals={task.agentAccessApprovals} compact />
            </div>
          ) : blockers.length > 0 ? (
            <p data-testid="inspector-decision" className="mt-4 rounded-lg border border-warn/30 bg-warn-soft px-4 py-3 text-[12.5px] leading-relaxed text-warn">
              <strong>Blocked by prerequisites.</strong>{" "}
              {blockers.map((dep) => `${dep.id} (${statusLabelOf(dep.status)})`).join(" · ")}
            </p>
          ) : null}
          {run?.outcome === "failed" && (
            <p data-testid="inspector-failure" className="mt-4 rounded-lg border border-fail/30 bg-fail-soft px-4 py-3 text-[12.5px] leading-relaxed text-fail">
              <strong>Last attempt failed.</strong>
              {run.error ? ` ${run.error}` : " See the run log before retrying."}
            </p>
          )}
          {(failures.acceptance.length > 0 || failures.verification.length > 0) && (
            <p data-testid="inspector-failure" className="mt-4 rounded-lg border border-fail/30 bg-fail-soft px-4 py-3 text-[12.5px] leading-relaxed text-fail">
              <strong>Failing checks need action.</strong>{" "}
              {[
                failures.acceptance.length > 0 && `${failures.acceptance.length} acceptance`,
                failures.verification.length > 0 && `${failures.verification.length} verification`,
              ]
                .filter(Boolean)
                .join(" · ")}
              {" — "}details in the disclosures below.
            </p>
          )}
          {staleProofCount > 0 && (
            <p data-testid="inspector-stale-proof" className="mt-4 rounded-lg border border-warn/30 bg-warn-soft px-4 py-3 text-[12.5px] leading-relaxed text-warn">
              <strong>Previous proof is not current.</strong> Run the check again for this attempt before treating it as proof.
            </p>
          )}

          <section aria-label="What this task achieves" className="mt-6">
            <h3 className="wux-inspector-h">What this task achieves</h3>
            <div data-testid="inspector-intent" className="wux-inspector-intent">
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
            {taskIntent.fromTitle ? <p className="mt-2 text-[11.5px] text-muted">Authored intent is unavailable; this uses the task title.</p> : null}
          </section>

          <TaskContractDisclosure body={task.body} projectId={task.projectId ?? ""} />

          {task.projectId ? <div className="mt-6"><TaskRouting detail={task} run={run} projectId={task.projectId} /></div> : null}

          <section aria-label="Active attempt" className="mt-6">
            <h3 className="wux-inspector-h">{stage.live ? "Current attempt" : "Latest attempt"}</h3>
            <dl className="space-y-2 text-[12.5px] leading-5">
              <div className="flex gap-2">
                <dt className="w-24 flex-none font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Profile / model</dt>
                <dd data-testid="inspector-attempt-profile" className="min-w-0 break-words text-ink">{identity.state === "identity" && run ? run.runnerProfile || identity.provider : "Unavailable"} · {identity.state === "identity" && run ? run.model || identity.model : "Unavailable"}</dd>
              </div>
              <div className="flex gap-2">
                <dt className="w-24 flex-none font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Identity</dt>
                <dd data-testid="inspector-identity" className="min-w-0 break-words text-ink">{identitySummary(identity)}</dd>
              </div>
              <div className="flex gap-2">
                <dt className="w-24 flex-none font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Attempt</dt>
                <dd data-testid="inspector-run" className="min-w-0 break-words text-muted">
                  {attemptDescription}
                  {currentAttempt?.id ? <span data-testid="inspector-current-attempt-id"> · {currentAttempt.id}</span> : null}
                </dd>
              </div>
              <div className="flex gap-2">
                <dt className="w-24 flex-none font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Last activity</dt>
                <dd data-testid="inspector-event" className="min-w-0 break-words text-muted">
                  {lastEvent ? <><time dateTime={lastEvent.ts}>{lastEvent.ts}</time>{run ? <> · <span data-testid="inspector-staleness">{run.liveness === "fresh" ? "Fresh activity" : run.liveness === "stale" ? "No recent activity" : "Process not observed"}</span> · {Number.isFinite(run.sinceLastEventSec) ? `${duration(Math.max(0, run.sinceLastEventSec))} ago` : "age unavailable"}</> : null}<div className="text-ink-soft">{lastEvent.text}</div></> : <span data-testid="inspector-staleness">Unavailable — no activity recorded</span>}
                </dd>
              </div>
              {task.projectId && run ? <div className="flex gap-2"><dt className="w-24 flex-none font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Details</dt><dd className="min-w-0 break-words"><a data-testid="inspector-open-run" href={`/p/${encodeURIComponent(task.projectId)}/runs/${encodeURIComponent(task.id)}`} className="text-info underline">Open run logs and details <span aria-hidden="true">→</span></a></dd></div> : null}
            </dl>
            {previousAttempts.length > 0 ? <details data-testid="inspector-history" className="mt-3"><summary>Previous attempts ({previousAttempts.length})</summary><ul className="mt-2 space-y-1.5 text-[11.5px] text-muted">{previousAttempts.map((history) => <li key={history.id ?? `${history.lane ?? "execute"}:${history.n}:${history.startedAt}`} className="break-words"><span data-testid="inspector-historical-attempt">{laneLabel(history.lane)} · attempt {history.n} · {history.outcome.replaceAll("-", " ")}{history.id ? ` · ${history.id}` : ""}</span>{history.runner ? ` · ${history.runner}` : ""}</li>)}</ul></details> : null}
          </section>

		  <details className="mt-6" data-testid="agent-coordination">
			<summary>Agent coordination (technical details)</summary>
			<div className="mt-3"><AgentCoordinationSummary task={task} run={run} />
			{!task.contacts?.length && <p className="mt-2 text-[12.5px] text-muted">No contacts recorded.</p>}
			{task.messages?.length ? <ol className="mt-3 space-y-2">{task.messages.map((message) => <li key={message.id} className="rounded-lg border border-line bg-raised px-3 py-2.5"><div className="text-[12.5px] text-ink">{message.body}</div><div className="mt-1 font-mono text-[10px] text-faint">{message.kind.replaceAll("_", " ")} · {message.sender} · {message.transportState} · {message.state}{message.yieldSender ? " · sender yields" : ""}</div>{message.kind === "question" && message.recipient.kind === "task" && message.recipient.id === task.id && !task.messages?.some((answer) => answer.replyTo === message.id) ? <button type="button" onClick={() => setReplyTo(message.id)} className="mt-2 text-[11.5px] underline">Reply</button> : null}</li>)}</ol> : <p className="mt-2 text-[11.5px] text-faint">No questions or replies.</p>}
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

          {accepted ? (
            <section aria-label="Accepted result" className="mt-6">
              <h3 className="wux-inspector-h">Accepted result</h3>
              <p data-testid="inspector-accepted" className="rounded-lg border border-pass/30 bg-pass-soft px-4 py-3 text-[12.5px] leading-relaxed text-ink">
                {accepted.summary}
              </p>
            </section>
          ) : null}

          <section aria-label="Available evidence" className="mt-6">
            <h3 className="wux-inspector-h">Available evidence</h3>
            {task.evidence.length === 0 ? (
              <p data-testid="inspector-evidence-empty" className="text-[12.5px] leading-5 text-muted">
                No additional attachments. Check results are listed below.
              </p>
            ) : (
              <>
                <ul className="space-y-2">
                  {task.evidence.map((item) => (
                    <li
                      key={item.id}
                      data-testid="inspector-evidence-item"
                      className="rounded-lg border border-line bg-raised px-3 py-2.5"
                    >
					  {item.availability === "available" && item.href && isSafeHref(item.href) ? <a href={item.href} className="truncate text-[12.5px] font-medium text-ink-soft underline">Open {item.label}</a> : <div className="truncate text-[12.5px] font-medium text-ink-soft">{item.label}</div>}
                      <div className="mt-0.5 truncate font-mono text-[10.5px] text-faint">
						{item.kind.replaceAll("_", " ")} · {item.kept ? "Kept" : item.availability === "expired" ? `Evidence expired${item.expiredAt ? ` ${item.expiredAt}` : ""}` : item.availability} · {item.ref}
                      </div>
                    </li>
                  ))}
                </ul>
                <p className="mt-2 text-[11.5px] leading-5 text-faint">
                  Appearing here is not acceptance — see the acceptance proofs below.
                </p>
              </>
            )}
          </section>

          <div className="mt-6 space-y-2">
            <details data-testid="inspector-acceptance">
              <summary>Acceptance ({task.acceptance.length})</summary>
              {task.acceptance.length === 0 ? (
                <p className="mt-2 text-[12.5px] text-muted">No acceptance rows recorded.</p>
              ) : (
                <ul className="mt-2 space-y-2">
                  {task.acceptance.map((row) => (
                    (() => {
                      const proof = proofStatusForCurrentAttempt(row.proof, run);
                      return (
                    <li key={row.id} className="flex items-start gap-2 text-[12.5px] leading-5">
                      <span className="font-mono text-[10.5px] text-faint">{row.id}</span>
                      <span className="flex-1 text-ink">{row.text}</span>
                      <span
                        className={`inline-flex rounded-full border px-2 py-0.5 text-[10.5px] font-medium ${
                          proof === "pass"
                            ? "border-pass/30 bg-pass-soft text-pass"
                            : proof === "fail"
                              ? "border-fail/30 bg-fail-soft text-fail"
                              : "border-line bg-panel/60 text-muted"
                        }`}
                      >
                        {proof === "not-current" ? "not current" : proof}
                      </span>
                    </li>
                      );
                    })()
                  ))}
                </ul>
              )}
            </details>

            <details data-testid="inspector-verification">
              <summary>Exact verification ({task.verification.length})</summary>
              {task.verification.length === 0 ? (
                <p className="mt-2 text-[12.5px] text-muted">No verification commands recorded.</p>
              ) : (
                <ul className="mt-2 space-y-3">
                  {task.verification.map((row) => (
                    (() => {
                      const result = proofStatusForCurrentAttempt(row.result, run);
                      return (
                    <li key={row.id} className="text-[12px]">
                      <code className="block break-words font-mono text-[11px] leading-5 text-ink-soft">{row.command}</code>
                      <span className={`mt-1 inline-flex rounded-full border px-2 py-0.5 text-[10.5px] font-medium ${result === "pass" ? "border-pass/30 bg-pass-soft text-pass" : result === "fail" ? "border-fail/30 bg-fail-soft text-fail" : "border-line bg-panel/60 text-muted"}`}>
                        {result === "not-current" ? "not current" : result}
                      </span>
                      {row.detail && <p className="mt-1 leading-5 text-muted">{row.detail}</p>}
                    </li>
                      );
                    })()
                  ))}
                </ul>
              )}
            </details>

            {task.nonGoals.length > 0 && (
              <details data-testid="inspector-nongoals">
                <summary>Non-goals ({task.nonGoals.length})</summary>
                <ul className="mt-2 list-disc space-y-1 pl-5 text-[12.5px] leading-5 text-muted">
                  {task.nonGoals.map((goal) => (
                    <li key={goal}>{goal}</li>
                  ))}
                </ul>
              </details>
            )}

            <details data-testid="inspector-metadata">
              <summary>Technical metadata</summary>
              <dl className="mt-2 space-y-2 text-[12px]">
                {[
                  ["Epic", `${task.epicId} · ${task.epicTitle}`],
                  ["Priority", task.priority.toUpperCase()],
                  ["Risk", task.risk],
                  ["Readiness", task.readiness.replaceAll("_", " ")],
                  ["Updated", task.updatedAt],
                ].map(([name, value]) => (
                  <div key={name} className="flex gap-2 border-b border-line-soft pb-1.5">
                    <dt className="w-20 flex-none font-mono text-[10px] uppercase tracking-[0.12em] text-faint">{name}</dt>
                    <dd className="text-ink">{value}</dd>
                  </div>
                ))}
              </dl>
            </details>
          </div>

          <div className="mt-6 flex flex-wrap gap-2 border-t border-line pt-4">
            <button
              type="button"
              data-testid="inspector-open-task"
              onClick={() => onOpenTask(task.id)}
              className="wux-inspector-action wux-inspector-action-primary"
            >
              Open full task <span aria-hidden="true">→</span>
            </button>
            <button type="button" onClick={onClose} className="wux-inspector-action">
              Close panel
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}
