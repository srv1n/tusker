import { Check, Lock, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import type { MergeCheck } from "./types";

const markStyle: Record<MergeCheck["state"], { bg: string; mark: "check" | "x" | "dot" }> = {
  pass: { bg: "bg-pass", mark: "check" },
  fail: { bg: "bg-fail", mark: "x" },
  pending: { bg: "bg-warn", mark: "dot" },
};

/** Merge-readiness checklist. Lifecycle actions remain durable API operations. */
export function MergeReadiness({ checks, onAccept }: { checks: MergeCheck[]; onAccept: () => void }) {
  const green = checks.filter((c) => c.state === "pass").length;
  const allGreen = checks.every((c) => c.state === "pass");
  const blockers = checks.length - green;
  return (
    <div className="mb-7 overflow-hidden rounded-xl border border-pass/30">
      <div className="flex items-center gap-2.5 border-b border-pass/20 bg-pass-soft px-4 py-3"><span className="h-2 w-2 flex-none rounded-full bg-pass" /><span className="text-[13.5px] font-semibold text-ink">Merge-readiness</span><Mono className="ml-auto text-[11px] text-pass">{green} / {checks.length} green</Mono></div>
      {checks.map((c) => { const m = markStyle[c.state]; return <div key={c.id} className="flex items-center gap-3 border-b border-line-soft px-4 py-2.5"><span className={cn("flex h-[15px] w-[15px] flex-none items-center justify-center rounded-full text-surface", m.bg)}>{m.mark === "check" ? <Check size={9} strokeWidth={3} /> : m.mark === "x" ? <X size={9} strokeWidth={3} /> : <span className="h-[3px] w-[6px] rounded-full bg-surface" />}</span><span className="w-[116px] flex-none text-[13px] text-ink-soft">{c.label}</span><Mono className="min-w-0 flex-1 truncate text-[11px] text-faint">{c.detail}</Mono></div>; })}
      <div className="flex items-center gap-3 px-4 py-3"><button disabled={!allGreen} onClick={onAccept} className={cn("rounded-lg px-4 py-2 text-[12.5px] font-semibold leading-none text-surface transition-colors", "bg-pass hover:opacity-90", "disabled:cursor-not-allowed disabled:opacity-45")}>Accept &amp; close</button><span className="flex items-center gap-1.5 font-mono text-[11px] text-faint">{!allGreen && <Lock size={10} strokeWidth={2} />}{allGreen ? "all checks green" : `${blockers} check${blockers > 1 ? "s" : ""} blocking close`}</span></div>
    </div>
  );
}
