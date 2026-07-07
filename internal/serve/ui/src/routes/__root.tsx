import { Outlet } from "@tanstack/react-router";
import { AlertTriangle } from "lucide-react";
import { Sidebar } from "@/components/Sidebar";
import { useDaemon } from "@/lib/queries";

/** App shell: fixed sidebar + a single scrolling content pane. */
export function RootLayout() {
  return (
    <div className="flex h-screen w-full overflow-hidden bg-surface text-ink">
      <Sidebar />
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <EscalationBanner />
        <InvariantCircuitBanner />
        <div className="min-h-0 flex-1 overflow-hidden">
          <Outlet />
        </div>
      </main>
    </div>
  );
}

/** Pass-through layout for project-scoped routes. */
export function ProjectLayout() {
  return <Outlet />;
}

function InvariantCircuitBanner() {
  const daemon = useDaemon();
  const circuit = daemon.data?.invariantCircuit;
  if (circuit?.open !== true) {
    return null;
  }
  const detail = circuit.violations?.[0]?.detail ?? circuit.summary ?? circuit.reason ?? "invariant_violation";
  return (
    <div className="flex flex-none items-center gap-2 border-b border-fail/30 bg-fail-soft px-4 py-2 text-[13px] font-medium text-fail">
      <AlertTriangle size={15} aria-hidden="true" />
      <span className="font-semibold">Invariant circuit open</span>
      <span className="min-w-0 truncate text-fail/90">{detail}</span>
    </div>
  );
}

function EscalationBanner() {
  const daemon = useDaemon();
  if (daemon.data?.persistentEscalationBanner !== true) {
    return null;
  }
  return (
    <div className="flex flex-none items-center gap-2 border-b border-fail/40 bg-fail px-4 py-2 text-[13px] font-semibold text-white">
      <AlertTriangle size={15} aria-hidden="true" />
      <span>P0 escalation open</span>
      <span className="min-w-0 truncate font-medium text-white/90">Open the morning digest for details.</span>
    </div>
  );
}
