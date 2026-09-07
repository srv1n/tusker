import type { WaveSummary } from "@/types/domain";

export type WorkView = "flow" | "results";

export function initialWaveView(wave: WaveSummary, requested?: string): WorkView {
  if (requested === "flow" || requested === "results") return requested;
  return wave.landedAt || ["landed", "closed"].includes(wave.status) ? "results" : "flow";
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
