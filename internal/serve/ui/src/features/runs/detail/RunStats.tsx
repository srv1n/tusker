import type { RunDetail } from "@/types/domain";
import { runStats } from "@/features/runs/detail/helpers";

/** Four-cell headline stat grid — hairline-separated serif numbers (design §07). */
export function RunStats({ run, waitingForDaemon = false }: { run: RunDetail; waitingForDaemon?: boolean }) {
  const stats = runStats(run, waitingForDaemon);
  return (
    <div className="mb-6 grid grid-cols-2 gap-px overflow-hidden rounded-[10px] border border-line bg-line sm:grid-cols-4">
      {stats.map((s) => (
        <div key={s.label} className="bg-raised px-4 py-3.5">
          <div className="font-mono text-[9px] uppercase tracking-[0.1em] text-faint">{s.label}</div>
          <div className="mt-1 font-serif text-[22px] font-semibold tracking-[-0.01em] text-ink tabular">
            {s.value}
          </div>
        </div>
      ))}
    </div>
  );
}
