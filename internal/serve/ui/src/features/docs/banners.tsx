import { Check, Lock, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import type {
  ConflictDiff,
  DiffSpan,
  MergeCheck,
  ReconcileChoice,
  ValidationIssue,
} from "./types";

// Local semantic button styles — the shared Button lacks info/warn/pass hues.
const solid: Record<"info" | "warn" | "fail" | "pass", string> = {
  info: "bg-info text-surface hover:opacity-90",
  warn: "bg-warn text-surface hover:opacity-90",
  fail: "bg-fail text-surface hover:opacity-90",
  pass: "bg-pass text-surface hover:opacity-90",
};
const outline: Record<"info" | "warn" | "fail", string> = {
  info: "border border-info/40 text-info hover:bg-info-soft",
  warn: "border border-warn/40 text-warn hover:bg-warn-soft",
  fail: "border border-fail/40 text-fail hover:bg-fail-soft",
};
const btn = "rounded-lg px-3.5 py-2 text-[12.5px] font-semibold leading-none transition-colors";

/** Approve-spec banner (packet §5, moment 5). */
export function ApproveBanner({
  blocked,
  onApprove,
  onRequestChanges,
}: {
  blocked: number;
  onApprove: () => void;
  onRequestChanges: () => void;
}) {
  return (
    <div className="mb-6 flex animate-rise flex-wrap items-center gap-3.5 rounded-xl border border-info/30 bg-info-soft px-4 py-3.5">
      <span className="h-2 w-2 flex-none rounded-full bg-info" />
      <span className="min-w-0 flex-1 text-[13.5px] text-ink-soft">
        This spec is awaiting your approval.{" "}
        <span className="font-medium text-ink">{blocked} downstream tasks are blocked.</span>
      </span>
      <button className={cn(btn, outline.info, "font-medium")} onClick={onRequestChanges}>
        Request changes
      </button>
      <button className={cn(btn, solid.info)} onClick={onApprove}>
        Approve spec
      </button>
    </div>
  );
}

function DiffColumn({ title, spans }: { title: string; spans: DiffSpan[] }) {
  return (
    <div className="overflow-hidden rounded-lg border border-warn/25 bg-raised">
      <div className="border-b border-warn/15 bg-warn-soft px-3 py-1.5 font-mono text-[10px] uppercase tracking-[0.06em] text-warn">
        {title}
      </div>
      <div className="px-3 py-2.5 font-mono text-[11.5px] leading-[1.7] text-ink-soft">
        {spans.map((s, i) => (
          <span
            key={i}
            className={cn(
              s.mark === "add" && "rounded-[3px] bg-pass-soft px-0.5 text-pass",
              s.mark === "del" && "rounded-[3px] bg-fail-soft px-0.5 text-fail",
            )}
          >
            {s.text}
          </span>
        ))}
      </div>
    </div>
  );
}

/** CAS conflict banner + side-by-side diff (packet §4.6 — the critical moment). */
export function ConflictBanner({
  conflict,
  onReconcile,
}: {
  conflict: ConflictDiff;
  onReconcile: (choice: ReconcileChoice) => void;
}) {
  return (
    <div className="mb-6 animate-rise rounded-xl border border-warn/30 bg-warn-soft px-4.5 py-4">
      <div className="mb-1.5 flex items-center gap-2.5">
        <span className="h-2 w-2 flex-none rounded-full bg-warn" />
        <span className="text-[14px] font-semibold text-ink">This note changed while you were editing</span>
      </div>
      <p className="mb-3 text-[13px] leading-[1.5] text-muted">
        Agent <Mono className="font-semibold text-ink-soft">{conflict.agent}</Mono> saved a new
        revision {conflict.agoLabel} (state_rev {conflict.fromRev} → {conflict.toRev}). Your save was
        rejected — nothing is lost.
      </p>
      <div className="mb-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <DiffColumn title="Yours" spans={conflict.yours} />
        <DiffColumn title={`Theirs · rev ${conflict.toRev}`} spans={conflict.theirs} />
      </div>
      <div className="flex flex-wrap gap-2.5">
        <button className={cn(btn, solid.warn)} onClick={() => onReconcile("take-theirs")}>
          Take theirs
        </button>
        <button className={cn(btn, outline.warn, "font-medium")} onClick={() => onReconcile("keep-mine")}>
          Keep editing on top
        </button>
        <button className={cn(btn, outline.warn, "font-medium")} onClick={() => onReconcile("copy-mine")}>
          Copy my text
        </button>
      </div>
    </div>
  );
}

/** Inline validation summary strip after a rejected save (packet §4.6). */
export function ValidationStrip({
  issues,
  onFix,
  onDiscard,
}: {
  issues: ValidationIssue[];
  onFix: () => void;
  onDiscard: () => void;
}) {
  const errors = issues.filter((i) => i.severity === "error").length;
  const warns = issues.filter((i) => i.severity === "warn").length;
  const parts = [
    errors > 0 && `${errors} error${errors > 1 ? "s" : ""}`,
    warns > 0 && `${warns} warning${warns > 1 ? "s" : ""}`,
  ].filter(Boolean);

  return (
    <div className="mb-6 animate-rise rounded-xl border border-fail/30 bg-fail-soft px-4 py-3.5">
      <div className="mb-2 flex items-center gap-2.5">
        <span className="h-2 w-2 flex-none rounded-full bg-fail" />
        <span className="text-[14px] font-semibold text-ink">Save rejected · {parts.join(", ")}</span>
      </div>
      <div className="flex flex-col gap-1.5 text-[12.5px]">
        {issues.map((issue, i) => (
          <div key={i} className="flex gap-2.5">
            <Mono
              className={cn(
                "flex-none font-semibold",
                issue.severity === "error" ? "text-fail" : "text-warn",
              )}
            >
              {issue.severity === "error" ? "error" : "warn "}
            </Mono>
            <span className="text-ink-soft">{issue.message}</span>
          </div>
        ))}
      </div>
      {errors > 0 && (
        <div className="mt-3 flex flex-wrap gap-2.5">
          <button className={cn(btn, outline.fail)} onClick={onFix}>
            Fix errors
          </button>
          <button className={cn(btn, "border border-fail/25 text-muted hover:bg-fail-soft font-medium")} onClick={onDiscard}>
            Discard changes
          </button>
        </div>
      )}
    </div>
  );
}

/** Success confirmation after a validated save. */
export function SavedBanner({ rev }: { rev: string }) {
  return (
    <div className="mb-6 flex animate-rise items-center gap-2.5 rounded-xl border border-pass/30 bg-pass-soft px-4 py-3">
      <span className="h-2 w-2 flex-none rounded-full bg-pass" />
      <span className="text-[13.5px] text-ink-soft">
        Saved · validated · state_rev {rev}. Round-tripped to markdown.
      </span>
    </div>
  );
}

const markStyle: Record<MergeCheck["state"], { bg: string; mark: "check" | "x" | "dot" }> = {
  pass: { bg: "bg-pass", mark: "check" },
  fail: { bg: "bg-fail", mark: "x" },
  pending: { bg: "bg-warn", mark: "dot" },
};

/** Merge-readiness / closeout checklist (Conductor pattern — packet §9). */
export function MergeReadiness({
  checks,
  onAccept,
}: {
  checks: MergeCheck[];
  onAccept: () => void;
}) {
  const green = checks.filter((c) => c.state === "pass").length;
  const allGreen = checks.every((c) => c.state === "pass");
  const blockers = checks.length - green;

  return (
    <div className="mb-7 overflow-hidden rounded-xl border border-pass/30">
      <div className="flex items-center gap-2.5 border-b border-pass/20 bg-pass-soft px-4 py-3">
        <span className="h-2 w-2 flex-none rounded-full bg-pass" />
        <span className="text-[13.5px] font-semibold text-ink">Merge-readiness</span>
        <Mono className="ml-auto text-[11px] text-pass">
          {green} / {checks.length} green
        </Mono>
      </div>
      {checks.map((c) => {
        const m = markStyle[c.state];
        return (
          <div key={c.id} className="flex items-center gap-3 border-b border-line-soft px-4 py-2.5">
            <span
              className={cn(
                "flex h-[15px] w-[15px] flex-none items-center justify-center rounded-full text-surface",
                m.bg,
              )}
            >
              {m.mark === "check" ? (
                <Check size={9} strokeWidth={3} />
              ) : m.mark === "x" ? (
                <X size={9} strokeWidth={3} />
              ) : (
                <span className="h-[3px] w-[6px] rounded-full bg-surface" />
              )}
            </span>
            <span className="w-[116px] flex-none text-[13px] text-ink-soft">{c.label}</span>
            <Mono className="min-w-0 flex-1 truncate text-[11px] text-faint">{c.detail}</Mono>
          </div>
        );
      })}
      <div className="flex items-center gap-3 px-4 py-3">
        <button
          disabled={!allGreen}
          onClick={onAccept}
          className={cn(
            "rounded-lg px-4 py-2 text-[12.5px] font-semibold leading-none text-surface transition-colors",
            solid.pass,
            "disabled:cursor-not-allowed disabled:opacity-45",
          )}
        >
          Accept &amp; close
        </button>
        <span className="flex items-center gap-1.5 font-mono text-[11px] text-faint">
          {!allGreen && <Lock size={10} strokeWidth={2} />}
          {allGreen ? "all checks green" : `${blockers} check${blockers > 1 ? "s" : ""} blocking close`}
        </span>
      </div>
    </div>
  );
}
