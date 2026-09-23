import type { Attempt } from "@/types/domain";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { OutcomeChip } from "@/components/ui/chips";
import { outcomeToneOf, tone } from "@/components/ui/tone";
import { relativeTime } from "@/lib/time";
import { attemptMeta } from "@/features/runs/detail/helpers";

/**
 * Vertical attempts timeline (design §07). Latest attempt on top; the node dot
 * carries the attempt outcome's tone, the thread connects to older attempts.
 */
export function AttemptTimeline({
  attempts,
  selectedAttemptId,
  onSelect,
}: {
  attempts: Attempt[];
  selectedAttemptId?: string;
  onSelect?: (attempt: Attempt) => void;
}) {
  if (attempts.length === 0) {
    return <div className="text-[12.5px] text-muted">No attempts recorded yet.</div>;
  }
  const ordered = [...attempts].reverse();
  return (
    <ol className="flex flex-col">
      {ordered.map((a, i) => {
        const isLast = i === ordered.length - 1;
        const attemptId = a.id ?? String(a.n);
        const selected = selectedAttemptId === attemptId;
        const t = tone[outcomeToneOf(a.outcome)];
        return (
          <li key={attemptId} className="flex gap-3 pb-[18px] last:pb-0">
            <div className="flex flex-none flex-col items-center">
              <span
                className={cn(
                  "mt-1 h-[11px] w-[11px] flex-none rounded-full ring-2 ring-surface",
                  t.dot,
                )}
              />
              {!isLast && <span className="mt-[3px] w-px flex-1 bg-line" />}
            </div>
            <button
              type="button"
              onClick={() => onSelect?.(a)}
              aria-current={selected ? "true" : undefined}
              aria-label={`Show activity for attempt ${a.n}`}
              className={cn(
                "-mt-0.5 min-w-0 rounded-md pb-1 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40",
                onSelect ? "cursor-pointer hover:bg-hover" : "cursor-default",
                selected && "bg-hover",
              )}
            >
              <div className="flex flex-wrap items-center gap-2 px-1 text-[13px] font-semibold text-ink">
                Attempt {a.n}
                <OutcomeChip outcome={a.outcome} />
                {selected && <Mono className="text-[10px] font-normal text-info">selected</Mono>}
              </div>
              {/* TODO(api): per-attempt summary/note — Attempt carries no prose today. */}
              <Mono className="mt-1.5 block px-1 text-[10.5px] text-faint">{attemptMeta(a)}</Mono>
              <div className="mt-1 px-1 text-[12px] text-muted">started {relativeTime(a.startedAt)}</div>
            </button>
          </li>
        );
      })}
    </ol>
  );
}
