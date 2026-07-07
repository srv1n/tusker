import { Link, useParams, useRouterState } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, Monitor, Moon, Search, Sun } from "lucide-react";
import { cn } from "@/lib/cn";
import { CountBadge, Dot, Mono } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import { useDaemon, useNeeds, useProjects } from "@/lib/queries";
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
  { key: "docs", label: "Library", to: "/p/$projectId/docs" as const },
];

export function Sidebar() {
  const projects = useProjects();
  const daemon = useDaemon();
  const globalNeeds = useNeeds();
  const activeProject = useParams({ strict: false }).projectId as string | undefined;
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const needsCount = globalNeeds.data?.length ?? 0;
  const budgetCircuitOpen = daemon.data?.budgetCircuit?.open === true;
  const invariantCircuitOpen = daemon.data?.invariantCircuit?.open === true;

  return (
    <aside className="flex w-[246px] flex-none flex-col border-r border-line bg-panel py-4">
      {/* Brand */}
      <div className="flex items-center gap-2.5 px-[18px] pb-4">
        <span className="flex h-[22px] w-[22px] items-center justify-center rounded-md bg-ink font-serif text-[13px] font-semibold text-surface">
          t
        </span>
        <span className="font-serif text-[17px] font-semibold tracking-[-0.01em] text-ink">tusker</span>
        <SectionLabel className="mt-[3px] tracking-[0.18em]">serve</SectionLabel>
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
        <Mono className="text-[10.5px] text-fainter">{projects.data?.length ?? 0}</Mono>
      </div>

      <div className="tk-scroll flex-1 overflow-y-auto px-3 pb-2">
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
            <span className={daemon.data?.connected ? "text-pass" : "text-fail"}>
              {daemon.data?.connected ? "● live" : "● down"}
            </span>
          </div>
          <div>{daemon.data?.addr ?? "localhost:7420"}</div>
        </div>
        <ThemeToggle />
      </div>
    </aside>
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
