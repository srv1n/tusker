import type { WaveSummary } from "@/types/domain";

export type WorkView = "flow" | "results";

export function initialWaveView(wave: WaveSummary, requested?: string): WorkView {
  if (requested === "flow" || requested === "results") return requested;
  return wave.landedAt || ["landed", "closed"].includes(wave.status) ? "results" : "flow";
}

/**
 * Entry-view latch for the wave detail (real-work-ui-acceptance A3/A6).
 * The first computed view sticks: a wave that lands while the operator
 * watches Flow keeps Flow instead of yanking into Results mid-interaction.
 * An already-completed wave still leads with its result because the latch
 * starts empty on entry.
 */
export function nextEnteredView(current: WorkView | null, wave: WaveSummary, requested?: string): WorkView {
  return current ?? initialWaveView(wave, requested);
}

export function waveStartability(waves: WaveSummary[]) {
  return Object.fromEntries(waves.map((wave) => [wave.id, {
    state: "unknown" as const,
    reason: "Authoritative start readiness is not exposed by the current Serve API.",
  }]));
}

export function restoredPath(savedPath: string | undefined, explicitPath: string): string {
  return explicitPath || savedPath || "/";
}
