import type { Attempt } from "@/types/domain";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { OutcomeChip } from "@/components/ui/chips";
import { outcomeTone, tone } from "@/components/ui/tone";
import { relativeTime } from "@/lib/time";
import { attemptMeta } from "@/features/runs/detail/helpers";

/**
 * Vertical attempts timeline (design §07). Latest attempt on top; the node dot
 * carries the attempt outcome's tone, the thread connects to older attempts.
 */
export function AttemptTimeline({ attempts }: { attempts: Attempt[] }) {
  if (attempts.length === 0) {
    return <div className="text-[12.5px] text-muted">No attempts recorded yet.</div>;
  }
  const ordered = [...attempts].reverse();
  return (
    <ol className="flex flex-col">
      {ordered.map((a, i) => {
        const isLast = i === ordered.length - 1;
        const t = tone[outcomeTone[a.outcome]];
        return (
          <li key={a.n} className="flex gap-3 pb-[18px] last:pb-0">
            <div className="flex flex-none flex-col items-center">
              <span
                className={cn(
                  "mt-1 h-[11px] w-[11px] flex-none rounded-full ring-2 ring-surface",
                  t.dot,
                )}
              />
              {!isLast && <span className="mt-[3px] w-px flex-1 bg-line" />}
            </div>
            <div className="-mt-0.5 min-w-0 pb-1">
              <div className="flex flex-wrap items-center gap-2 text-[13px] font-semibold text-ink">
                Attempt {a.n}
                <OutcomeChip outcome={a.outcome} />
              </div>
              {/* TODO(api): per-attempt summary/note — Attempt carries no prose today. */}
              <Mono className="mt-1.5 block text-[10.5px] text-faint">{attemptMeta(a)}</Mono>
              <div className="mt-1 text-[12px] text-muted">started {relativeTime(a.startedAt)}</div>
            </div>
          </li>
        );
      })}
    </ol>
  );
}
