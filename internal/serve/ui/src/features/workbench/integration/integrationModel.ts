import type { WaveReview, WaveSummary } from "@/types/domain";

export type WaveReviewStage = "ready" | "queued" | "executing" | "awaiting_review" | "reviewing" | "paused" | "blocked" | "failed" | "completed" | "cancelled" | "unknown";

export function usableWaveReview(review: WaveReview | undefined, error?: unknown): WaveReview | undefined {
  return error ? undefined : review;
}

/** Results are acceptance evidence, so a failed refresh invalidates cached data. */
export function canShowWaveResults(review: WaveReview | undefined, error?: unknown): boolean {
  const current = usableWaveReview(review, error);
  return current ? waveReviewStage(current) === "completed" : false;
}

export function waveSummaryWithReview(wave: WaveSummary, review: WaveReview | undefined, error?: unknown): WaveSummary {
  if (error) return { ...wave, status: "unknown", landedAt: null };
  const stage = review && waveReviewStage(review);
  if (stage === "unknown") return { ...wave, status: "unknown", landedAt: null };
  if (stage === "completed") return { ...wave, status: "completed" };
  if (stage === "cancelled") return { ...wave, status: "cancelled", landedAt: null };
  // A review in any other known state is newer authority than a terminal
  // summary left behind by an earlier read. It must not redirect into Results.
  if (review && ["landed", "closed", "completed", "cancelled"].includes(wave.status.toLowerCase())) {
    return { ...wave, status: "open", landedAt: null };
  }
  return wave;
}

export type WorkView = "work" | "flow" | "results";

export function initialWaveView(_wave: WaveSummary, requested?: string): WorkView {
  if (requested === "work" || requested === "flow" || requested === "results") return requested;
  return "flow";
}

/** Wait for the authoritative review before fixing an implicit entry tab. */
export function settleEnteredWaveView(
  current: WorkView | null,
  wave: WaveSummary,
  reviewPending: boolean,
  requested?: string,
): WorkView | null {
  return current ?? (reviewPending ? null : initialWaveView(wave, requested));
}

/**
 * One interpretation of the authoritative review projection for every surface.
 * Durable task fields and old mutation responses never substitute for this.
 */
export function waveReviewStage(review: WaveReview): WaveReviewStage {
  if (review.authorization === "stale") return "unknown";
  if (review.state === "Cancelled") return "cancelled";
  if (review.state === "Completed") return "completed";
  if (review.state === "Paused") return "paused";
  if (review.members.some((member) => member.phase === "failed")) return "failed";
  if (review.members.some((member) => member.phase === "proof_blocked")) return "blocked";
  if (review.members.some((member) => member.state === "blocked")) return "failed";
  if (review.members.some((member) => member.phase === "reviewing")) return "reviewing";
  if (review.members.some((member) => member.phase === "awaiting_review" || member.waitingReason?.includes("independent review"))) return "awaiting_review";
  if (review.members.some((member) => member.phase === "executing" || member.state === "running")) return "executing";
  if (review.humanActions?.length || review.blockers.some((blocker) => blocker.code !== "DEPENDENCY_WAITING" && blocker.code !== "HUMAN_GATE_OPEN")) return "blocked";
  if (review.controls.some((control) => control.action === "wave start" && control.enabled)) return "ready";
  return review.authorization === "authorized" ? "queued" : "blocked";
}

export function restoredPath(savedPath: string | undefined, explicitPath: string): string {
  return explicitPath || savedPath || "/";
}
