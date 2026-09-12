/*
  Shared layout shell for the three knowledge routes.

  Each route (list, reader, graph) wraps its own content in this shell, which
  renders the persistent explorer rail to the left. On wide viewports the rail is
  a static column; on narrow viewports it collapses to an off-canvas drawer so it
  never crowds the reader, toggled by <RailToggle/> placed in each view's header.

  Collapse/filter state lives in treeStore, not in this component, so it survives
  the per-route remount (no router change is involved).
*/

import { useEffect, useRef, type ReactNode } from "react";
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
  const drawerRef = useRef<HTMLElement>(null);
  const openerRef = useRef<HTMLElement | null>(null);
  const openFocusFrameRef = useRef<number | null>(null);

  useEffect(() => {
    if (!open) {
      openerRef.current?.focus();
      openerRef.current = null;
      return;
    }
    openerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        event.preventDefault();
        store.setRailOpen(false);
        return;
      }
      if (event.key !== "Tab") return;
      const drawer = drawerRef.current;
      if (!drawer) return;
      const focusable = Array.from(
        drawer.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ),
      );
      if (focusable.length === 0) {
        event.preventDefault();
        drawer.focus();
        return;
      }
      const first = focusable[0]!;
      const last = focusable[focusable.length - 1]!;
      if (document.activeElement === drawer || !drawer.contains(document.activeElement)) {
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    openFocusFrameRef.current = requestAnimationFrame(() => drawerRef.current?.focus());
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      if (openFocusFrameRef.current !== null) {
        cancelAnimationFrame(openFocusFrameRef.current);
        openFocusFrameRef.current = null;
      }
    };
  }, [open, store]);

  return (
    <div className="flex h-full min-h-0 w-full">
      {/* Static rail — wide viewports. */}
      <aside aria-label="Documents explorer" className="hidden w-[280px] flex-none lg:block">
        <KnowledgeTree projectId={projectId} currentSubject={currentSubject} />
      </aside>

      {/* View content. */}
      <div className="min-h-0 min-w-0 flex-1">{children}</div>

      {/* Off-canvas drawer — narrow viewports. */}
      {open && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <div
            aria-hidden="true"
            className="absolute inset-0 bg-black/25"
            onClick={() => store.setRailOpen(false)}
          />
          <aside
            ref={drawerRef}
            aria-label="Documents explorer"
            aria-modal="true"
            className="absolute inset-y-0 left-0 flex w-[280px] max-w-[calc(100%-32px)] flex-col bg-surface shadow-lg focus:outline-none"
            role="dialog"
            tabIndex={-1}
            onClickCapture={(event) => {
              // A document link can unmount this shell before the close effect
              // runs. Queue focus against the replacement toggle as a fallback;
              // route chunk loading can take longer than one animation frame.
              const target = event.target as Element | null;
              if (!target?.closest("a[href]")) return;
              let previousToggle = document.querySelector<HTMLElement>('[aria-label="Toggle docs explorer"]');
              const restore = (attempt = 0): void => {
                // Route chunk loading may replace the current shell after the
                // drawer has closed. Wait for the replacement toggle, while
                // stopping if the operator immediately reopens the drawer.
                if (document.querySelector('[role="dialog"]')) return;
                const toggle = document.querySelector<HTMLElement>('[aria-label="Toggle docs explorer"]');
                const active = document.activeElement;
                // Stop once the operator moves to another control. Only repair
                // focus lost when navigation replaces the previous toggle.
                if (active !== document.body && active !== previousToggle && active !== toggle) return;
                if (toggle) {
                  if (document.activeElement !== toggle) toggle.focus();
                  previousToggle = toggle;
                }
                if (attempt < 40) window.setTimeout(() => restore(attempt + 1), 50);
              };
              window.setTimeout(() => restore(), 0);
            }}
          >
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
      <Link
        to="/p/$projectId/knowledge"
        params={{ projectId }}
        aria-current={active === "files" ? "page" : undefined}
        className={tab(active === "files")}
      >
        Files
      </Link>
      <Link
        to="/p/$projectId/knowledge/graph"
        params={{ projectId }}
        aria-current={active === "graph" ? "page" : undefined}
        className={tab(active === "graph")}
      >
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
