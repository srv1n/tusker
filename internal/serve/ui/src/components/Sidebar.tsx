import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { Link, useNavigate, useParams, useRouterState } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, Monitor, Moon, Plus, Search, Sun, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { CountBadge, Dot, Mono } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import { useDaemon, useNeeds, useProjects, useRegisterProject } from "@/lib/queries";
import { formatStreamAge, getStreamStatus, subscribeStreamStatus } from "@/lib/stream";
import { useTheme } from "@/lib/theme";
import { USE_MOCK } from "@/lib/api";
import type { ProjectSummary } from "@/types/domain";

// Project settings live behind the "Details" button on the Overview, not as a
// separate rail item — keeps the sidebar minimal.
const SUBSECTIONS = [
  { key: "overview", label: "Overview", to: "/p/$projectId" as const },
  { key: "needs", label: "Needs me", to: "/p/$projectId/needs" as const },
  { key: "runs", label: "Runs", to: "/p/$projectId/runs" as const },
  { key: "work", label: "Work", to: "/p/$projectId/work" as const },
  { key: "ops", label: "Ops", to: "/p/$projectId/ops" as const },
  { key: "docs", label: "Library", to: "/p/$projectId/docs" as const },
];

export function Sidebar({ open = false, onClose }: { open?: boolean; onClose?: () => void }) {
  const projects = useProjects();
  const daemon = useDaemon();
  const globalNeeds = useNeeds();
  const stream = useSyncExternalStore(subscribeStreamStatus, getStreamStatus, getStreamStatus);
  const activeProject = useParams({ strict: false }).projectId as string | undefined;
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const [addingProject, setAddingProject] = useState(false);
  const asideRef = useRef<HTMLElement>(null);

  const needsCount = globalNeeds.data?.length ?? 0;
  const daemonLive = !!daemon.data?.connected && stream.connected;
  const budgetCircuitOpen = daemon.data?.budgetCircuit?.open === true;
  const invariantCircuitOpen = daemon.data?.invariantCircuit?.open === true;

  // Drawer keyboard/focus contract: Escape closes; opening moves focus into
  // the drawer so keyboard and screen-reader users land where the tap went.
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose?.();
    };
    window.addEventListener("keydown", onKey);
    if (window.matchMedia("(max-width: 1023px)").matches) {
      asideRef.current?.focus();
    }
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  return (
    <>
      {/* Mobile scrim — tap to dismiss. Hidden ≥lg where the rail is static. */}
      {open && (
        <div
          className="fixed inset-0 z-40 bg-black/40 lg:hidden"
          onClick={onClose}
          aria-hidden="true"
        />
      )}
      <aside
        ref={asideRef}
        tabIndex={-1}
        aria-label="Navigation"
        className={cn(
          // <lg: off-canvas drawer. `invisible` (not display:none) keeps the
          // slide-out animation and removes the closed drawer from the focus
          // order and accessibility tree.
          "fixed inset-y-0 left-0 z-50 flex w-[264px] max-w-[85vw] flex-none flex-col border-r border-line bg-panel pb-4 pl-[env(safe-area-inset-left)] pt-[max(1rem,env(safe-area-inset-top))] transition-[transform,visibility] duration-200 ease-out focus-visible:outline-none",
          // ≥lg: the original static 246px rail, always visible, no animation.
          "lg:static lg:z-auto lg:w-[246px] lg:max-w-none lg:translate-x-0 lg:pl-0 lg:pt-4 lg:transition-none lg:visible",
          open ? "translate-x-0" : "-translate-x-full max-lg:invisible",
        )}
      >
      {/* Brand */}
      <div className="flex items-center gap-2.5 px-[18px] pb-4">
        <span className="flex h-[22px] w-[22px] items-center justify-center rounded-md bg-ink font-serif text-[13px] font-semibold text-surface">
          t
        </span>
        <span className="font-serif text-[17px] font-semibold tracking-[-0.01em] text-ink">tusker</span>
        <SectionLabel className="mt-[3px] tracking-[0.18em]">serve</SectionLabel>
        {onClose && (
          <button
            type="button"
            onClick={onClose}
            aria-label="Close navigation"
            className="ml-auto flex h-9 w-9 items-center justify-center rounded-lg text-faint transition-colors hover:bg-hover hover:text-ink lg:hidden"
          >
            <X size={16} />
          </button>
        )}
      </div>

      {/* Global */}
      <div className="flex flex-col gap-0.5 px-3">
        <Link
          to="/"
          className={cn(
            "flex items-center justify-between gap-2 rounded-lg px-2.5 py-[9px] text-[13.5px] transition-colors hover:bg-hover",
            pathname === "/" ? "bg-hover font-semibold text-ink" : "font-medium text-ink-soft",
          )}
        >
          <span className="flex items-center gap-2.5">
            <Dot tone="fail" />
            Needs me · all
          </span>
          {needsCount > 0 && <CountBadge count={needsCount} tone="fail" />}
        </Link>
        <button className="flex items-center gap-2.5 rounded-lg px-2.5 py-[9px] text-left text-[13.5px] font-medium text-muted transition-colors hover:bg-hover">
          <Search size={13} className="text-faint" />
          Search
          <Mono className="ml-auto text-[10px] text-fainter">⌘K</Mono>
        </button>
      </div>

      {/* Projects */}
      <div className="flex items-center justify-between px-5 pb-1.5 pt-[18px]">
        <SectionLabel>Projects</SectionLabel>
        <div className="flex items-center gap-2">
          <Mono className="text-[10.5px] text-fainter">{projects.data?.length ?? 0}</Mono>
          <button
            type="button"
            onClick={() => setAddingProject((open) => !open)}
            className="flex h-5 w-5 items-center justify-center rounded text-faint transition-colors hover:bg-hover hover:text-ink"
            aria-label={addingProject ? "Close add project form" : "Add project"}
            title="Add project"
          >
            {addingProject ? <X size={12} /> : <Plus size={12} />}
          </button>
        </div>
      </div>

      <div className="tk-scroll flex-1 overflow-y-auto px-3 pb-2">
        {addingProject && <AddProjectForm onDone={() => setAddingProject(false)} />}
        {projects.data?.map((p) => (
          <ProjectRailItem
            key={p.id}
            project={p}
            active={p.id === activeProject}
            pathname={pathname}
          />
        ))}
      </div>

      {/* App settings */}
      <div className="px-3 pt-2">
        <Link
          to="/settings"
          className={cn(
            "flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-[13.5px] font-medium transition-colors hover:bg-hover",
            pathname === "/settings" ? "bg-hover text-ink" : "text-ink-soft",
          )}
        >
          <Dot tone="neutral" />
          Settings
        </Link>
      </div>

      {/* Footer: fixture flag (mock only) + daemon status + theme */}
      <div className="mx-5 mt-2.5 flex items-end justify-between border-t border-line pt-3">
        <div className="font-mono text-[10.5px] leading-[1.7] text-faint">
          {/* Honest badge: the live views (LibraryList / DocReader / DocSourceView /
              Markdown) are now purged of fixture fallbacks, so fixtures can only
              render when USE_MOCK is on — exactly when this badge shows. Keep it
              tied to USE_MOCK; do not surface it in live mode. */}
          {USE_MOCK && (
            <div
              className="mb-1 inline-flex items-center gap-1 rounded bg-warn-soft px-1.5 py-px text-[9px] font-semibold uppercase tracking-[0.12em] text-warn"
              title="Screens render mock fixtures, not live vault data. IDs and counts are illustrative until the serve API lands."
            >
              fixture data
            </div>
          )}
          {budgetCircuitOpen && (
            <div
              className="mb-1 inline-flex items-center gap-1 rounded bg-fail-soft px-1.5 py-px text-[9px] font-semibold uppercase tracking-[0.12em] text-fail"
              title={daemon.data?.budgetCircuit?.reason ?? "Budget circuit is open"}
            >
              budget circuit open
            </div>
          )}
          {invariantCircuitOpen && (
            <div
              className="mb-1 inline-flex items-center gap-1 rounded bg-fail-soft px-1.5 py-px text-[9px] font-semibold uppercase tracking-[0.12em] text-fail"
              title={daemon.data?.invariantCircuit?.summary ?? "Invariant circuit is open"}
            >
              invariant circuit open
            </div>
          )}
          <div className="flex items-center gap-1.5">
            daemon
            <span className={daemonLive ? "text-pass" : "text-fail"}>
              {daemonLive ? "● live" : "● down"}
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            stream
            <span className={stream.connected ? "text-pass" : "text-fail"}>
              {stream.connected ? "● connected" : "● disconnected"}
            </span>
          </div>
          <div>last event {formatStreamAge(stream.lastEventAt)}</div>
          <div>{daemon.data?.addr ?? "localhost:7420"}</div>
        </div>
        <ThemeToggle />
      </div>
      </aside>
    </>
  );
}

