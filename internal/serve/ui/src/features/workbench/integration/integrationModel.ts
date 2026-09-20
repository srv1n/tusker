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

function reviewErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Wave status could not be loaded. Try refreshing.";
}

function activeMemberStage(review: WaveReview): "executing" | "reviewing" | undefined {
  if (review.members.some((member) => member.phase === "reviewing")) return "reviewing";
  return review.members.some((member) => member.phase === "executing" || member.state === "running") ? "executing" : undefined;
}

function reviewHumanAction(review: WaveReview): string | undefined {
  return review.humanActions?.map((item) => item.action.action.trim()).find(Boolean);
}

function reviewBlocker(review: WaveReview): string | undefined {
  return review.blockers.find((blocker) => blocker.code !== "DEPENDENCY_WAITING" && blocker.code !== "HUMAN_GATE_OPEN")?.reason;
}

export function waveStartability(
  waves: WaveSummary[],
  reviews: WaveReview[] = [],
  reviewErrors: Record<string, unknown> = {},
) {
  return Object.fromEntries(waves.map((wave) => {
    const error = reviewErrors[wave.id];
    const review = usableWaveReview(reviews.find((item) => item.waveId === wave.id), error);
    const start = review?.controls.find((item) => item.action === "wave start");
    const stage = review && waveReviewStage(review);
    return [wave.id, !review ? {
      state: "unknown" as const,
      // An error is authoritative enough to suppress a stale terminal summary;
      // an absent review without an error still permits legacy-only callers.
      ...(error ? { stage: "unknown" as const } : {}),
      reason: reviewErrorMessage(error),
    } : {
      state: stage === "ready" ? "ready" as const : "blocked" as const,
      stage,
      activeStage: activeMemberStage(review),
      humanAction: reviewHumanAction(review),
      blocker: reviewBlocker(review),
      reason: stage === "queued" ? "Authorized; waiting for the next eligible task." : stage === "completed" || stage === "cancelled" ? undefined : start?.reason,
    }];
  }));
}

export function restoredPath(savedPath: string | undefined, explicitPath: string): string {
  return explicitPath || savedPath || "/";
}
