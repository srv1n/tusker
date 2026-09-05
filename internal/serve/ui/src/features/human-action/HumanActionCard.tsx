import { useState } from "react";
import { ArrowLeft, CheckCircle2, ClipboardCheck, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/controls";
import { ActionResultLine, useConfirm } from "@/components/ui/action-feedback";
import { ProofChip } from "@/components/ui/chips";
import type { HumanReceiptBridgeResult } from "@/lib/humanReceipt";
import { useGateAction, useTaskStatusAction } from "@/lib/queries";
import type { HumanAction } from "@/types/domain";

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
  compact = false,
}: {
  action: HumanAction;
  taskId: string;
  taskTitle: string;
  projectId?: string;
  blockedTaskIds?: string[];
  compact?: boolean;
}) {
  const gateAction = useGateAction();
  const statusAction = useTaskStatusAction(taskId, projectId);
  const confirm = useConfirm();
  const [reworkReason, setReworkReason] = useState("");
  const [reworkOpen, setReworkOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [dispositionReason, setDispositionReason] = useState("");
  const [resolved, setResolved] = useState(false);
  const [receiptPending, setReceiptPending] = useState(false);
  const [receiptResult, setReceiptResult] = useState<HumanReceiptBridgeResult | null>(null);
  const [receiptError, setReceiptError] = useState<Error | null>(null);
  const controlId = action.gateId.replace(/[^A-Za-z0-9_-]/g, "-");
  const blockedIds = blockedTaskIds?.length ? blockedTaskIds : action.blockedTaskIds?.length ? action.blockedTaskIds : [taskId];

  if (resolved) return null;

  const busy = gateAction.isPending || statusAction.isPending || receiptPending;
  const result = statusAction.data ?? gateAction.data;
  const receiptFeedback = receiptResult
    ? {
        ok: receiptResult.status === "accepted",
        refused: receiptResult.status !== "accepted",
        reason:
          receiptResult.message ??
          (receiptResult.status === "cancelled" ? "Confirmation cancelled." : "Native confirmation failed."),
      }
    : undefined;

  const complete = async () => {
    if (busy) return;
    const bridge = window.tuskerShell?.requestHumanReceipt;
    if (!projectId) {
      setReceiptError(new Error("This action is missing its project context."));
      return;
    }
    if (!bridge) {
      setReceiptError(new Error("Open this task in the Tusker Mac app to confirm the action."));
      return;
    }
    setReceiptPending(true);
    setReceiptError(null);
    setReceiptResult(null);
    try {
      const response = await bridge({ projectId, gateId: action.gateId, action: "satisfy" });
      if (!response) {
        setReceiptError(new Error("Native confirmation returned no result."));
        return;
      }
      setReceiptResult(response);
      if (response.status === "accepted") setResolved(true);
    } catch (error) {
      setReceiptError(error instanceof Error ? error : new Error("Native confirmation failed."));
    } finally {
      setReceiptPending(false);
    }
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
      data-human-action-card
      data-need-card
      data-need-focus-target
      tabIndex={0}
      aria-labelledby={`human-action-${controlId}`}
      className={compact
        ? "rounded-xl border border-accent/30 bg-accent-soft/30 p-3.5 shadow-2xs"
        : "mb-7 rounded-xl border border-accent/30 bg-accent-soft/30 p-4 sm:p-5 shadow-xs"}
    >
      <div className="flex items-start gap-3">
        <div className="mt-0.5 flex h-8 w-8 flex-none items-center justify-center rounded-lg bg-accent text-surface shadow-2xs">
          <ClipboardCheck size={16} aria-hidden="true" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 id={`human-action-${controlId}`} className="text-[17px] font-semibold leading-tight text-ink">
              Your action
            </h2>
            <span className="rounded-full bg-accent/15 border border-accent/20 px-2.5 py-0.5 text-[11px] font-semibold text-accent">
              {action.title}
            </span>
          </div>
          <p className="mt-1 text-[12.5px] leading-relaxed text-muted">
            <span className="font-mono text-[11px] font-semibold text-warn">{action.gateId}</span>
            {" · blocks "}<span className="font-mono text-[11px] text-ink-soft">{blockedIds.join(", ")}</span>{" · "}{taskTitle}
          </p>
        </div>
      </div>

      <div className="mt-4 grid gap-3 text-[13px] leading-relaxed text-ink-soft">
        <div>
          <div className="mb-0.5 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-faint">Do this</div>
          <p className="text-[13.5px] font-medium text-ink">{action.action}</p>
        </div>
        <div>
          <div className="mb-0.5 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-faint">Why you’re needed</div>
          <p className="text-muted">{action.whyAgentCannot}</p>
        </div>
        <div>
          <div className="mb-0.5 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-faint">Done when</div>
          <p className="text-muted">{action.completionCondition}</p>
        </div>
      </div>

      {action.acceptance.length > 0 && (
        <div className="mt-4 overflow-hidden rounded-xl border border-line bg-raised shadow-2xs">
          <div className="border-b border-line bg-panel/60 px-3.5 py-2 font-mono text-[10px] font-semibold uppercase tracking-[0.12em] text-faint">
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
            <p className="rounded-lg border border-line-soft bg-surface/70 px-3 py-2.5 text-[12px] leading-relaxed text-muted">
              Tusker will open a native confirmation showing the server-authorized action. The gate changes only after that confirmation succeeds.
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" variant="primary" disabled={busy} onClick={() => void complete()}>
                <CheckCircle2 size={14} aria-hidden="true" />
                Mark complete
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
        <div className="border-t border-line/60 pt-2">
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
        <ActionResultLine
          pending={busy}
          error={receiptError ?? statusAction.error ?? gateAction.error}
          result={receiptFeedback ?? result}
        />
      </div>
    </section>
  );
}
