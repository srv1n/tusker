/*
  Screen-local primitives for Project Work. These don't belong in the shared
  library (they're specific to this browser) so they live here per the ownership
  rule.
*/

import type { ReactNode } from "react";
import { ChevronDown, ChevronsUpDown, ChevronUp, Flag } from "lucide-react";
import { cn } from "@/lib/cn";

/**
 * Human-gate marker (packet §4.4: "show a gate marker when task.hasGate").
 * Icon-only in dense rows/cards; `label` adds the word for the wider contexts.
 * Warn tone matches the gate hue used across the app.
 */
export function GateMarker({ label = false }: { label?: boolean }) {
  return (
    <span
      title="Waiting on a human gate"
      className="inline-flex items-center gap-1 text-warn"
    >
      <Flag size={11} strokeWidth={2.25} fill="currentColor" fillOpacity={0.15} />
      {label && (
        <span className="font-mono text-[9px] font-semibold uppercase tracking-[0.08em]">gate</span>
      )}
    </span>
  );
}

/** Rounded filter pill (design: epic pills). Filled when active. */
export function FilterPill({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "rounded-full border px-3 py-1 text-[12px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30",
        active
          ? "border-transparent bg-ink text-surface"
          : "border-line text-ink-soft hover:bg-hover",
      )}
    >
      {children}
    </button>
  );
}

/** Column-header sort direction glyph for the table. */
export function SortIcon({ dir }: { dir: false | "asc" | "desc" }) {
  if (dir === "asc") return <ChevronUp size={12} strokeWidth={2.25} />;
  if (dir === "desc") return <ChevronDown size={12} strokeWidth={2.25} />;
  return <ChevronsUpDown size={12} strokeWidth={2} className="opacity-40" />;
}