function AddProjectForm({ onDone }: { onDone: () => void }) {
  const [repoRoot, setRepoRoot] = useState("");
  const [vaultRoot, setVaultRoot] = useState("");
  const register = useRegisterProject();
  const navigate = useNavigate();

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    const result = await register.mutateAsync({
      repoRoot: repoRoot.trim(),
      ...(vaultRoot.trim() ? { vaultRoot: vaultRoot.trim() } : {}),
    });
    if (result.ok && result.projectId) {
      onDone();
      await navigate({ to: "/p/$projectId/settings", params: { projectId: result.projectId } });
    }
  };

  const message = register.error instanceof Error ? register.error.message : register.data?.reason;
  const failed = !!register.error || register.data?.ok === false;

  return (
    <form onSubmit={submit} className="mb-2 rounded-lg border border-line bg-surface p-2.5" data-add-project-form>
      <label className="block font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-faint">
        Repository path
        <input
          autoFocus
          required
          value={repoRoot}
          onChange={(event) => setRepoRoot(event.target.value)}
          placeholder="/Users/me/code/project"
          className="mt-1 w-full rounded-md border border-line bg-panel px-2 py-1.5 font-mono text-[11px] normal-case tracking-normal text-ink outline-none focus:border-accent"
        />
      </label>
      <label className="mt-2 block font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-faint">
        Vault path <span className="font-normal normal-case tracking-normal">(optional)</span>
        <input
          value={vaultRoot}
          onChange={(event) => setVaultRoot(event.target.value)}
          placeholder="defaults to .tusker"
          className="mt-1 w-full rounded-md border border-line bg-panel px-2 py-1.5 font-mono text-[11px] normal-case tracking-normal text-ink outline-none focus:border-accent"
        />
      </label>
      <p className="mt-2 text-[10.5px] leading-snug text-faint">Registers only. Daemon automation stays off.</p>
      {message && (
        <p className={cn("mt-2 text-[10.5px] leading-snug", failed ? "text-fail" : "text-pass")}>{message}</p>
      )}
      <button
        type="submit"
        disabled={!repoRoot.trim() || register.isPending}
        className="mt-2 w-full rounded-md bg-ink px-2 py-1.5 text-[11px] font-semibold text-surface disabled:cursor-not-allowed disabled:opacity-50"
      >
        {register.isPending ? "Registering…" : "Register project"}
      </button>
    </form>
  );
}

