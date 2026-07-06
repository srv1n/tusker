import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { cn } from "@/lib/cn";
import { tone, type Tone } from "@/components/ui/tone";

/** Monospace inline text — ids, commands, counts, addresses. */
export function Mono({ className, children, ...rest }: ComponentPropsWithoutRef<"span">) {
  return (
    <span className={cn("font-mono tabular", className)} {...rest}>
      {children}
    </span>
  );
}

/** Small status dot; `pulse` animates it for live/running states. */
export function Dot({
  tone: t = "neutral",
  pulse = false,
  className,
  size = 6,
}: {
  tone?: Tone;
  pulse?: boolean;
  className?: string;
  size?: number;
}) {
  return (
    <span
      className={cn("inline-block rounded-full", tone[t].dot, pulse && "animate-pulse-soft", className)}
      style={{ width: size, height: size }}
    />
  );
}

export type ChipVariant = "soft" | "outline" | "solid";

/** Base pill. Chips carry meaning by tone; keep them quiet by default. */
export function Chip({
  tone: t = "neutral",
  variant = "soft",
  mono = false,
  className,
  children,
}: {
  tone?: Tone;
  variant?: ChipVariant;
  mono?: boolean;
  className?: string;
  children: ReactNode;
}) {
  const tc = tone[t];
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11.5px] font-medium leading-none whitespace-nowrap",
        mono && "font-mono tabular text-[10.5px] tracking-tight",
        variant === "soft" && cn(tc.softBg, tc.softText),
        variant === "outline" && cn("border border-line", tc.text),
        variant === "solid" && "text-surface",
        className,
      )}
      style={variant === "solid" ? { backgroundColor: `var(--k-${t === "neutral" ? "faint" : t})` } : undefined}
    >
      {children}
    </span>
  );
}

/** Compact rounded count, e.g. the needs-me badge. */
export function CountBadge({
  count,
  tone: t = "fail",
  className,
}: {
  count: number;
  tone?: Tone;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex min-w-[19px] items-center justify-center rounded-full px-[5px] text-[11px] font-semibold text-surface font-mono tabular",
        className,
      )}
      style={{ height: 19, backgroundColor: `var(--k-${t === "neutral" ? "faint" : t})` }}
    >
      {count}
    </span>
  );
}

/** Keyboard hint. */
export function Kbd({ children }: { children: ReactNode }) {
  return (
    <kbd className="rounded border border-line bg-panel px-1.5 py-0.5 font-mono text-[10.5px] text-muted leading-none">
      {children}
    </kbd>
  );
}

/** Surface card with a hairline border. */
export function Card({
  className,
  children,
  interactive = false,
  ...rest
}: ComponentPropsWithoutRef<"div"> & { interactive?: boolean }) {
  return (
    <div
      className={cn(
        "rounded-xl border border-line bg-raised",
        interactive && "transition-colors hover:border-line hover:bg-hover cursor-pointer",
        className,
      )}
      {...rest}
    >
      {children}
    </div>
  );
}
