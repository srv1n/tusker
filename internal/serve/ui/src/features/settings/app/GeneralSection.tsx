/*
  General tab — Appearance, Defaults, Daemon.

  The Theme control is wired for real to the app ThemeProvider (useTheme), so the
  whole UI flips light / dark / system live. Density + the Defaults dropdowns hold
  local state (UI works today; // TODO(api) marks where persistence lands). The
  Daemon group is read-only/derived: `Port` comes from the live daemon status,
  the rest are locked machine/derived values.
*/

import { useEffect, useState } from "react";
import type { ThemePref } from "@/lib/theme";
import { FONT_FAMILY_OPTIONS, useFontScale, type FontFamily, type FontScale } from "@/lib/font-scale";
import { useTheme } from "@/lib/theme";
import { useDaemon, useDaemonAction } from "@/lib/queries";
import { Button, SegmentedControl, TextInput } from "@/components/ui/controls";
import { ActionResultLine } from "@/components/ui/action-feedback";
import type { SegmentOption } from "@/components/ui/controls";
import { SectionLabel } from "@/components/ui/page";
import { Dot, Mono } from "@/components/ui/primitives";
import { Skeleton } from "@/components/ui/states";
import { SelectPill, SettingRow, SettingsCard } from "./parts";
import { daemonRows, defaultRows } from "./mock";

const themeOptions: SegmentOption<ThemePref>[] = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
];

const fontScaleOptions: SegmentOption<FontScale>[] = [
  { value: "small", label: <>A<sup>−</sup><span className="sr-only"> Small</span></> },
  { value: "default", label: <>A<span className="sr-only"> Default</span></> },
  { value: "large", label: <>A<sup>+</sup><span className="sr-only"> Large</span></> },
];

export function GeneralSection() {
  const { pref, setPref } = useTheme();
  const { scale, setScale, family, setFamily } = useFontScale();
  const daemonQ = useDaemon();
  const daemonAction = useDaemonAction();
  const livePort = daemonQ.data?.addr.split(":").pop();
  const connected = !!daemonQ.data?.connected;
  const [globalLimit, setGlobalLimit] = useState("");
  useEffect(() => {
    if (daemonQ.data?.maxActiveRuns) setGlobalLimit(String(daemonQ.data.maxActiveRuns));
  }, [daemonQ.data?.maxActiveRuns]);
  const parsedGlobalLimit = Number(globalLimit);
  const validGlobalLimit = Number.isInteger(parsedGlobalLimit) && parsedGlobalLimit > 0;

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
          label="Font"
          source="local"
          control={<SelectPill value={family} options={FONT_FAMILY_OPTIONS} onChange={(value) => setFamily(value as FontFamily)} ariaLabel="Interface font" />}
        />
        <SettingRow
          label="Text size"
          source="local"
          control={<SegmentedControl<FontScale> size="sm" options={fontScaleOptions} value={scale} onChange={setScale} />}
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
            control={<span className="font-mono text-[11.5px] text-muted">{r.value} · coming soon</span>}
          />
        ))}
      </SettingsCard>

      {/* Daemon */}
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
        <SettingRow
          label="Global concurrency"
          description="Maximum tasks running across every project. Project and wave limits may be lower."
          source="global"
          control={
            <div className="flex items-center gap-2">
              <TextInput aria-label="Global concurrent tasks" inputMode="numeric" value={globalLimit} onChange={(event) => setGlobalLimit(event.target.value)} className="w-20 font-mono" />
              <Button size="sm" disabled={!validGlobalLimit || daemonAction.isPending} onClick={() => daemonAction.mutate({ action: "limits", body: { maxActiveRuns: parsedGlobalLimit } })}>
                {daemonAction.isPending ? "Saving…" : "Save"}
              </Button>
            </div>
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
      <ActionResultLine className="mt-2" pending={daemonAction.isPending} error={daemonAction.error} result={daemonAction.data} />
    </div>
  );
}