function ProjectRailItem({
  project: p,
  active,
  pathname,
}: {
  project: ProjectSummary;
  active: boolean;
  pathname: string;
}) {
  return (
    <div className="mb-0.5">
      <Link
        to="/p/$projectId"
        params={{ projectId: p.id }}
        className={cn(
          "flex items-center gap-2.5 rounded-lg px-2.5 py-2 transition-colors hover:bg-hover",
          active && "bg-hover",
        )}
      >
        <span className="flex-none text-faint">
          {active ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </span>
        <span
          className={cn(
            "min-w-0 flex-1 truncate text-[13px]",
            active ? "font-semibold text-ink" : "font-medium text-ink-soft",
          )}
        >
          {p.name}
        </span>
        {p.needsCount > 0 ? (
          <CountBadge count={p.needsCount} tone="fail" className="h-[17px] min-w-[17px] text-[10px]" />
        ) : p.activeRuns > 0 && p.worstLiveness ? (
          <Dot tone={p.worstLiveness === "dead" ? "fail" : p.worstLiveness === "stale" ? "warn" : "pass"} pulse={p.worstLiveness === "fresh"} size={7} />
        ) : null}
      </Link>

      {active && (
        <div className="ml-[18px] mt-0.5 mb-2 flex flex-col gap-px border-l border-line pl-2">
          {SUBSECTIONS.map((s) => {
            const to = s.to.replace("$projectId", p.id);
            const isActive =
              s.key === "overview" ? pathname === to : pathname.startsWith(to);
            const badge =
              s.key === "needs" && p.needsCount > 0 ? p.needsCount : undefined;
            return (
              <Link
                key={s.key}
                to={s.to}
                params={{ projectId: p.id }}
                className={cn(
                  "flex items-center justify-between gap-2 rounded-md px-2.5 py-1.5 text-[12.5px] transition-colors hover:bg-hover",
                  isActive ? "bg-hover font-semibold text-ink" : "font-medium text-muted",
                )}
              >
                <span>{s.label}</span>
                {badge !== undefined && (
                  <Mono className="rounded-full bg-fail-soft px-1.5 py-px text-[10px] font-semibold text-fail">
                    {badge}
                  </Mono>
                )}
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}

function ThemeToggle() {
  const { pref, cycle } = useTheme();
  const Icon = pref === "light" ? Sun : pref === "dark" ? Moon : Monitor;
  return (
    <button
      onClick={cycle}
      title={`Theme: ${pref} (click to change)`}
      className="flex h-7 w-7 items-center justify-center rounded-md text-faint transition-colors hover:bg-hover hover:text-ink-soft"
    >
      <Icon size={14} />
    </button>
  );
}
