/*
  App Settings (route "/settings") — application-wide configuration that applies
  across every project. Four tabs: General (appearance / defaults / daemon),
  Runner profiles, Permissions, and Notifications. Projects override individual
  values under their own Details; provenance chips on each row say where a value
  comes from and therefore whether teammates see it.

  Section bodies live under ./app/*. The Theme control is wired to the real
  ThemeProvider; other controls hold working local state with // TODO(api) marks
  where the settings API must read/persist.
*/

import { useState } from "react";
import { cn } from "@/lib/cn";
import { GeneralSection } from "./app/GeneralSection";
import { ProfilesSection } from "./app/ProfilesSection";
import { PermissionsSection } from "./app/PermissionsSection";
import { NotificationsSection } from "./app/NotificationsSection";

type AppTab = "general" | "profiles" | "permissions" | "notifications";

const TABS: { key: AppTab; label: string }[] = [
  { key: "general", label: "General" },
  { key: "profiles", label: "Runner profiles" },
  { key: "permissions", label: "Permissions" },
  { key: "notifications", label: "Notifications" },
];

function SectionTabs({ value, onChange }: { value: AppTab; onChange: (t: AppTab) => void }) {
  return (
    <div className="mb-[26px] inline-flex overflow-hidden rounded-lg border border-line bg-surface">
      {TABS.map((t, i) => {
        const active = t.key === value;
        return (
          <button
            key={t.key}
            type="button"
            aria-current={active ? "page" : undefined}
            onClick={() => onChange(t.key)}
            className={cn(
              "px-[14px] py-[7px] text-[12.5px] transition-colors",
              i > 0 && "border-l border-line-soft",
              active
                ? "bg-ink font-semibold text-surface"
                : "font-medium text-muted hover:bg-hover hover:text-ink-soft",
            )}
          >
            {t.label}
          </button>
        );
      })}
    </div>
  );
}

export function AppSettings() {
  const [tab, setTab] = useState<AppTab>("general");

  return (
    <div className="tk-scroll h-full overflow-y-auto">
      <div className="mx-auto max-w-[820px] px-11 pb-20 pt-[30px]">
        <h1 className="font-serif text-[30px] font-semibold tracking-[-0.02em] text-ink">Settings</h1>
        <p className="mb-[18px] mt-1 text-[13.5px] text-muted">
          Applies across all projects. Each value shows its source; projects can override under their
          own Details.
        </p>

        <SectionTabs value={tab} onChange={setTab} />

        {tab === "general" && <GeneralSection />}
        {tab === "profiles" && <ProfilesSection />}
        {tab === "permissions" && <PermissionsSection />}
        {tab === "notifications" && <NotificationsSection />}
      </div>
    </div>
  );
}
