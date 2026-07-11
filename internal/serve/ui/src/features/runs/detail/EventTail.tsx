import { useEffect, useRef, useState } from "react";
import type { Liveness, RunEvent } from "@/types/domain";
import { cn } from "@/lib/cn";
import { Toggle } from "@/components/ui/controls";
import { LivenessIndicator } from "@/components/ui/liveness";
import { SectionLabel } from "@/components/ui/page";
import { Mono } from "@/components/ui/primitives";
import { clockTime, eventToneClasses } from "@/features/runs/detail/helpers";

/**
 * Live event tail — compact monospace console (design §07). Lines are colored by
 * level; a liveness indicator shows event freshness and an auto-follow toggle
 * pins the view to the newest line. Scrolling up releases the pin so history can
 * be read; toggling follow back on re-pins to the bottom.
 *
 * Note: the console is a themed panel (token-driven) rather than a hardcoded
 * dark terminal, so it flips correctly light↔dark per the foundation rules.
 * The tail stays un-virtualized — mock tails are short; swap in
 * @tanstack/react-virtual here if real tails grow large.
 */
export function EventTail({
  events,
  liveness,
  sinceLastEventSec,
  waitingForDaemonReason,
}: {
  events: RunEvent[];
  liveness: Liveness;
  sinceLastEventSec: number;
  waitingForDaemonReason?: string | null;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [autoFollow, setAutoFollow] = useState(true);

  useEffect(() => {
    if (!autoFollow) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [events, autoFollow]);

  function handleScroll() {
    const el = scrollRef.current;
    if (!el || !autoFollow) return;
    const fromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (fromBottom > 40) setAutoFollow(false);
  }

  return (
    <div className="min-w-0">
      <div className="mb-3 flex items-center justify-between gap-3">
        <SectionLabel>Event tail</SectionLabel>
        <div className="flex items-center gap-3.5">
          {waitingForDaemonReason ? (
            <Mono className="text-[11px] text-warn" title={waitingForDaemonReason}>
              Waiting for daemon
            </Mono>
          ) : (
            <LivenessIndicator liveness={liveness} sinceSec={sinceLastEventSec} />
          )}
          <Toggle checked={autoFollow} onChange={setAutoFollow} label="auto-follow" />
        </div>
      </div>
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="tk-scroll h-[360px] overflow-y-auto rounded-[10px] border border-line bg-panel px-4 py-3.5"
      >
        {events.length === 0 ? (
          <div className="flex h-full items-center justify-center text-center text-[12px] text-faint">
            {waitingForDaemonReason
              ? "No new events while the daemon is down."
              : "No events yet — waiting for the first protocol event."}
          </div>
        ) : (
          events.map((ev, i) => {
            const c = eventToneClasses(ev.level);
            return (
              <div
                key={`${ev.ts}-${i}`}
                className="flex gap-2.5 font-mono text-[11.5px] leading-[1.75]"
              >
                <span className="flex-none tabular text-faint">{clockTime(ev.ts)}</span>
                <span className={cn("w-[132px] flex-none truncate", c.kind)} title={ev.kind}>
                  {ev.kind}
                </span>
                <span className={cn("min-w-0 break-words", c.text)}>{ev.text}</span>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}
