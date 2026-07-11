/*
  Screen-local building blocks for App Settings. These are NOT general enough to
  live in the shared library (they encode settings-row provenance + the design's
  compact mono controls), so they stay in the feature folder per the contract.
*/

import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { ChevronDown, Lock } from "lucide-react";
import { cn } from "@/lib/cn";
import { Chip } from "@/components/ui/primitives";
import { sourceTone, type Harness, type SettingSource } from "./mock";

/** Provenance chip — the small mono source tag on every settings row (addendum §1.3). */
export function SourceChip({ source, className }: { source: SettingSource; className?: string }) {
  return (
    <Chip
      tone={sourceTone[source]}
      variant="soft"
      mono
      className={cn("rounded px-[7px] py-[2px] text-[9px] font-semibold tracking-normal", className)}
    >
      {source}
    </Chip>
  );
}

/** Quiet "reset to inherited" — only shown on overridden rows. */
export function ResetToInherited({ onReset }: { onReset: () => void }) {
  return (
    <button
      type="button"
      onClick={onReset}
      className="text-[11px] text-faint transition-colors hover:text-info"
    >
      reset to inherited
    </button>
  );
}

/** Harness identity — codex reads as a solid dark tag, claude-code as a warm soft tag. */
export function HarnessChip({ harness }: { harness: Harness }) {
  const isCodex = harness === "codex";
  return (
    <span
      className={cn(
        "inline-flex items-center rounded px-[7px] py-[2px] font-mono text-[9.5px] font-semibold leading-none",
        isCodex ? "bg-ink text-surface" : "bg-warn-soft text-warn",
      )}
    >
      {harness}
    </span>
  );
}

/**
 * Compact mono dropdown matching the design's inline "value ▾" pill. The shared
 * `Select` is a full-height, non-mono control that would tower over these rows;
 * this wraps a native <select> so it stays a real, keyboard-accessible control.
 */
export function SelectPill({
  value,
  options,
  onChange,
  ariaLabel,
}: {
  value: string;
  options: string[];
  onChange: (value: string) => void;
  ariaLabel: string;
}) {
  return (
    <div className="relative inline-flex items-center">
      <select
        aria-label={ariaLabel}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="cursor-pointer appearance-none rounded-md border border-line bg-surface py-[3px] pl-[9px] pr-[24px] font-mono text-[11.5px] text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/20"
      >
        {options.map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>
      <ChevronDown size={12} strokeWidth={2} className="pointer-events-none absolute right-[7px] text-faint" />
    </div>
  );
}

/** A bordered, rounded settings group with hairline dividers between rows. */
export function SettingsCard({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("divide-y divide-line-soft overflow-hidden rounded-[10px] border border-line", className)}>
      {children}
    </div>
  );
}

/** One settings row: label (+ optional description / lock) · source chip · control. */
export function SettingRow({
  label,
  description,
  source,
  overridden,
  onReset,
  locked,
  control,
}: {
  label: ReactNode;
  description?: ReactNode;
  source?: SettingSource;
  overridden?: boolean;
  onReset?: () => void;
  locked?: boolean;
  control?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2 px-4 py-[13px] sm:flex-row sm:items-center sm:justify-between sm:gap-3">
      <div className="flex min-w-0 items-start gap-2">
        {locked && (
          <Lock
            size={11}
            strokeWidth={2}
            aria-label="read-only"
            className="mt-[3px] flex-none text-fainter"
          />
        )}
        <div className="flex min-w-0 flex-col gap-[2px]">
          <span className="text-[13.5px] text-ink-soft">{label}</span>
          {description && <span className="text-[11.5px] leading-snug text-faint">{description}</span>}
        </div>
      </div>
      <div className="flex flex-none items-center gap-[9px]">
        {overridden && onReset && <ResetToInherited onReset={onReset} />}
        {source && <SourceChip source={source} />}
        {control}
      </div>
    </div>
  );
}

/** Dashed "add more" button (new profile). */
export function DashedButton({ className, children, ...rest }: ComponentPropsWithoutRef<"button">) {
  return (
    <button
      className={cn(
        "rounded-lg border border-dashed border-line bg-surface px-[14px] py-2 text-[12.5px] text-muted transition-colors hover:border-fainter hover:text-ink-soft",
        className,
      )}
      {...rest}
    >
      {children}
    </button>
  );
}
