import { AlertTriangle } from "lucide-react";
import type { DaemonStatus } from "@/types/domain";

type CrashLoopCircuit = DaemonStatus["crashLoop"];

export function CrashLoopCircuitBanner({
  circuit,
  embedded: _embedded,
}: {
  circuit: CrashLoopCircuit;
  embedded: boolean;
}) {
  if (circuit?.open !== true) {
    return null;
  }
  const detail = circuit.summary ?? circuit.reason ?? "crash_loop";
  return (
    <div className="flex flex-none items-center gap-2 border-b border-fail/40 bg-fail px-4 py-2 text-[13px] font-semibold text-white">
      <AlertTriangle size={15} aria-hidden="true" />
      <span>Daemon crash loop</span>
      <span className="min-w-0 truncate font-medium text-white/90">{detail}</span>
      <span className="ml-auto whitespace-nowrap font-mono text-[11px]">tusker daemon resume</span>
    </div>
  );
}
