import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

/** Uppercase mono section label (design: letter-spaced faint caps). */
export function SectionLabel({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        "font-mono text-[9.5px] font-medium uppercase tracking-[0.16em] text-fainter",
        className,
      )}
    >
      {children}
    </div>
  );
}

/** Page header: serif title, optional subtitle + right-aligned actions. */
export function PageHeader({
  title,
  subtitle,
  actions,
  eyebrow,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  eyebrow?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3 pb-5">
      <div className="min-w-0 flex-1 basis-60">
        {eyebrow && <div className="mb-1.5">{eyebrow}</div>}
        <h1 className="font-serif text-[22px] font-semibold leading-tight tracking-[-0.01em] text-ink">
          {title}
        </h1>
        {subtitle && <p className="mt-1 text-[13.5px] text-muted">{subtitle}</p>}
      </div>
      {actions && <div className="flex flex-none flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

/** Scrollable page body with the thin control-room scrollbar. */
export function PageScroll({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("tk-scroll h-full overflow-y-auto", className)}>
      <div className="mx-auto w-full max-w-[1180px] px-4 pt-5 pb-[max(1.25rem,env(safe-area-inset-bottom))] sm:px-6 lg:px-8 lg:pt-7 lg:pb-7">
        {children}
      </div>
    </div>
  );
}

/** A horizontal toolbar (filters, view toggles). Wraps on narrow screens. */
export function Toolbar({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("flex flex-wrap items-center gap-2 border-b border-line pb-3", className)}>
      {children}
    </div>
  );
}
