/*
  Shared layout shell for the three knowledge routes (SRV-T-0004).

  Each route (list, reader, graph) wraps its own content in this shell, which
  renders the persistent explorer rail to the left. On wide viewports the rail is
  a static column; on narrow viewports it collapses to an off-canvas drawer so it
  never crowds the reader, toggled by <RailToggle/> placed in each view's header.

  Collapse/filter state lives in treeStore, not in this component, so it survives
  the per-route remount (no router change is involved).
*/

import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { PanelLeft } from "lucide-react";
import { cn } from "@/lib/cn";
import { KnowledgeTree } from "./KnowledgeTree";
import { useTreeStore } from "./treeStore";

export function KnowledgeShell({
  projectId,
  currentSubject,
  children,
}: {
  projectId: string;
  currentSubject?: string;
  children: ReactNode;
}) {
  const store = useTreeStore();
  const open = store.isRailOpen();

  return (
    <div className="flex h-full min-h-0 w-full">
      {/* Static rail — wide viewports. */}
      <aside className="hidden w-64 flex-none border-r border-line lg:block">
        <KnowledgeTree projectId={projectId} currentSubject={currentSubject} />
      </aside>

      {/* View content. */}
      <div className="min-h-0 min-w-0 flex-1">{children}</div>

      {/* Off-canvas drawer — narrow viewports. */}
      {open && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <div
            className="absolute inset-0 bg-black/30 backdrop-blur-[1px]"
            onClick={() => store.setRailOpen(false)}
          />
          <aside className="absolute inset-y-0 left-0 flex w-72 max-w-[85%] flex-col border-r border-line bg-surface shadow-xl">
            <KnowledgeTree projectId={projectId} currentSubject={currentSubject} />
          </aside>
        </div>
      )}
    </div>
  );
}

/**
 * Slim top toolbar shared by every knowledge view — one row: the rail toggle
 * (narrow only), a left context slot, and a right controls slot. Height and
 * treatment match the app's other section toolbars.
 */
export function SectionToolbar({ left, right }: { left?: ReactNode; right?: ReactNode }) {
  return (
    <header className="sticky top-0 z-20 flex h-11 flex-none items-center gap-2 border-b border-line bg-surface/85 px-3 backdrop-blur-md sm:px-4">
      <RailToggle />
      <div className="flex min-w-0 flex-1 items-center gap-2">{left}</div>
      <div className="flex flex-none items-center gap-2">{right}</div>
    </header>
  );
}

/** Compact Files/Graph view switch (segmented-control styling). */
export function ViewSwitch({ projectId, active }: { projectId: string; active: "files" | "graph" }) {
  const tab = (on: boolean) =>
    cn(
      "rounded-md px-2.5 py-1 text-[12px] font-medium transition-colors",
      on ? "bg-raised text-ink shadow-sm" : "text-muted hover:text-ink-soft",
    );
  return (
    <div className="inline-flex items-center gap-0.5 rounded-lg border border-line bg-panel p-0.5">
      <Link to="/p/$projectId/knowledge" params={{ projectId }} className={tab(active === "files")}>
        Files
      </Link>
      <Link to="/p/$projectId/knowledge/graph" params={{ projectId }} className={tab(active === "graph")}>
        Graph
      </Link>
    </div>
  );
}

/** Rail toggle for narrow viewports — placed in each view's header. */
export function RailToggle({ className }: { className?: string }) {
  const store = useTreeStore();
  return (
    <button
      type="button"
      onClick={() => store.toggleRail()}
      aria-label="Toggle docs explorer"
      className={cn(
        "flex h-7 w-7 flex-none items-center justify-center rounded-lg text-muted transition-colors hover:bg-hover hover:text-ink lg:hidden",
        className,
      )}
    >
      <PanelLeft size={15} strokeWidth={2} />
    </button>
  );
}
