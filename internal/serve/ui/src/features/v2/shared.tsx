import type { ReactNode } from "react";
import { AlertTriangle, ArrowRight, Check, CircleDashed } from "lucide-react";
import { cn } from "@/lib/cn";

export function V2Page({
  title,
  intro,
  eyebrow,
  actions,
  children,
  wide = false,
}: {
  title: string;
  intro?: string;
  eyebrow?: string;
  actions?: ReactNode;
  children: ReactNode;
  wide?: boolean;
}) {
  return (
    <div className="tk-scroll h-full overflow-y-auto bg-surface">
      <div
        className={cn(
          "mx-auto w-full px-5 pb-20 pt-9 sm:px-8 lg:px-12 2xl:px-16",
          wide ? "max-w-[1500px]" : "max-w-[1320px]",
        )}
      >
        <header className="mb-10 border-b-2 border-ink pb-5">
          <div className="flex items-end justify-between gap-8">
            <div className="min-w-0">
              {eyebrow && <V2Label className="mb-3">{eyebrow}</V2Label>}
              <h1 className="text-[34px] font-semibold leading-[1.05] tracking-[-0.035em] text-ink sm:text-[40px]">
                {title}
              </h1>
              {intro && <p className="mt-3 max-w-[760px] text-[15px] leading-6 text-muted">{intro}</p>}
            </div>
            {actions && <div className="flex flex-none items-center gap-2 pb-1">{actions}</div>}
          </div>
        </header>
        {children}
      </div>
    </div>
  );
}

export function V2Label({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("font-mono text-[10px] font-semibold uppercase tracking-[0.16em] text-faint", className)}>
      {children}
    </div>
  );
}

export function V2Section({
  title,
  count,
  action,
  children,
  className,
}: {
  title: string;
  count?: number | string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("mb-12", className)}>
      <div className="mb-4 flex min-h-7 items-end gap-3 border-b border-line pb-2">
        <h2 className="text-[18px] font-semibold tracking-[-0.015em] text-ink">{title}</h2>
        {count !== undefined && <span className="pb-0.5 font-mono text-[11px] text-faint">{count}</span>}
        {action && <div className="ml-auto">{action}</div>}
      </div>
      {children}
    </section>
  );
}

export function V2Status({
  tone = "neutral",
  children,
}: {
  tone?: "neutral" | "info" | "pass" | "warn" | "fail";
  children: ReactNode;
}) {
  const styles = {
    neutral: "border-line text-muted",
    info: "border-info/35 bg-info-soft text-info",
    pass: "border-pass/35 bg-pass-soft text-pass",
    warn: "border-warn/35 bg-warn-soft text-warn",
    fail: "border-fail/35 bg-fail-soft text-fail",
  };
  return (
    <span className={cn("inline-flex items-center border px-2 py-1 text-[11px] font-medium", styles[tone])}>
      {children}
    </span>
  );
}

export function V2Button({
  children,
  tone = "secondary",
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  tone?: "primary" | "secondary" | "danger" | "text";
}) {
  const styles = {
    primary: "border-ink bg-ink text-surface hover:bg-ink-soft",
    secondary: "border-line bg-raised text-ink hover:border-ink hover:bg-hover",
    danger: "border-fail bg-fail text-white hover:opacity-90",
    text: "border-transparent bg-transparent text-ink hover:bg-hover",
  };
  return (
    <button
      type="button"
      {...props}
      className={cn(
        "inline-flex min-h-9 items-center justify-center gap-2 rounded-[3px] border px-3 text-[12px] font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-45",
        styles[tone],
        props.className,
      )}
    >
      {children}
    </button>
  );
}

export function V2Row({
  title,
  meta,
  detail,
  status,
  action,
  onClick,
}: {
  title: string;
  meta?: ReactNode;
  detail?: ReactNode;
  status?: ReactNode;
  action?: ReactNode;
  onClick?: () => void;
}) {
  return (
    <div
      className={cn(
        "grid min-h-[76px] grid-cols-1 gap-3 border-b border-line-soft py-4 sm:grid-cols-[minmax(0,1fr)_minmax(180px,0.55fr)_auto] sm:items-center sm:gap-6",
        onClick && "cursor-pointer transition-colors hover:bg-hover/70",
      )}
      onClick={onClick}
    >
      <div className="min-w-0">
        {meta && <div className="mb-1 font-mono text-[10px] text-faint">{meta}</div>}
        <div className="text-[14px] font-semibold leading-5 text-ink">{title}</div>
        {detail && <div className="mt-1 text-[12.5px] leading-5 text-muted">{detail}</div>}
      </div>
      <div className="min-w-0">{status}</div>
      <div className="flex items-center justify-end gap-2">{action ?? (onClick ? <ArrowRight size={15} /> : null)}</div>
    </div>
  );
}

export function V2Empty({ title, detail }: { title: string; detail: string }) {
  return (
    <div className="border-y border-line-soft py-12 text-center">
      <Check className="mx-auto mb-3 text-pass" size={21} />
      <div className="text-[15px] font-semibold text-ink">{title}</div>
      <p className="mx-auto mt-1 max-w-lg text-[13px] leading-5 text-muted">{detail}</p>
    </div>
  );
}

export function V2Loading({ rows = 3 }: { rows?: number }) {
  return (
    <div aria-label="Loading" className="animate-pulse">
      {Array.from({ length: rows }).map((_, index) => (
        <div key={index} className="grid min-h-[76px] grid-cols-[1fr_0.4fr] items-center gap-8 border-b border-line-soft">
          <div className="h-3 w-3/5 bg-active" />
          <div className="h-3 w-2/5 bg-active" />
        </div>
      ))}
    </div>
  );
}

export function V2Unavailable({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-start gap-3 border border-warn/35 bg-warn-soft px-4 py-3 text-[12.5px] leading-5 text-warn">
      <AlertTriangle className="mt-0.5 flex-none" size={15} />
      <div>{children}</div>
    </div>
  );
}

export function phaseTone(value: string): "neutral" | "info" | "pass" | "warn" | "fail" {
  const state = value.toLowerCase();
  if (state.includes("fail") || state.includes("blocked") || state.includes("exhaust")) return "fail";
  if (state.includes("wait") || state.includes("stale") || state.includes("repair") || state.includes("paused")) return "warn";
  if (state.includes("done") || state.includes("deliver") || state.includes("land") || state.includes("pass")) return "pass";
  if (state.includes("run") || state.includes("build") || state.includes("review") || state.includes("progress")) return "info";
  return "neutral";
}

export function V2PhaseStrip({ current }: { current: string }) {
  const phases = ["Planned", "Building", "Checking", "Integrating", "Delivered"];
  const normalized = current.toLowerCase();
  const currentIndex =
    normalized.includes("deliver") || normalized.includes("land")
      ? 4
      : normalized.includes("integr")
        ? 3
        : normalized.includes("review") || normalized.includes("check")
          ? 2
          : normalized.includes("run") || normalized.includes("build")
            ? 1
            : 0;
  return (
    <div className="grid grid-cols-5 border-y border-line">
      {phases.map((phase, index) => (
        <div
          key={phase}
          className={cn(
            "flex items-center gap-2 border-r border-line px-3 py-3 text-[11px] last:border-r-0",
            index < currentIndex && "bg-pass-soft text-pass",
            index === currentIndex && "bg-info-soft font-semibold text-info",
            index > currentIndex && "text-faint",
          )}
        >
          {index < currentIndex ? <Check size={13} /> : <CircleDashed size={13} />}
          {phase}
        </div>
      ))}
    </div>
  );
}
