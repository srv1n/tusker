import { useEffect, useRef, useState } from "react";
import type { Liveness, RunActivityFreshness, RunEvent } from "@/types/domain";
import { cn } from "@/lib/cn";
import { Button, Toggle } from "@/components/ui/controls";
import { LivenessIndicator } from "@/components/ui/liveness";
import { SectionLabel } from "@/components/ui/page";
import { Mono } from "@/components/ui/primitives";
import { clockTime, eventToneClasses } from "@/features/runs/detail/helpers";
import { duration, relativeTime } from "@/lib/time";

/**
 * Live event tail — compact monospace console (design §07). Lines are colored by
 * level; a liveness indicator shows event freshness and an auto-follow toggle
 * pins the view to the newest line. Scrolling up releases the pin so history can
 * be read; toggling follow back on re-pins to the bottom.
 *
 * Note: the console is a themed panel (token-driven) rather than a hardcoded
 * dark terminal, so it flips correctly light↔dark per the foundation rules.
 * The API bounds this view to 50 activity records and 50 diagnostics.
 */
export function EventTail({
  events,
  liveness,
  sinceLastEventSec,
  waitingForDaemonReason,
  activity,
  historicalAttempt,
}: {
  events: RunEvent[];
  liveness: Liveness;
  sinceLastEventSec: number;
  waitingForDaemonReason?: string | null;
  activity?: RunActivityFreshness;
  historicalAttempt?: boolean;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [autoFollow, setAutoFollow] = useState(true);
  const [showEvents, setShowEvents] = useState(false);
  const messages = events.filter((event) => event.activity);
  const visible = showEvents ? events : messages;

  useEffect(() => {
    if (!autoFollow) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [events, autoFollow, showEvents]);

  function handleScroll() {
    const el = scrollRef.current;
    if (!el || !autoFollow) return;
    const fromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (fromBottom > 40) setAutoFollow(false);
  }

  return (
    <div className="min-w-0">
      <div className="mb-3 flex items-center justify-between gap-3">
        <SectionLabel>{showEvents ? "Event tail" : historicalAttempt ? "Attempt messages" : "Recent messages"}</SectionLabel>
        <div className="flex items-center gap-3.5">
          {waitingForDaemonReason ? (
            <Mono className="text-[11px] text-warn" title={waitingForDaemonReason}>
              Waiting for daemon
            </Mono>
          ) : (
            <span className="flex items-center gap-1.5" title="Heartbeat freshness does not prove work progress">
              <Mono className="text-[10px] text-faint">last event</Mono>
              <LivenessIndicator liveness={liveness} sinceSec={sinceLastEventSec} />
            </span>
          )}
          <Toggle checked={autoFollow} onChange={setAutoFollow} label="auto-follow" />
        </div>
      </div>
      <ActivityFreshness activity={activity} eventAgeSec={sinceLastEventSec} historicalAttempt={historicalAttempt} />
      <div className="mb-2 flex gap-3 text-[11px]">
        <Button type="button" size="sm" variant={!showEvents ? "subtle" : "ghost"} aria-pressed={!showEvents} onClick={() => setShowEvents(false)}>Messages &amp; tools</Button>
        <Button type="button" size="sm" variant={showEvents ? "subtle" : "ghost"} aria-pressed={showEvents} onClick={() => setShowEvents(true)}>All events</Button>
      </div>
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="tk-scroll h-[360px] overflow-y-auto rounded-[10px] border border-line bg-panel px-4 py-3.5"
      >
        {visible.length === 0 ? (
          <div className="flex h-full items-center justify-center text-center text-[12px] text-faint">
            {waitingForDaemonReason
              ? "No new events while the daemon is down."
              : showEvents
                ? "No events recorded for this attempt."
                : "No messages captured for this attempt. Older ACP runs only recorded event names; heartbeat freshness does not establish progress."}
          </div>
        ) : (
          visible.map((ev, i) => {
            const c = eventToneClasses(ev.level);
            return (
              <div
                key={ev.id ?? `${ev.ts}-${i}`}
                className="border-b border-line-soft py-2 font-mono text-[11.5px] leading-[1.75] last:border-0"
              >
                <div className="flex flex-wrap gap-2.5">
                  <span className="tabular text-faint" title={ev.ts || "This runner did not provide a timestamp"}>{ev.ts ? `${clockTime(ev.ts)} UTC` : "Time unavailable"}</span>
                  <span className={c.kind}>{ev.kind.replaceAll("_", " ")}</span>
                </div>
                <div className={cn("min-w-0 whitespace-pre-wrap break-words [overflow-wrap:anywhere]", c.text)}>{ev.text}</div>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}

function ActivityFreshness({
  activity,
  eventAgeSec,
  historicalAttempt,
}: {
  activity?: RunActivityFreshness;
  eventAgeSec: number;
  historicalAttempt?: boolean;
}) {
  const captureState = activity?.captureState;
  const rows = [
    ["heartbeat", activity?.heartbeatAgeSec, activity?.heartbeatAt],
    ["message", activity?.messageAgeSec, activity?.messageAt],
    ["tool progress", activity?.toolAgeSec, activity?.toolAt],
  ] as const;
  return (
    <div className="mb-3 rounded-md border border-line-soft bg-panel px-3 py-2 text-[10.5px]" data-activity-freshness>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        {rows.map(([label, age, at]) => (
          <span key={label} className="text-muted">
            <span className="text-faint">{label}</span>{" "}
            <Mono className="text-ink-soft">{freshnessLabel(age, at)}</Mono>
          </span>
        ))}
        {historicalAttempt && <Mono className="text-info">historical attempt</Mono>}
      </div>
      {captureState && captureState !== "available" && (
        <div className="mt-1 text-warn">
          Activity capture {captureState}: {activity?.captureReason ?? "the provider did not expose a readable message feed"}
        </div>
      )}
      {!activity && !historicalAttempt && (
        <div className="mt-1 text-faint">Activity freshness is unavailable; last event age {duration(Math.max(0, eventAgeSec))} does not prove forward progress.</div>
      )}
    </div>
  );
}

function freshnessLabel(age: number | null | undefined, at: string | null | undefined): string {
  if (typeof age === "number" && Number.isFinite(age) && age >= 0) return `${duration(age)} ago`;
  if (at) {
    const timestamp = new Date(at);
    if (!Number.isNaN(timestamp.getTime())) return relativeTime(at);
  }
  return "not observed";
}
