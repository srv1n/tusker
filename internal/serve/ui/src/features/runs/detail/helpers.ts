/*
  Run-detail local helpers. Pure derivations over the shared domain types — no
  new data invented here. Where the API must supply a field the run detail lacks
  today, it is marked `// TODO(api)` at the call site.
*/

import type { Attempt, RunDetail, RunEvent } from "@/types/domain";
import { compactNumber, duration } from "@/lib/time";

/** A derived stat cell for the run summary grid (design §07 — four headline numbers). */
export interface RunStat {
  label: string;
  value: string;
}

/** The four headline numbers shown above the run body. */
export function runStats(run: RunDetail): RunStat[] {
  return [
    { label: "Elapsed", value: duration(run.elapsedSec) },
    { label: "Input", value: compactNumber(run.tokens.input) },
    { label: "Output", value: compactNumber(run.tokens.output) },
    { label: "Attempts", value: String(run.attemptCount) },
  ];
}

/** Compact "1m 04s · 21.1k→1.5k tok" attempt meta line. */
export function attemptMeta(a: Attempt): string {
  return `${duration(a.durationSec)} · ${compactNumber(a.tokens.input)}→${compactNumber(
    a.tokens.output,
  )} tok`;
}

/**
 * Level → text tone classes for the event console. The design colors each line
 * by severity; normal protocol events read calm, warn/error pop (packet §4.3).
 */
export function eventToneClasses(level: RunEvent["level"]): { kind: string; text: string } {
  switch (level) {
    case "error":
      return { kind: "text-fail font-semibold", text: "text-fail" };
    case "warn":
      return { kind: "text-warn font-semibold", text: "text-warn" };
    default:
      return { kind: "text-info", text: "text-ink-soft" };
  }
}

/** ISO → "18:22:05" wall clock (UTC, to stay consistent with the frozen mock timeline). */
export function clockTime(iso: string): string {
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getUTCHours())}:${p(d.getUTCMinutes())}:${p(d.getUTCSeconds())}`;
}
