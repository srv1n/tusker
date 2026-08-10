/*
  General tab — Appearance, Defaults, Daemon.

  The Theme control is wired for real to the app ThemeProvider (useTheme), so the
  whole UI flips light / dark / system live. Density + the Defaults dropdowns hold
  local state (UI works today; // TODO(api) marks where persistence lands). The
  Daemon group is read-only/derived: `Port` comes from the live daemon status,
  the rest are locked machine/derived values.
*/

import type { ThemePref } from "@/lib/theme";
import { useTheme } from "@/lib/theme";
import { useDaemon } from "@/lib/queries";
import { SegmentedControl } from "@/components/ui/controls";
import type { SegmentOption } from "@/components/ui/controls";
import { SectionLabel } from "@/components/ui/page";
import { Dot, Mono } from "@/components/ui/primitives";
import { Skeleton } from "@/components/ui/states";
import { SettingRow, SettingsCard } from "./parts";
import { daemonRows, defaultRows } from "./mock";

const themeOptions: SegmentOption<ThemePref>[] = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
];

export function GeneralSection() {
  const { pref, setPref } = useTheme();
  const daemonQ = useDaemon();
  const livePort = daemonQ.data?.addr.split(":").pop();
  const connected = !!daemonQ.data?.connected;

  return (
    <div className="animate-rise">
      {/* Appearance */}
      <SectionLabel className="mb-[10px]">Appearance</SectionLabel>
      <SettingsCard className="mb-[26px]">
        <SettingRow
          label="Theme"
          source="global"
          control={
            <SegmentedControl<ThemePref>
              size="sm"
              options={themeOptions}
              value={pref}
              onChange={setPref}
            />
          }
        />
        <SettingRow
          label="Density"
          source="global"
          locked
          description="Persistence is not available yet."
          control={<span className="font-mono text-[11.5px] text-muted">Comfortable · coming soon</span>}
        />
      </SettingsCard>

      {/* Defaults */}
      <SectionLabel className="mb-[10px]">Defaults</SectionLabel>
      <SettingsCard className="mb-[26px]">
        {defaultRows.map((r) => (
          <SettingRow
            key={r.key}
            label={r.key}
            source={r.source}
            locked
            description="Persistence is not available yet."
            control={<span className="font-mono text-[11.5px] text-muted">{r.value} · coming soon</span>}
          />
        ))}
      </SettingsCard>

      {/* Daemon — read-only / machine-derived */}
      <SectionLabel className="mb-[10px]">Daemon</SectionLabel>
      <SettingsCard>
        <SettingRow
          label="Port"
          source="local"
          locked
          control={
            daemonQ.isLoading ? (
              <Skeleton className="h-[15px] w-10" />
            ) : (
              <span className="inline-flex items-center gap-1.5">
                <Dot tone={connected ? "pass" : "neutral"} pulse={connected} />
                <Mono className="text-[11.5px] text-muted">{livePort ?? "7420"}</Mono>
              </span>
            )
          }
        />
        {daemonRows.map((r) => (
          <SettingRow
            key={r.key}
            label={r.key}
            source={r.source}
            locked
            control={<Mono className="text-[11.5px] text-muted">{r.value}</Mono>}
          />
        ))}
      </SettingsCard>
    </div>
  );
}
