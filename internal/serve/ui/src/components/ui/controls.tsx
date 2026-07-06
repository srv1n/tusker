import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { cn } from "@/lib/cn";

type ButtonVariant = "default" | "primary" | "ghost" | "danger" | "subtle";
type Size = "sm" | "md";

const buttonBase =
  "inline-flex items-center justify-center gap-1.5 rounded-lg font-medium leading-none transition-colors disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40";

const buttonVariants: Record<ButtonVariant, string> = {
  default: "border border-line text-ink-soft hover:bg-hover",
  primary: "bg-ink text-surface hover:opacity-90",
  ghost: "text-muted hover:bg-hover hover:text-ink-soft",
  danger: "border border-fail/30 text-fail hover:bg-fail-soft",
  subtle: "bg-hover text-ink-soft hover:bg-active",
};

const buttonSizes: Record<Size, string> = {
  sm: "h-7 px-2.5 text-[12px]",
  md: "h-8.5 px-3 text-[13px]",
};

export function Button({
  variant = "default",
  size = "md",
  className,
  children,
  ...rest
}: ComponentPropsWithoutRef<"button"> & { variant?: ButtonVariant; size?: Size }) {
  return (
    <button className={cn(buttonBase, buttonVariants[variant], buttonSizes[size], className)} {...rest}>
      {children}
    </button>
  );
}

export function IconButton({
  className,
  children,
  active = false,
  ...rest
}: ComponentPropsWithoutRef<"button"> & { active?: boolean }) {
  return (
    <button
      className={cn(
        "inline-flex h-8 w-8 items-center justify-center rounded-lg text-muted transition-colors hover:bg-hover hover:text-ink-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40",
        active && "bg-hover text-ink",
        className,
      )}
      {...rest}
    >
      {children}
    </button>
  );
}

export interface SegmentOption<T extends string> {
  value: T;
  label: ReactNode;
}

/** Segmented control — view toggles (board/table), theme picker, etc. */
export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  size = "md",
  className,
}: {
  options: SegmentOption<T>[];
  value: T;
  onChange: (value: T) => void;
  size?: Size;
  className?: string;
}) {
  return (
    <div className={cn("inline-flex items-center gap-0.5 rounded-lg border border-line bg-panel p-0.5", className)}>
      {options.map((o) => (
        <button
          key={o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            "rounded-md font-medium transition-colors",
            size === "sm" ? "px-2 py-1 text-[11.5px]" : "px-2.5 py-1.5 text-[12.5px]",
            o.value === value ? "bg-raised text-ink shadow-sm" : "text-muted hover:text-ink-soft",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** On/off switch. */
export function Toggle({
  checked,
  onChange,
  label,
  disabled = false,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label?: ReactNode;
  disabled?: boolean;
}) {
  return (
    <label className={cn("inline-flex items-center gap-2.5", disabled && "opacity-50")}>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={cn(
          "relative h-[18px] w-[30px] flex-none rounded-full transition-colors",
          checked ? "bg-ink" : "bg-line",
        )}
      >
        <span
          className={cn(
            "absolute top-[2px] h-[14px] w-[14px] rounded-full bg-surface transition-transform",
            checked ? "translate-x-[14px]" : "translate-x-[2px]",
          )}
        />
      </button>
      {label && <span className="text-[13px] text-ink-soft">{label}</span>}
    </label>
  );
}

export function TextInput({ className, ...rest }: ComponentPropsWithoutRef<"input">) {
  return (
    <input
      className={cn(
        "h-8.5 rounded-lg border border-line bg-surface px-3 text-[13px] text-ink placeholder:text-faint focus-visible:border-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/20",
        className,
      )}
      {...rest}
    />
  );
}

export function Select({ className, children, ...rest }: ComponentPropsWithoutRef<"select">) {
  return (
    <select
      className={cn(
        "h-8.5 rounded-lg border border-line bg-surface px-2.5 text-[13px] text-ink-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/20",
        className,
      )}
      {...rest}
    >
      {children}
    </select>
  );
}
