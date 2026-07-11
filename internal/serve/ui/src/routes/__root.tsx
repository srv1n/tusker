import { useEffect, useState } from "react";
import { Link, Outlet, useLocation } from "@tanstack/react-router";
import { AlertTriangle, Menu } from "lucide-react";
import { Sidebar } from "@/components/Sidebar";
import { CountBadge } from "@/components/ui/primitives";
import { useDaemon, useNeeds } from "@/lib/queries";

let shellMode = typeof window !== "undefined" &&
  new URLSearchParams(window.location.search).get("shell") === "1";

export function isTuskerShellMode(): boolean {
  if (typeof navigator !== "undefined" && navigator.userAgent.includes("TuskerShell/")) shellMode = true;
  return shellMode;
}

/**
 * App shell. ≥lg: static sidebar + a single scrolling content pane. <lg: the
 * sidebar becomes an overlay drawer behind a hamburger in a mobile top bar.
 */
export function RootLayout() {
  const location = useLocation();
  const embedded = isTuskerShellMode() && location.pathname === "/panel";
  const [navOpen, setNavOpen] = useState(false);

  // Any navigation closes the drawer — a tapped link should reveal its page.
  useEffect(() => {
    setNavOpen(false);
  }, [location.pathname]);

  return (
    <div className="flex h-dvh w-full overflow-hidden bg-surface text-ink">
      {!embedded && <Sidebar open={navOpen} onClose={() => setNavOpen(false)} />}
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        {!embedded && <MobileTopBar onMenuOpen={() => setNavOpen(true)} />}
        {!embedded && <InvariantCircuitBanner />}
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

/** <lg only: hamburger + brand, with a needs-me shortcut when items are waiting. */
function MobileTopBar({ onMenuOpen }: { onMenuOpen: () => void }) {
  const needs = useNeeds();
  const needsCount = needs.data?.length ?? 0;
  return (
    <header className="flex flex-none items-center gap-1 border-b border-line bg-panel px-2 pb-1.5 pt-[max(0.375rem,env(safe-area-inset-top))] lg:hidden">
      <button
        type="button"
        onClick={onMenuOpen}
        aria-label="Open navigation"
        className="flex h-10 w-10 items-center justify-center rounded-lg text-ink-soft transition-colors hover:bg-hover active:bg-active"
      >
        <Menu size={18} />
      </button>
      <Link to="/" className="flex items-center gap-2 py-2 pr-2">
        <span className="flex h-5 w-5 items-center justify-center rounded-md bg-ink font-serif text-[12px] font-semibold text-surface">
          t
        </span>
        <span className="font-serif text-[16px] font-semibold tracking-[-0.01em] text-ink">tusker</span>
      </Link>
      {needsCount > 0 && (
        <Link
          to="/"
          aria-label={`${needsCount} items need you`}
          className="ml-auto flex h-10 items-center px-2"
        >
          <CountBadge count={needsCount} tone="fail" />
        </Link>
      )}
    </header>
  );
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
