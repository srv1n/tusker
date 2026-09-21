/*
  App Settings (route "/settings") — application-wide configuration that applies
  across every project. Four tabs: General (appearance / defaults / daemon),
  Agents and Notifications. Projects override individual
  values under their own Details; provenance chips on each row say where a value
  comes from and therefore whether teammates see it.

  Section bodies live under ./app/*. The Theme control is wired to the real
  ThemeProvider; other controls hold working local state with // TODO(api) marks
  where the settings API must read/persist.
*/

import { useState } from "react";
import { cn } from "@/lib/cn";
import { GeneralSection } from "./app/GeneralSection";
import { AgentsSection } from "./app/AgentsSection";
import { NotificationsSection } from "./app/NotificationsSection";
import { useProjectVisibility, useProjects, useServeCapabilities } from "@/lib/queries";
import { SectionLabel } from "@/components/ui/page";
import { SettingsCard, SettingRow } from "./app/parts";

type AppTab = "general" | "projects" | "agents" | "notifications";

const TABS: { key: AppTab; label: string }[] = [
  { key: "general", label: "General" },
  { key: "projects", label: "All Projects" },
  { key: "agents", label: "Agents" },
  { key: "notifications", label: "Notifications" },
];

function SectionTabs({ value, onChange }: { value: AppTab; onChange: (t: AppTab) => void }) {
  return (
    <div className="mb-[26px] max-w-full overflow-x-auto overflow-y-hidden tk-scroll">
      <div className="inline-flex rounded-lg border border-line bg-panel p-0.5">
        {TABS.map((t) => {
          const active = t.key === value;
          return (
            <button
              key={t.key}
              type="button"
              aria-current={active ? "page" : undefined}
              onClick={() => onChange(t.key)}
              className={cn(
                "whitespace-nowrap rounded-md px-[14px] py-[7px] text-[12.5px] transition-colors",
                active
                  ? "bg-raised font-semibold text-ink shadow-2xs"
                  : "font-medium text-muted hover:text-ink-soft",
              )}
            >
              {t.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}

export function AppSettings() {
  const [tab, setTab] = useState<AppTab>("general");
  const capabilities = useServeCapabilities();
  const unavailable = capabilities.data?.capabilities.find((c) => c.id === "profiles" && c.class === "unavailable");

  return (
    <div className="tk-scroll h-full overflow-y-auto">
      <div className="mx-auto max-w-[820px] px-4 pb-20 pt-[30px] sm:px-11">
        <h1 className="mb-[18px] font-serif text-[30px] font-semibold tracking-[-0.02em] text-ink">Settings</h1>

        <SectionTabs value={tab} onChange={setTab} />
        {unavailable && tab === "agents" && (
          <p role="status" className="mb-4 rounded-lg border border-warn/30 bg-warn-soft px-3 py-2 text-[12px] text-warn">
            Agents are reference-only in this Serve version. {unavailable.description}
          </p>
        )}

        {tab === "general" && <GeneralSection />}
        {tab === "projects" && <ProjectsSection />}
        {tab === "agents" && <AgentsSection />}
        {tab === "notifications" && <NotificationsSection />}
      </div>
    </div>
  );
}

function ProjectsSection() {
  const projects = useProjects();
  const visibility = useProjectVisibility();
  return (
    <div className="animate-rise">
      <SectionLabel className="mb-[10px]">Main screen</SectionLabel>
      <p className="mb-3 text-[12px] text-muted">Tusker discovers projects when you run <code>tusker init</code>. Choose which ones appear in the project strip.</p>
      <SettingsCard>
		{(projects.data ?? []).map((project) => (
          <SettingRow
            key={project.id}
            label={project.name}
            description={<><span className="break-all">{project.repoRoot}</span>{project.lastError ? <span className="mt-1 block text-fail">{project.lastError}</span> : null}</>}
            control={<input type="checkbox" checked={project.visible !== false} disabled={visibility.isPending} onChange={(event) => visibility.mutate({ projectId: project.id, visible: event.currentTarget.checked })} aria-label={`Show ${project.name} on main screen`} className="h-4 w-4 accent-current" />}
          />
        ))}
        {!projects.isLoading && (projects.data?.length ?? 0) === 0 && <div className="px-4 py-6 text-center text-[12px] text-muted">No initialized projects yet.</div>}
      </SettingsCard>
    </div>
  );
}
