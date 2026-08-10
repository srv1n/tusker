/*
  Permissions tab — the screen where the operator decides how much rope every
  agent gets. Three presets as a radio with one-line consequences, an editable
  denylist (built-ins visible + non-deletable, operator entries append below),
  and a network-access toggle for the workspace-only preset.
*/

import { cn } from "@/lib/cn";
import { X } from "lucide-react";
import { Chip } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import { SettingRow, SettingsCard } from "./parts";
import {
  initialDenylist,
  permissionPresets,
  type DenylistEntry,
  type PermissionPreset,
} from "./mock";

function PresetRadio({
  preset,
  selected,
  onSelect,
}: {
  preset: PermissionPreset;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      disabled
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "flex w-full items-start gap-3 rounded-[10px] border-[1.5px] p-4 text-left transition-colors",
        selected ? "border-info bg-info-soft" : "border-line bg-surface hover:bg-hover",
      )}
    >
      <span
        className={cn(
          "mt-[1px] flex h-4 w-4 flex-none items-center justify-center rounded-full border-[1.5px] bg-surface",
          selected ? "border-info" : "border-fainter",
        )}
      >
        {selected && <span className="h-2 w-2 rounded-full bg-info" />}
      </span>
      <span className="flex flex-col gap-[3px]">
        <span className="text-[14px] font-semibold text-ink">{preset.label}</span>
        <span className="text-[12.5px] leading-[1.45] text-muted">{preset.desc}</span>
      </span>
    </button>
  );
}

function DenyRow({ entry, onRemove }: { entry: DenylistEntry; onRemove?: () => void }) {
  return (
    <div className="flex items-center gap-3 px-4 py-[11px]">
      <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-ink-soft">{entry.pattern}</span>
      <Chip
        tone={entry.builtin ? "neutral" : "info"}
        variant="soft"
        mono
        className="rounded px-[7px] py-[2px] text-[9px] font-semibold tracking-normal"
      >
        {entry.builtin ? "built-in" : "custom"}
      </Chip>
      {onRemove && (
        <button
          type="button"
          aria-label={`Remove ${entry.pattern}`}
          onClick={onRemove}
          className="flex-none text-fainter transition-colors hover:text-fail"
        >
          <X size={14} strokeWidth={2.25} />
        </button>
      )}
    </div>
  );
}

export function PermissionsSection() {
  return (
    <div className="animate-rise">
      <p className="mb-4 max-w-[64ch] text-[13px] leading-relaxed text-muted">
        How much rope every agent gets. Applies to any profile that doesn’t set its own. <strong className="text-warn">These controls are read-only until durable persistence is available.</strong>
      </p>

      {/* Presets */}
      <div role="radiogroup" aria-label="Permission preset" className="mb-[26px] flex flex-col gap-2.5">
        {permissionPresets.map((p) => (
          <PresetRadio
            key={p.key}
            preset={p}
            selected={p.key === "full"}
            onSelect={() => undefined}
          />
        ))}
      </div>

      {/* Denylist */}
      <SectionLabel className="mb-[10px]">
        Denylist <span className="text-fainter">· blocks these under guarded access</span>
      </SectionLabel>
      <SettingsCard className="mb-3">
        {initialDenylist.map((d, i) => (
          <DenyRow
            key={`${d.pattern}-${i}`}
            entry={d}
            onRemove={undefined}
          />
        ))}
        <div className="bg-panel px-4 py-[10px] text-[11px] text-faint">Custom denylist entries · coming soon (not persisted)</div>
      </SettingsCard>

      {/* Network access — the workspace-only escape hatch */}
      <SettingsCard>
        <SettingRow
          label="Network access"
          description="Under workspace-only — lets agents fetch docs and search the web. Persistence is not available yet."
          locked
          control={<span className="font-mono text-[11.5px] text-muted">Enabled · coming soon</span>}
        />
      </SettingsCard>
    </div>
  );
}
