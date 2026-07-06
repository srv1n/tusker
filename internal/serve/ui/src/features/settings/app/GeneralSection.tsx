/*
  General tab — Appearance, Defaults, Daemon.

  The Theme control is wired for real to the app ThemeProvider (useTheme), so the
  whole UI flips light / dark / system live. Density + the Defaults dropdowns hold
  local state (UI works today; // TODO(api) marks where persistence lands). The
  Daemon group is read-only/derived: `Port` comes from the live daemon status,
  the rest are locked machine/derived values.
*/

import { useState } from "react";
import type { ThemePref } from "@/lib/theme";
import { useTheme } from "@/lib/theme";
import { useDaemon } from "@/lib/queries";
import { SegmentedControl } from "@/components/ui/controls";
import type { SegmentOption } from "@/components/ui/controls";
import { SectionLabel } from "@/components/ui/page";
import { Dot, Mono } from "@/components/ui/primitives";
import { Skeleton } from "@/components/ui/states";
import { SelectPill, SettingRow, SettingsCard } from "./parts";
import { daemonRows, defaultRows, densityOptions, type Density } from "./mock";

const themeOptions: SegmentOption<ThemePref>[] = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
];

const densitySegments: SegmentOption<Density>[] = densityOptions.map((d) => ({ value: d, label: d }));

export function GeneralSection() {
  const { pref, setPref } = useTheme();
  const [density, setDensity] = useState<Density>("Comfortable"); // TODO(api): persist to global config
  const [defaults, setDefaults] = useState<Record<string, string>>(() =>
    Object.fromEntries(defaultRows.map((r) => [r.key, r.value])),
  );
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
          control={
            <SegmentedControl<Density>
              size="sm"
              options={densitySegments}
              value={density}
              onChange={setDensity}
            />
          }
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
            control={
              <SelectPill
                ariaLabel={r.key}
                value={defaults[r.key]}
                options={r.options}
                onChange={(v) => setDefaults((prev) => ({ ...prev, [r.key]: v }))}
              />
            }
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
