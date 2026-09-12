import { useSyncExternalStore } from "react";
import { getStreamStatus, subscribeStreamStatus } from "@/lib/stream";

/**
 * Honest liveness for the Work surface (real-work-ui-acceptance A4).
 *
 * The shared SSE subscription in main.tsx owns freshness; while it is
 * disconnected the queries fall back to interval refetch, so the operator
 * must see that the view may be stale. Connectivity text is the live
 * region; the last-event age is supplementary and stays out of it so every
 * event does not re-announce.
 */
export function StreamStatusNote() {
  const status = useSyncExternalStore(subscribeStreamStatus, getStreamStatus, getStreamStatus);
  const lastEvent = status.lastEventAt === null ? "No events received yet" : `Last event ${new Date(status.lastEventAt).toLocaleString()}`;
  return (
    <p
      data-testid="stream-status"
      aria-live="polite"
      title={lastEvent}
      className="flex items-center gap-1.5 text-[11.5px] text-faint"
    >
      <span
        aria-hidden="true"
        data-testid="stream-status-dot"
        data-connected={status.connected ? "true" : "false"}
        className={`inline-block h-2 w-2 rounded-full ${status.connected ? "bg-pass" : "bg-warn"}`}
      />
      {status.connected ? "Live updates" : "Reconnecting — showing last known state"}
    </p>
  );
}
