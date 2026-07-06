/*
  Permissions tab — the screen where the operator decides how much rope every
  agent gets. Three presets as a radio with one-line consequences, an editable
  denylist (built-ins visible + non-deletable, operator entries append below),
  and a network-access toggle for the workspace-only preset.
*/

import { useState } from "react";
import { X } from "lucide-react";
import { cn } from "@/lib/cn";
import { Chip } from "@/components/ui/primitives";
import { Toggle } from "@/components/ui/controls";
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
  const [selected, setSelected] = useState<PermissionPreset["key"]>("full"); // TODO(api): persist
  const [denylist, setDenylist] = useState<DenylistEntry[]>(initialDenylist); // TODO(api): persist operator entries
  const [draft, setDraft] = useState("");
  const [networkOn, setNetworkOn] = useState(true); // TODO(api): persist

  const addPattern = () => {
    const pattern = draft.trim();
    if (!pattern) return;
    setDenylist((prev) => [...prev, { pattern, builtin: false }]);
    setDraft("");
  };

  return (
    <div className="animate-rise">
      <p className="mb-4 max-w-[64ch] text-[13px] leading-relaxed text-muted">
        How much rope every agent gets. Applies to any profile that doesn’t set its own.
      </p>

      {/* Presets */}
      <div role="radiogroup" aria-label="Permission preset" className="mb-[26px] flex flex-col gap-2.5">
        {permissionPresets.map((p) => (
          <PresetRadio
            key={p.key}
            preset={p}
            selected={selected === p.key}
            onSelect={() => setSelected(p.key)}
          />
        ))}
      </div>

      {/* Denylist */}
      <SectionLabel className="mb-[10px]">
        Denylist <span className="text-fainter">· blocks these under guarded access</span>
      </SectionLabel>
      <SettingsCard className="mb-3">
        {denylist.map((d, i) => (
          <DenyRow
            key={`${d.pattern}-${i}`}
            entry={d}
            onRemove={d.builtin ? undefined : () => setDenylist((prev) => prev.filter((_, j) => j !== i))}
          />
        ))}
        <div className="bg-panel px-4 py-[10px]">
          <input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addPattern();
              }
            }}
            placeholder="+ add a pattern…"
            className="w-full bg-transparent font-mono text-[12px] text-ink-soft outline-none placeholder:text-faint"
          />
        </div>
      </SettingsCard>

      {/* Network access — the workspace-only escape hatch */}
      <SettingsCard>
        <SettingRow
          label="Network access"
          description="Under workspace-only — lets agents fetch docs and search the web."
          control={<Toggle checked={networkOn} onChange={setNetworkOn} />}
        />
      </SettingsCard>
    </div>
  );
}
