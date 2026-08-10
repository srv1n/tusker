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
import { useServeCapabilities } from "@/lib/queries";

type AppTab = "general" | "profiles" | "permissions" | "notifications";

const TABS: { key: AppTab; label: string }[] = [
  { key: "general", label: "General" },
  { key: "profiles", label: "Runner profiles" },
  { key: "permissions", label: "Permissions" },
  { key: "notifications", label: "Notifications" },
];

function SectionTabs({ value, onChange }: { value: AppTab; onChange: (t: AppTab) => void }) {
  return (
    <div className="mb-[26px] max-w-full overflow-x-auto overflow-y-hidden tk-scroll">
      <div className="inline-flex overflow-hidden rounded-lg border border-line bg-surface">
        {TABS.map((t, i) => {
          const active = t.key === value;
          return (
            <button
              key={t.key}
              type="button"
              aria-current={active ? "page" : undefined}
              onClick={() => onChange(t.key)}
              className={cn(
                "whitespace-nowrap px-[14px] py-[7px] text-[12.5px] transition-colors",
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
        <h1 className="font-serif text-[30px] font-semibold tracking-[-0.02em] text-ink">Settings</h1>
        <p className="mb-[18px] mt-1 text-[13.5px] text-muted">
          Applies across all projects. Each value shows its source; projects can override under their
          own Details.
        </p>

        <SectionTabs value={tab} onChange={setTab} />
        {unavailable && tab === "profiles" && (
          <p role="status" className="mb-4 rounded-lg border border-warn/30 bg-warn-soft px-3 py-2 text-[12px] text-warn">
            Runner profiles are reference-only in this Serve version. {unavailable.description}
          </p>
        )}

        {tab === "general" && <GeneralSection />}
        {tab === "profiles" && <ProfilesSection />}
        {tab === "permissions" && <PermissionsSection />}
        {tab === "notifications" && <NotificationsSection />}
      </div>
    </div>
  );
}
