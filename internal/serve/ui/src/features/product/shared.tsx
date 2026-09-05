import type { ReactNode } from "react";
import { AlertTriangle, ArrowRight, Check, CircleDashed } from "lucide-react";
import { cn } from "@/lib/cn";

export function ProductPage({
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
          "mx-auto w-full px-5 pb-20 pt-8 sm:px-8 lg:px-12",
          wide ? "max-w-[1440px]" : "max-w-[1240px]",
        )}
      >
        <header className="mb-8 border-b border-line pb-6">
          <div className="flex flex-wrap items-end justify-between gap-6">
            <div className="min-w-0 flex-1">
              {eyebrow && <ProductLabel className="mb-2">{eyebrow}</ProductLabel>}
              <h1 className="text-[28px] font-semibold leading-tight tracking-[-0.03em] text-ink sm:text-[34px]">
                {title}
              </h1>
              {intro && <p className="mt-2.5 max-w-[760px] text-[14.5px] leading-relaxed text-muted">{intro}</p>}
            </div>
            {actions && <div className="flex flex-none items-center gap-2 pb-0.5">{actions}</div>}
          </div>
        </header>
        {children}
      </div>
    </div>
  );
}

export function ProductLabel({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("font-mono text-[10.5px] font-semibold uppercase tracking-[0.14em] text-faint", className)}>
      {children}
    </div>
  );
}

export function ProductSection({
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
    <section className={cn("mb-10", className)}>
      <div className="mb-3.5 flex min-h-7 items-end gap-3 border-b border-line pb-2">
        <h2 className="text-[16px] font-semibold tracking-[-0.015em] text-ink">{title}</h2>
        {count !== undefined && <span className="pb-0.5 font-mono text-[11px] text-faint">{count}</span>}
        {action && <div className="ml-auto">{action}</div>}
      </div>
      {children}
    </section>
  );
}

export function ProductStatus({
  tone = "neutral",
  children,
}: {
  tone?: "neutral" | "info" | "pass" | "warn" | "fail";
  children: ReactNode;
}) {
  const styles = {
    neutral: "border-line bg-panel/60 text-muted",
    info: "border-info/30 bg-info-soft text-info",
    pass: "border-pass/30 bg-pass-soft text-pass",
    warn: "border-warn/30 bg-warn-soft text-warn",
    fail: "border-fail/30 bg-fail-soft text-fail",
  };
  return (
    <span className={cn("inline-flex items-center rounded-full border px-2.5 py-0.5 text-[11px] font-medium leading-normal whitespace-nowrap", styles[tone])}>
      {children}
    </span>
  );
}

export function ProductButton({
  children,
  tone = "secondary",
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  tone?: "primary" | "secondary" | "danger" | "text";
}) {
  const styles = {
    primary: "border-transparent bg-ink text-surface hover:opacity-90 shadow-2xs",
    secondary: "border-line bg-raised text-ink hover:border-ink/40 hover:bg-hover shadow-2xs",
    danger: "border-transparent bg-fail text-white hover:opacity-90 shadow-2xs",
    text: "border-transparent bg-transparent text-ink hover:bg-hover",
  };
  return (
    <button
      type="button"
      {...props}
      className={cn(
        "inline-flex min-h-8.5 items-center justify-center gap-2 rounded-lg border px-3 text-[12.5px] font-medium transition-all disabled:cursor-not-allowed disabled:opacity-45 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30 active:scale-[0.99]",
        styles[tone],
        props.className,
      )}
    >
      {children}
    </button>
  );
}

export function ProductRow({
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
        "grid min-h-[72px] grid-cols-1 gap-3 rounded-lg border-b border-line-soft px-3 py-3.5 transition-colors sm:grid-cols-[minmax(0,1fr)_minmax(180px,0.55fr)_auto] sm:items-center sm:gap-6",
        onClick && "cursor-pointer hover:bg-hover/70",
      )}
      onClick={onClick}
    >
      <div className="min-w-0">
        {meta && <div className="mb-1 font-mono text-[10px] text-faint">{meta}</div>}
        <div className="text-[13.5px] font-semibold leading-5 text-ink">{title}</div>
        {detail && <div className="mt-1 text-[12px] leading-5 text-muted">{detail}</div>}
      </div>
      <div className="min-w-0">{status}</div>
      <div className="flex items-center justify-end gap-2">{action ?? (onClick ? <ArrowRight size={15} /> : null)}</div>
    </div>
  );
}

export function ProductEmpty({ title, detail }: { title: string; detail: string }) {
  return (
    <div className="my-4 rounded-xl border border-dashed border-line bg-panel/30 py-12 px-6 text-center">
      <Check className="mx-auto mb-3 text-pass" size={20} />
      <div className="text-[14.5px] font-semibold text-ink">{title}</div>
      <p className="mx-auto mt-1 max-w-md text-[13px] leading-5 text-muted">{detail}</p>
    </div>
  );
}

export function ProductLoading({ rows = 3 }: { rows?: number }) {
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

export function ProductUnavailable({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-start gap-3 rounded-lg border border-warn/30 bg-warn-soft px-4 py-3 text-[12.5px] leading-relaxed text-warn">
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

export function ProductPhaseStrip({ current }: { current: string }) {
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
    <div className="grid grid-cols-5 overflow-hidden rounded-lg border border-line">
      {phases.map((phase, index) => (
        <div
          key={phase}
          className={cn(
            "flex items-center gap-2 border-r border-line px-3 py-2.5 text-[11.5px] last:border-r-0",
            index < currentIndex && "bg-pass-soft text-pass font-medium",
            index === currentIndex && "bg-info-soft font-semibold text-info",
            index > currentIndex && "bg-surface text-faint",
          )}
        >
          {index < currentIndex ? <Check size={13} /> : <CircleDashed size={13} />}
          {phase}
        </div>
      ))}
    </div>
  );
}
