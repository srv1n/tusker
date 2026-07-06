import { cn } from "@/lib/cn";
import { Dot } from "@/components/ui/primitives";
import { livenessTone } from "@/components/ui/tone";
import { sinceLabel } from "@/lib/time";
import type { Liveness } from "@/types/domain";

/**
 * Liveness indicator (packet §4.2). A run showing "running" with a dead process
 * must be visually alarming within a minute: fresh = green + pulse, stale =
 * amber, dead = red (steady, loud). The label is time-since-last-event.
 */
export function LivenessIndicator({
  liveness,
  sinceSec,
  showLabel = true,
}: {
  liveness: Liveness;
  sinceSec: number;
  showLabel?: boolean;
}) {
  const t = livenessTone[liveness];
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5",
        liveness === "dead" ? "text-fail" : liveness === "stale" ? "text-warn" : "text-muted",
      )}
      title={`Last event ${sinceLabel(sinceSec)} ago`}
    >
      <Dot tone={t} pulse={liveness === "fresh"} size={liveness === "dead" ? 8 : 7} />
      {showLabel && (
        <span className={cn("font-mono tabular text-[11px]", liveness === "dead" && "font-semibold")}>
          {sinceLabel(sinceSec)}
        </span>
      )}
    </span>
  );
}

/** Bare liveness dot for dense contexts (sidebar, table cells). */
export function LivenessDot({ liveness }: { liveness: Liveness }) {
  return <Dot tone={livenessTone[liveness]} pulse={liveness === "fresh"} size={7} />;
}
