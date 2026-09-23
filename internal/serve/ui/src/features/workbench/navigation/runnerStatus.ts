import type { DaemonStatus, ProjectSummary } from "@/types/domain";

export interface RunnerStatus {
  label: string;
  tone: "fail" | "warn" | "pass" | "muted";
  reason?: string;
}

/** One-line rail status. An open circuit always wins: a real fault is never hidden. */
export function runnerStatus(daemon: DaemonStatus | undefined, project?: ProjectSummary): RunnerStatus {
  if (daemon?.crashLoop?.open) {
    const detail = daemon.crashLoop.summary ?? daemon.crashLoop.reason ?? "crash_loop";
    return { label: "Auto-run stopped", tone: "fail", reason: `Daemon crash loop: ${detail}. Run tusker daemon resume.` };
  }
  if (daemon?.invariantCircuit?.open) {
    const circuit = daemon.invariantCircuit;
    const detail = circuit.violations?.[0]?.detail ?? circuit.summary ?? circuit.reason ?? "invariant_violation";
    return { label: "Auto-run stopped", tone: "fail", reason: `Invariant circuit open: ${detail}` };
  }
  if (!daemon) return { label: "Runner unknown", tone: "muted" };
  if (daemon.daemonAlive === false) {
    return { label: "Runner offline", tone: "warn", reason: daemon.daemonDownReason ?? undefined };
  }
  if (project && !project.automationEnabled) return { label: "Auto-run off", tone: "muted" };
  if (daemon.activeRuns > 0) return { label: `${daemon.activeRuns} running`, tone: "pass" };
  return { label: "Runner idle", tone: "pass" };
}
