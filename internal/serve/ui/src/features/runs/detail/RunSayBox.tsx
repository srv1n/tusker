import { useRef, useState } from "react";
import type { RunDetail, RunSayResponse } from "@/types/domain";
import { ActionRefusalError, api } from "@/lib/api";

export function RunSayBox({ run, onReadback }: { run: RunDetail; onReadback: () => Promise<unknown> }) {
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const [feedback, setFeedback] = useState("");
  const key = useRef<string | undefined>(undefined);
  const canSay = Boolean(run.controls?.capabilities?.find((action) => action.action === "say")?.available);
  const canContinue = Boolean(run.controls?.capabilities?.find((action) => action.action === "continue")?.available);
  const mode = canSay ? "say" : canContinue ? "continue" : null;
  const send = async () => {
    if (!mode || busyRef.current || (mode === "say" && !message.trim())) return;
    busyRef.current = true;
    setBusy(true);
    setFeedback("Sending…");
    if (!key.current) key.current = crypto.randomUUID();
    try {
      const result: RunSayResponse = mode === "say"
        ? await api.sayRun(run.taskId, message, key.current, run.projectId)
        : await api.continueRun(run.taskId, message, run.projectId);
      if (result.refused || !result.ok) {
        setFeedback(result.reason || "The server refused this message.");
      } else {
        const receipt = result.duplicate
          ? `Already sent. Delivery: ${result.delivery?.state || "stored"}.`
          : "Accepted.";
        setFeedback(`${receipt} Waiting for run readback…`);
        try { await onReadback(); setFeedback(receipt); }
        catch { setFeedback(`${receipt} Run readback is unavailable. Refresh to see its current state.`); }
        setMessage("");
        key.current = undefined;
      }
    } catch (error) {
      if (error instanceof ActionRefusalError) {
        setFeedback(error.message);
        await onReadback().catch(() => undefined);
      } else {
        setFeedback(`Result uncertain. Retry uses the same message key. ${error instanceof Error ? error.message : String(error)}`);
      }
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };
  return (
    <section className="mb-6 rounded-[10px] border border-line bg-raised p-4" data-run-say>
      <h2 className="text-[12px] font-semibold text-ink">{mode === "continue" ? "Continue this run" : "Say to this run"}</h2>
      <p className="mt-1 text-[11px] text-faint">{canSay ? run.sayRoute?.note : canContinue ? "Continue the same conversation with an optional message." : run.sayRoute?.reason || "Messaging is unavailable in this state."}</p>
      {mode && <div className="mt-3 flex gap-2">
        <textarea aria-label="Run message" value={message} onChange={(event) => { setMessage(event.target.value); if (key.current) key.current = undefined; }} disabled={busy} maxLength={32768} rows={2} className="min-w-0 flex-1 rounded border border-line bg-canvas p-2 text-[12px] text-ink" placeholder={mode === "continue" ? "Optional message" : "Message to the agent"} />
        <button type="button" disabled={busy || (mode === "say" && !message.trim())} onClick={() => void send()} className="self-end rounded bg-ink px-3 py-2 text-[12px] text-canvas disabled:opacity-50">{mode === "say" ? "Say" : "Continue"}</button>
      </div>}
      {feedback && <p role="status" className="mt-2 text-[11px] text-ink-soft">{feedback}</p>}
      {run.lastSayDelivery && <p className="mt-2 text-[11px] text-faint">Last Say: {run.lastSayDelivery.state} · {run.lastSayDelivery.body}</p>}
    </section>
  );
}
