/*
  Time formatting.
*/

function now(): Date {
  return new Date();
}

/** "3m ago", "2h ago", "just now". */
export function relativeTime(iso: string, from: Date = now()): string {
  const diffMs = from.getTime() - new Date(iso).getTime();
  const s = Math.max(0, Math.round(diffMs / 1000));
  if (s < 10) return "just now";
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.round(h / 24);
  return `${d}d ago`;
}

/** Seconds → "8m 32s" / "1h 04m" / "47s". */
export function duration(sec: number): string {
  if (sec < 60) return `${sec}s`;
  const m = Math.floor(sec / 60);
  const rem = sec % 60;
  if (m < 60) return `${m}m ${String(rem).padStart(2, "0")}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${String(m % 60).padStart(2, "0")}m`;
}

/** Seconds since last event → compact "7s" / "1m 36s". */
export function sinceLabel(sec: number): string {
  return duration(sec);
}

/** 184320 → "184.3k", 990 → "990", 1_240_000 → "1.24M". */
export function compactNumber(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`;
  return `${(n / 1_000_000).toFixed(2)}M`;
}
