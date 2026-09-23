import { useEffect, useMemo, useState } from "react";
import { ArrowLeft, CheckCircle2, ClipboardCheck, LoaderCircle, RotateCcw, ShieldAlert, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/controls";
import { ActionResultLine, useConfirm } from "@/components/ui/action-feedback";
import { ProofChip } from "@/components/ui/chips";
import { useAgentAccessApprovalAction, useAgentAccessApprovals, useGateAction, useTaskStatusAction } from "@/lib/queries";
import { api } from "@/lib/api";
import type { AgentAccessApproval, HumanAction } from "@/types/domain";

/**
 * The one human-owned action surface. It accepts the served contract rather
 * than deriving meaning from raw gate kinds in the client.
 */
export function HumanActionCard({
  action,
  taskId,
  taskTitle,
  projectId,
  blockedTaskIds,
  approvals,
  onRetry,
  continueOnApproval = false,
  compact = false,
}: {
  action: HumanAction;
  taskId: string;
  taskTitle: string;
  projectId?: string;
  blockedTaskIds?: string[];
  approvals?: AgentAccessApproval[];
  onRetry?: () => void;
  continueOnApproval?: boolean;
  compact?: boolean;
}) {
  if (action.kind === "question") return <QuestionActionCard action={action} taskId={taskId} projectId={projectId} compact={compact} />;
  if (action.kind === "permission") return <section className={compact ? "border-t border-line pt-4" : "mb-7 rounded-xl border border-line bg-raised p-4 sm:p-5"} data-human-action-card data-need-card><h2 className="text-[17px] font-semibold text-ink">{action.title}</h2><AgentAccessApprovalList projectId={projectId} taskId={taskId} approvals={approvals?.filter((approval) => approval.requestId === action.requestId)} onRetry={onRetry} compact={compact} /></section>;
  const gateAction = useGateAction();
  const statusAction = useTaskStatusAction(taskId, projectId);
  const confirm = useConfirm();
  const [reworkReason, setReworkReason] = useState("");
  const [reworkOpen, setReworkOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [dispositionReason, setDispositionReason] = useState("");
  const [resolved, setResolved] = useState(false);
  const controlId = action.gateId.replace(/[^A-Za-z0-9_-]/g, "-");
  const blockedIds = blockedTaskIds?.length ? blockedTaskIds : action.blockedTaskIds?.length ? action.blockedTaskIds : [taskId];

  if (resolved) return null;

  const busy = gateAction.isPending || statusAction.isPending;
  const result = statusAction.data ?? gateAction.data;

  const complete = async () => {
    if (busy) return;
    try {
      const response = await gateAction.mutateAsync({
        gateId: action.gateId,
        action: "satisfy",
        body: { evidence: "Approved in the authenticated Tusker app.", materialRevision: action.materialRevision },
        taskId,
        projectId,
      });
      if (response.ok && !response.refused) setResolved(true);
    } catch { /* mutation state renders the actionable error and always settles */ }
  };

  const sendBack = async () => {
    const reason = reworkReason.trim();
    if (!reason || busy) return;
    const response = await statusAction.mutateAsync({ status: "rework", reason });
    if (response.ok && !response.refused) setResolved(true);
  };

  const disposeGate = async (gateDisposition: "waive" | "obsolete") => {
    const reason = dispositionReason.trim();
    if (!reason || busy) return;
    const ok = await confirm({
      title: `${gateDisposition === "waive" ? "Waive" : "Mark obsolete"} ${action.gateId}?`,
      body: gateDisposition === "waive"
        ? "This bypasses the gate without satisfying its requested human action."
        : "This removes a gate that no longer applies.",
      confirmLabel: gateDisposition === "waive" ? "Waive gate" : "Mark obsolete",
      tone: "danger",
    });
    if (!ok) return;
    const response = await gateAction.mutateAsync({
      gateId: action.gateId,
      action: gateDisposition,
      body: { reason },
      taskId,
      projectId,
    });
    if (response.ok && !response.refused) setResolved(true);
  };

  return (
    <section
      id={`human-action-card-${controlId}`}
      data-human-action-card
      data-need-card
      data-need-focus-target
      tabIndex={0}
      aria-labelledby={`human-action-${controlId}`}
      className={compact
        ? "border-t border-line pt-4"
        : "mb-7 rounded-xl border border-line bg-raised p-4 sm:p-5 shadow-xs"}
    >
      <div className={compact ? "" : "flex items-start gap-3"}>
        {!compact ? (
        <div className="mt-0.5 flex h-8 w-8 flex-none items-center justify-center rounded-lg bg-accent text-surface shadow-2xs">
          <ClipboardCheck size={16} aria-hidden="true" />
        </div>
        ) : null}
        <div className="min-w-0 flex-1">
          <div className={compact ? "" : "flex flex-wrap items-center gap-2"}>
            <h2 id={`human-action-${controlId}`} className="text-[17px] font-semibold leading-tight text-ink">
              {compact ? action.title : "Your action"}
            </h2>
            {!compact ? (
            <span className="rounded-full bg-accent/15 border border-accent/20 px-2.5 py-0.5 text-[11px] font-semibold text-accent">
              {action.title}
            </span>
            ) : null}
          </div>
          {compact ? <p className="mt-2 text-[13.5px] leading-relaxed text-ink-soft">{action.action}</p> : <p className="mt-1 text-[12.5px] leading-relaxed text-muted">
            <span className="font-mono text-[11px] font-semibold text-warn">{action.gateId}</span>
            {" · blocks "}<span className="font-mono text-[11px] text-ink-soft">{blockedIds.join(", ")}</span>{" · "}{taskTitle}
          </p>}
        </div>
      </div>

      <AgentAccessApprovalList projectId={projectId} taskId={taskId} approvals={approvals} onRetry={onRetry} compact={compact} />

      {!compact ? <div className="mt-4 grid gap-3 text-[13px] leading-relaxed text-ink-soft">
        <div>
          <div className="mb-0.5 text-[11px] font-semibold text-muted">Do this</div>
          <p className="text-[13.5px] font-medium text-ink">{action.action}</p>
        </div>
        <div>
          <div className="mb-0.5 text-[11px] font-semibold text-muted">Why you’re needed</div>
          <p className="text-muted">{action.whyAgentCannot}</p>
        </div>
        <div>
          <div className="mb-0.5 text-[11px] font-semibold text-muted">Done when</div>
          <p className="text-muted">{action.completionCondition}</p>
        </div>
        <div>
          <div className="mb-0.5 text-[11px] font-semibold text-muted">Scope and limits</div>
          <p className="text-muted">This records this decision for the affected work only. It does not mark a task complete or authorize unrelated work.</p>
        </div>
      </div> : null}

      {!compact && action.acceptance.length > 0 && (
        <div className="mt-4 overflow-hidden rounded-xl border border-line bg-raised shadow-2xs">
          <div className="border-b border-line bg-panel/60 px-3.5 py-2 text-[11px] font-semibold text-muted">
            Review checklist
          </div>
          {action.acceptance.map((row) => (
            <div key={row.id} className="flex items-start gap-2.5 border-b border-line-soft px-3.5 py-2.5 last:border-0">
              <CheckCircle2 size={14} className="mt-0.5 flex-none text-muted" aria-hidden="true" />
              <span className="min-w-0 flex-1 text-[12.5px] leading-snug text-ink-soft">{row.text}</span>
              <ProofChip proof={row.proof} />
            </div>
          ))}
        </div>
      )}

      <div className="mt-4 space-y-2.5">
        {!reworkOpen ? (
          <>
            {!compact ? <p className="rounded-lg border border-line-soft bg-surface/70 px-3 py-2.5 text-[12px] leading-relaxed text-muted">Clicking records this server-authorized action. It does not claim that implementation, verification, or acceptance is complete.</p> : null}
            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" variant="primary" disabled={busy} onClick={() => void complete()}>
                <CheckCircle2 size={14} aria-hidden="true" />
                {continueOnApproval ? "Approve and continue" : compact ? "Authorize" : "Confirm action"}
              </Button>
              <button
                type="button"
                disabled={busy}
                onClick={() => setReworkOpen(true)}
                className="inline-flex items-center gap-1.5 rounded-lg px-3 py-2 text-[12.5px] font-medium text-muted hover:bg-hover hover:text-ink-soft disabled:opacity-50"
              >
                <RotateCcw size={13} aria-hidden="true" />
                Return to rework
              </button>
            </div>
          </>
        ) : (
          <>
            <label htmlFor={`human-rework-${controlId}`} className="text-[12px] font-medium text-ink-soft">
              Why should this go back?
            </label>
            <textarea
              id={`human-rework-${controlId}`}
              value={reworkReason}
              onChange={(event) => setReworkReason(event.target.value)}
              placeholder="Describe the change needed"
              className="min-h-[72px] w-full resize-y rounded-lg border border-line bg-panel px-3 py-2.5 text-[13px] leading-relaxed text-ink outline-none placeholder:text-faint focus:border-accent/50 focus:ring-2 focus:ring-accent/15"
            />
            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" variant="danger" disabled={busy || !reworkReason.trim()} onClick={() => void sendBack()}>
                <ArrowLeft size={14} aria-hidden="true" />
                Send back to rework
              </Button>
              <button
                type="button"
                disabled={busy}
                onClick={() => setReworkOpen(false)}
                className="rounded-lg px-3 py-2 text-[12.5px] text-muted hover:bg-hover hover:text-ink-soft disabled:opacity-50"
              >
                Keep reviewing
              </button>
            </div>
          </>
        )}
        <details className="border-t border-line/60 pt-2 text-[11.5px] text-faint">
          <summary className="cursor-pointer font-medium hover:text-ink-soft">Review scope and limits</summary>
          {compact ? <div className="mt-2 space-y-2 text-[12px] leading-5 text-muted"><p>{action.whyAgentCannot}</p><p>{action.completionCondition}</p><p>This records this decision for the affected work only. It does not mark a task complete or authorize unrelated work.</p>{action.acceptance.length ? <ul className="list-disc space-y-1 pl-4">{action.acceptance.map((row) => <li key={row.id}>{row.text}</li>)}</ul> : null}<p className="font-mono text-[10.5px]">{action.gateId} · blocks {blockedIds.join(", ")} · {taskTitle}</p></div> : null}
          <div className="mt-2">
          <button
            type="button"
            disabled={busy}
            onClick={() => setMoreOpen((value) => !value)}
            className="text-[11.5px] font-medium text-faint hover:text-ink-soft disabled:opacity-50"
          >
            {moreOpen ? "Hide gate administration" : "More gate actions"}
          </button>
          {moreOpen && (
            <div className="mt-2 space-y-2 rounded-lg border border-line/70 bg-panel/45 p-3 animate-rise">
              <label htmlFor={`human-disposition-${controlId}`} className="text-[11px] font-medium text-ink-soft">
                Why is this gate being bypassed or removed?
              </label>
              <textarea
                id={`human-disposition-${controlId}`}
                value={dispositionReason}
                onChange={(event) => setDispositionReason(event.target.value)}
                placeholder="Required reason"
                className="min-h-[60px] w-full resize-y rounded-lg border border-line bg-panel px-3 py-2 text-[12.5px] text-ink outline-none placeholder:text-faint focus:border-accent/50"
              />
              <div className="flex flex-wrap gap-2">
                <Button type="button" size="sm" disabled={busy || !dispositionReason.trim()} onClick={() => void disposeGate("waive")}>
                  Waive gate
                </Button>
                <Button type="button" size="sm" variant="danger" disabled={busy || !dispositionReason.trim()} onClick={() => void disposeGate("obsolete")}>
                  Mark obsolete
                </Button>
              </div>
            </div>
          )}
          </div>
        </details>
        <ActionResultLine
          pending={busy}
          error={statusAction.error ?? gateAction.error}
          result={result}
        />
      </div>
    </section>
  );
}

function QuestionActionCard({ action, taskId, projectId, compact }: { action: HumanAction; taskId: string; projectId?: string; compact: boolean }) {
  const [body, setBody] = useState("");
  const [pending, setPending] = useState(false);
  const [status, setStatus] = useState("");
  const [answered, setAnswered] = useState(false);
  const [key, setKey] = useState(() => crypto.randomUUID());
  if (!action.messageId || answered) return null;
  const reply = async () => {
    if (!projectId || !body.trim() || pending) return;
    const message = body.trim();
    setPending(true);
    try {
      const latest = await api.task(taskId, projectId);
      if (!latest.humanActions?.some((item) => item.messageId === action.messageId)) {
        setStatus("Already answered. Refresh the task.");
        return;
      }
      const sender = action.taskId || taskId;
      const result = await api.agentMessage({ projectId, recipientKind: "task", recipientId: sender, originTaskId: taskId, body: message, kind: "answer", replyTo: action.messageId, idempotencyKey: key });
      if (!result.ok || result.refused) { setStatus(result.reason || "Could not save answer."); return; }
      const readback = await api.task(taskId, projectId);
      if (!readback.humanActions?.some((item) => item.messageId === action.messageId)) setAnswered(true);
      else setStatus("Answer saved; awaiting refreshed task state.");
    } catch (error) { setStatus(error instanceof Error ? error.message : "Could not save answer."); }
    finally { setPending(false); }
  };
  return <section className={compact ? "border-t border-line pt-4" : "mb-7 rounded-xl border border-line bg-raised p-4 sm:p-5"} data-human-action-card data-need-card>
    <h2 className="text-[17px] font-semibold text-ink">{action.title}</h2>
    <p className="mt-1 text-[11px] text-muted">Asked {action.askedAt || "recently"} · to {action.recipientLabel || "operator"}{action.yieldSender ? " · worker waiting" : ""}</p>
    <p className="mt-3 whitespace-pre-wrap text-[13px] text-ink-soft">{action.body || action.action}</p>
    <label className="mt-3 block text-[12px] font-medium text-ink-soft" htmlFor={`question-reply-${action.messageId}`}>Reply</label>
    <textarea id={`question-reply-${action.messageId}`} value={body} onChange={(event) => { setBody(event.target.value); setKey(crypto.randomUUID()); }} maxLength={32768} className="mt-1 min-h-[72px] w-full rounded-lg border border-line bg-panel px-3 py-2 text-[13px] text-ink" />
    <Button type="button" variant="primary" disabled={pending || !body.trim()} onClick={() => void reply()}>{pending ? "Sending…" : "Reply"}</Button>
    {status ? <p role="status" className="mt-2 text-[12px] text-muted">{status}</p> : null}
  </section>;
}

function approvalIsLive(approval: AgentAccessApproval): boolean {
  if (approval.state !== "pending" || approval.nativeOptionKind !== "allow_once" || !approval.nativeOptionId) return false;
  const expiresAt = Date.parse(approval.expiresAt);
  const liveUntil = Date.parse(approval.liveUntil || "");
  return Number.isFinite(expiresAt) && Number.isFinite(liveUntil) && Date.now() < Math.min(expiresAt, liveUntil);
}

function approvalArgs(value: unknown): string {
  if (typeof value === "string") return value;
  try {
    const encoded = JSON.stringify(value, null, 2);
    return encoded === undefined ? "[redacted]" : encoded;
  } catch {
    return "[redacted]";
  }
}

function approvalStateLabel(state: AgentAccessApproval["state"]): string {
  return state === "allowed" ? "Allowed once" : state.charAt(0).toUpperCase() + state.slice(1);
}

/**
 * Approval projection shared by the task page, compact inspector and needs
 * panel. It is deliberately driven by the served immutable request rather
 * than reconstructing command meaning in the browser.
 */
export function AgentAccessApprovalList({
  projectId,
  taskId,
  approvals,
  onRetry,
  compact = false,
}: {
  projectId?: string;
  taskId: string;
  approvals?: AgentAccessApproval[];
  onRetry?: () => void;
  compact?: boolean;
}) {
  const query = useAgentAccessApprovals(projectId, taskId);
  const respond = useAgentAccessApprovalAction(projectId, taskId);
  const [dismissed, setDismissed] = useState<Set<string>>(new Set());
  const [settled, setSettled] = useState<Record<string, AgentAccessApproval>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [, setClock] = useState(0);
  const source = approvals ?? query.data?.approvals ?? [];
  const unique = useMemo(() => Array.from(new Map(source.map((item) => [item.requestId, item])).values()), [source]);

  useEffect(() => {
    const pending = unique.filter((item) => item.state === "pending").map((item) => Date.parse(item.expiresAt)).filter(Number.isFinite);
    if (pending.length === 0) return;
    const timeout = window.setTimeout(() => setClock((value) => value + 1), Math.max(250, Math.min(...pending) - Date.now() + 50));
    return () => window.clearTimeout(timeout);
  }, [unique]);

  const visible = unique.filter((item) => !dismissed.has(item.requestId));
  if (visible.length === 0) return null;

  const decide = async (approval: AgentAccessApproval, decision: "allow_once" | "deny") => {
    if (!approvalIsLive(approval) || respond.isPending) return;
    setErrors((old) => { const next = { ...old }; delete next[approval.requestId]; return next; });
    try {
      const result = await respond.mutateAsync({ requestId: approval.requestId, expectedRevision: approval.stateRevision, decision });
      if (result.approval) setSettled((old) => ({ ...old, [approval.requestId]: result.approval! }));
    } catch (error) {
      setErrors((old) => ({ ...old, [approval.requestId]: error instanceof Error ? error.message : "Approval response failed; the request is still pending." }));
    }
  };

  return <section data-agent-access-approvals aria-label="Agent access approvals" className={compact ? "mt-3 space-y-2" : "mt-5 space-y-3"}>
    {visible.map((sourceApproval) => {
      const approval = settled[sourceApproval.requestId] || sourceApproval;
      return <ApprovalCard key={approval.requestId} approval={approval} error={errors[approval.requestId]} busy={respond.isPending} onDismiss={() => setDismissed((old) => new Set(old).add(approval.requestId))} onDecision={(decision) => void decide(approval, decision)} onRetry={onRetry} compact={compact} />;
    })}
  </section>;
}

function ApprovalCard({ approval, error, busy, onDismiss, onDecision, onRetry, compact }: { approval: AgentAccessApproval; error?: string; busy: boolean; onDismiss: () => void; onDecision: (decision: "allow_once" | "deny") => void; onRetry?: () => void; compact: boolean }) {
  const live = approvalIsLive(approval);
  const args = approvalArgs(approval.redactedArguments);
  const tone = approval.state === "allowed" ? "pass" : approval.state === "denied" || approval.state === "expired" ? "warn" : "info";
  const toneClasses = { pass: "border-pass/30 bg-pass-soft text-pass", warn: "border-warn/30 bg-warn-soft text-warn", info: "border-info/30 bg-info-soft text-info" } as const;
  return <article data-testid={`agent-access-approval-${approval.requestId}`} data-approval-state={approval.state} className={`rounded-xl border p-3.5 ${toneClasses[tone]}`}>
    <div className="flex items-start gap-2.5"><div className="mt-0.5 flex-none">{approval.state === "allowed" ? <ShieldCheck size={16} aria-hidden="true" /> : <ShieldAlert size={16} aria-hidden="true" />}</div><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><h3 className="text-[13px] font-semibold">Access request</h3><span className="rounded-full border border-current/25 px-2 py-0.5 text-[11px] font-semibold">{approvalStateLabel(approval.state)}</span></div><p className="mt-1 text-[12px] leading-relaxed opacity-85">{approval.tool} · {approval.route}</p></div>{approval.state === "pending" && <button type="button" onClick={onDismiss} className="rounded px-2 py-1 text-[12px] opacity-75 hover:bg-black/5 hover:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-current/40">Dismiss</button>}</div>
    <dl className={`mt-3 grid gap-2 text-[12px] ${compact ? "" : "sm:grid-cols-2"}`}><div><dt className="font-semibold opacity-75">Working folder</dt><dd className="mt-0.5 break-all font-mono text-[11px]">{approval.workingDirectory || "Unavailable"}</dd></div><div><dt className="font-semibold opacity-75">Targets</dt><dd className="mt-0.5 break-words">{approval.targets?.length ? approval.targets.join(", ") : "No resolved targets"}</dd></div></dl>
    <div className="mt-3"><p className="text-[12px] font-semibold opacity-75">Redacted arguments</p><pre className="mt-1 max-h-[20rem] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-current/15 bg-black/5 p-2.5 font-mono text-[11px] leading-relaxed">{args}</pre></div>
    {approval.reason && <p className="mt-3 text-[12px] leading-relaxed"><strong>Consequence:</strong> {approval.reason}</p>}
    <p className="mt-2 break-words font-mono text-[10px] opacity-70">Request {approval.requestId} · revision {approval.stateRevision} · policy {approval.policyFingerprint} · args {approval.argsDigest}</p>
    {approval.state === "pending" && <p className="mt-2 text-[12px] leading-relaxed">{live ? "Allow once applies only to this exact live request. Block denies it without replaying the command." : "This request is no longer demonstrably live. No approval action is available; retry creates a new request."}</p>}
    {approval.state !== "pending" && approval.terminalReason && <p className="mt-2 text-[12px] leading-relaxed">{approval.terminalReason}</p>}
    {error && <p role="alert" className="mt-2 rounded-lg border border-fail/30 bg-fail-soft px-2.5 py-2 text-[12px] text-fail">{error}</p>}
    {approval.state === "pending" && <div className="mt-3 flex flex-wrap items-center gap-2">{live ? <><Button type="button" variant="danger" size="sm" disabled={busy} onClick={() => onDecision("deny")}>{busy ? <LoaderCircle className="animate-spin" size={13} /> : null}Block</Button><Button type="button" size="sm" disabled={busy} onClick={() => onDecision("allow_once")}>{busy ? <LoaderCircle className="animate-spin" size={13} /> : null}Allow once</Button></> : onRetry ? <Button type="button" size="sm" onClick={onRetry}>Retry</Button> : <span className="text-[12px] opacity-75">Retry from the task actions to create a new request.</span>}</div>}
  </article>;
}
