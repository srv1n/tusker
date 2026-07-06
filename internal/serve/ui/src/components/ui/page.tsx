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
    <div className="flex items-start justify-between gap-4 pb-5">
      <div className="min-w-0">
        {eyebrow && <div className="mb-1.5">{eyebrow}</div>}
        <h1 className="font-serif text-[22px] font-semibold leading-tight tracking-[-0.01em] text-ink">
          {title}
        </h1>
        {subtitle && <p className="mt-1 text-[13.5px] text-muted">{subtitle}</p>}
      </div>
      {actions && <div className="flex flex-none items-center gap-2">{actions}</div>}
    </div>
  );
}

/** Scrollable page body with the thin control-room scrollbar. */
export function PageScroll({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("tk-scroll h-full overflow-y-auto", className)}>
      <div className="mx-auto w-full max-w-[1180px] px-8 py-7">{children}</div>
    </div>
  );
}

/** A horizontal toolbar (filters, view toggles). */
export function Toolbar({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("flex items-center gap-2 border-b border-line pb-3", className)}>
      {children}
    </div>
  );
}
