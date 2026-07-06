/*
  Project Settings — screen-local presentational parts + the override write-path.

  These live here (not in components/ui) because they encode settings-specific
  behavior: the provenance chip, the "reset to inherited" affordance, and the
  rule that an edit becomes a `local` override rather than touching committed
  config. The primitives they build on (Toggle, Select, SegmentedControl, Mono,
  Dot) are all shared.
*/

import { useCallback, useState, type ReactNode } from "react";
import { cn } from "@/lib/cn";
import { Dot, Mono } from "@/components/ui/primitives";
import { SegmentedControl, Select, Toggle } from "@/components/ui/controls";
import { sourceMeta } from "./mock";
import type {
  RoutingRule,
  SettingRowData,
  SettingSource,
  SettingsTab,
  WorkspaceScript,
  WorktreeInfo,
} from "./mock";

// ----------------------------------------------------------------------------
// Override write-path — edits become `local`, reset restores inherited.
// ----------------------------------------------------------------------------

export function useConfigRows(initial: SettingRowData[]) {
  const [rows, setRows] = useState<SettingRowData[]>(initial);

  const setValue = useCallback((key: string, value: string | boolean) => {
    setRows((rs) =>
      rs.map((r) => {
        if (r.key !== key) return r;
        // Editing never rewrites committed config: matching the inherited value
        // clears the override, anything else is stored as a machine-local one.
        const overridden = value !== r.inherited.value;
        return {
          ...r,
          value,
          overridden,
          source: overridden ? ("local" as SettingSource) : r.inherited.source,
        };
      }),
    );
  }, []);

  const reset = useCallback((key: string) => {
    setRows((rs) =>
      rs.map((r) =>
        r.key === key
          ? { ...r, value: r.inherited.value, source: r.inherited.source, overridden: false }
          : r,
      ),
    );
  }, []);

  return { rows, setValue, reset };
}

// ----------------------------------------------------------------------------
// Chips & affordances
// ----------------------------------------------------------------------------

export function SourceChip({ source }: { source: SettingSource }) {
  const m = sourceMeta[source];
  return (
    <span
      title={m.hint}
      className={cn(
        "flex-none rounded px-[7px] py-[2px] font-mono text-[9px] font-semibold uppercase tracking-wide",
        m.cls,
      )}
    >
      {m.label}
    </span>
  );
}

export function ReadOnlyTag() {
  return (
    <span
      title="Derived — set outside committed config; not editable here."
      className="flex-none rounded bg-hover px-[7px] py-[2px] font-mono text-[9px] font-semibold uppercase tracking-wide text-faint"
    >
      read-only
    </span>
  );
}

export function ResetButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      title="Reset to inherited value"
      className="flex-none font-mono text-[11px] text-fainter transition-colors hover:text-ink"
    >
      ↺ reset
    </button>
  );
}

function ProfileChip({ name }: { name: string }) {
  return (
    <span className="flex-none rounded-[5px] bg-info-soft px-[9px] py-[3px] font-mono text-[11px] font-semibold text-info">
      {name}
    </span>
  );
}

// ----------------------------------------------------------------------------
// Layout primitives
// ----------------------------------------------------------------------------

/** Bordered list card; rows separated by hairlines. */
export function SectionCard({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        "divide-y divide-line-soft overflow-hidden rounded-[10px] border border-line",
        className,
      )}
    >
      {children}
    </div>
  );
}

/** Muted intro paragraph above a section (design: 13px, ~64ch). */
export function IntroText({ children }: { children: ReactNode }) {
  return <p className="mb-4 max-w-[64ch] text-[13px] leading-relaxed text-muted">{children}</p>;
}

/** Read-only callout: a "read-only" tag + one line (e.g. the port range). */
export function ReadOnlyNote({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-2.5 rounded-[9px] border border-line bg-panel px-3.5 py-3">
      <ReadOnlyTag />
      <span className="text-[12.5px] text-muted">{children}</span>
    </div>
  );
}

/** Quiet footnote with a leading dot (design: overlap / fallthrough notes). */
export function HintNote({ children }: { children: ReactNode }) {
  return (
    <div className="mt-4 flex gap-2.5 rounded-[9px] border border-line bg-panel px-3.5 py-3">
      <span className="mt-[6px] h-1.5 w-1.5 flex-none rounded-full bg-fainter" />
      <span className="text-[12.5px] leading-relaxed text-muted">{children}</span>
    </div>
  );
}

// ----------------------------------------------------------------------------
// Tabs
// ----------------------------------------------------------------------------

export function SettingsTabs({
  tabs,
  active,
  onChange,
}: {
  tabs: { key: SettingsTab; label: string }[];
  active: SettingsTab;
  onChange: (key: SettingsTab) => void;
}) {
  return (
    <div className="inline-flex overflow-hidden rounded-lg border border-line bg-raised">
      {tabs.map((t, i) => {
        const on = t.key === active;
        return (
          <button
            key={t.key}
            onClick={() => onChange(t.key)}
            aria-current={on ? "page" : undefined}
            className={cn(
              "px-3.5 py-[7px] text-[12.5px] transition-colors",
              i > 0 && "border-l border-line-soft",
              on
                ? "bg-hover font-semibold text-ink"
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

// ----------------------------------------------------------------------------
// Setting row (provenance + inline control)
// ----------------------------------------------------------------------------

function Control({
  row,
  onChange,
}: {
  row: SettingRowData;
  onChange: (value: string | boolean) => void;
}) {
  const c = row.control;
  if (c.kind === "readonly") {
    return <Mono className="text-[11.5px] text-ink-soft">{String(row.value)}</Mono>;
  }
  if (c.kind === "toggle") {
    return <Toggle checked={row.value as boolean} onChange={onChange} />;
  }
  if (c.kind === "select") {
    return (
      <Select value={String(row.value)} onChange={(e) => onChange(e.target.value)}>
        {c.options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </Select>
    );
  }
  return (
    <SegmentedControl
      size="sm"
      value={String(row.value)}
      onChange={onChange}
      options={c.options.map((o) => ({ value: o.value, label: o.label }))}
    />
  );
}

export function SettingRow({
  row,
  onChange,
  onReset,
}: {
  row: SettingRowData;
  onChange: (key: string, value: string | boolean) => void;
  onReset: (key: string) => void;
}) {
  const readonly = row.control.kind === "readonly";
  return (
    <div className="flex items-center justify-between gap-4 px-4 py-3">
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-[13px] text-ink-soft">{row.label}</span>
        {row.desc && (
          <span className="max-w-[46ch] text-[11.5px] leading-snug text-faint">{row.desc}</span>
        )}
      </div>
      <div className="flex flex-none items-center gap-2.5">
        {row.overridden && <ResetButton onClick={() => onReset(row.key)} />}
        {readonly ? <ReadOnlyTag /> : <SourceChip source={row.source} />}
        <Control row={row} onChange={(v) => onChange(row.key, v)} />
      </div>
    </div>
  );
}

/** Renders a config section from the override hook in one call. */
export function SettingList({
  rows,
  onChange,
  onReset,
}: {
  rows: SettingRowData[];
  onChange: (key: string, value: string | boolean) => void;
  onReset: (key: string) => void;
}) {
  return (
    <SectionCard>
      {rows.map((r) => (
        <SettingRow key={r.key} row={r} onChange={onChange} onReset={onReset} />
      ))}
    </SectionCard>
  );
}

// ----------------------------------------------------------------------------
// Worktrees (live, read-only)
// ----------------------------------------------------------------------------

export function WorktreeList({ worktrees }: { worktrees: WorktreeInfo[] }) {
  if (worktrees.length === 0) {
    return (
      <SectionCard>
        <div className="px-4 py-6 text-center text-[12.5px] text-faint">No active worktrees.</div>
      </SectionCard>
    );
  }
  return (
    <SectionCard>
      {worktrees.map((w) => (
        <div key={w.task} className="flex items-center gap-3 px-4 py-2.5">
          <Mono className="w-24 flex-none text-[11px] text-faint">{w.task}</Mono>
          <Mono className="min-w-0 flex-1 truncate text-[11.5px] text-ink-soft">{w.path}</Mono>
          <Mono className="flex-none text-[10.5px] text-pass">{w.lease}</Mono>
        </div>
      ))}
    </SectionCard>
  );
}

// ----------------------------------------------------------------------------
// Routing rules (ordered, first match wins)
// ----------------------------------------------------------------------------

export function RoutingList({
  initial,
  fallthrough,
}: {
  initial: RoutingRule[];
  fallthrough: string;
}) {
  const [rules, setRules] = useState<RoutingRule[]>(initial);

  const add = () =>
    setRules((rs) => [...rs, { id: `rr-${rs.length + 1}-${Date.now()}`, match: "risk = medium", profile: "default" }]);
  const remove = (id: string) => setRules((rs) => rs.filter((r) => r.id !== id));

  return (
    <>
      <SectionCard>
        {rules.map((r, i) => (
          <div key={r.id} className="group flex items-center gap-3 px-4 py-3">
            <span
              className="flex-none cursor-grab select-none text-fainter"
              title="Drag to reorder"
              aria-hidden
            >
              ⋮⋮
            </span>
            <Mono className="w-3.5 flex-none text-right text-[10px] text-fainter">{i + 1}</Mono>
            <Mono className="min-w-0 flex-1 truncate text-[12px] text-ink-soft">{r.match}</Mono>
            <span className="flex-none text-fainter" aria-hidden>
              →
            </span>
            <ProfileChip name={r.profile} />
            <button
              onClick={() => remove(r.id)}
              title="Remove rule"
              className="flex-none text-fainter opacity-0 transition-opacity hover:text-fail group-hover:opacity-100"
            >
              ✕
            </button>
          </div>
        ))}
        <div className="flex items-center gap-3 bg-panel px-4 py-3">
          <span className="w-3.5 flex-none text-center font-mono text-[10px] text-fainter" aria-hidden>
            ·
          </span>
          <Mono className="text-[11.5px] text-faint">fallthrough — {fallthrough}</Mono>
        </div>
      </SectionCard>
      <button
        onClick={add}
        className="mt-3 rounded-lg border border-dashed border-line px-3.5 py-2 text-[12.5px] text-muted transition-colors hover:border-fainter hover:text-ink-soft"
      >
        + Add rule
      </button>
    </>
  );
}

// ----------------------------------------------------------------------------
// Workspace lifecycle scripts (editable code fields)
// ----------------------------------------------------------------------------

export function ScriptField({ script }: { script: WorkspaceScript }) {
  const [value, setValue] = useState(script.value);
  return (
    <div className="mb-5">
      <div className="text-[13.5px] font-semibold text-ink">{script.label}</div>
      <div className="mb-2 mt-0.5 max-w-[64ch] text-[12px] leading-relaxed text-faint">
        {script.desc}
      </div>
      <textarea
        value={value}
        onChange={(e) => setValue(e.target.value)}
        spellCheck={false}
        rows={Math.max(2, value.split("\n").length)}
        className="tk-scroll block w-full resize-y rounded-[9px] border border-line bg-panel px-3.5 py-3 font-mono text-[12px] leading-relaxed text-ink-soft focus-visible:border-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/20"
      />
    </div>
  );
}

// ----------------------------------------------------------------------------
// Section header with an optional right-aligned live indicator
// ----------------------------------------------------------------------------

export function LiveMeta({
  connected,
  label,
}: {
  connected: boolean;
  label: string;
}) {
  return (
    <span className="inline-flex items-center gap-1.5 font-mono text-[10px] text-faint">
      <Dot tone={connected ? "pass" : "fail"} pulse={connected} />
      {label}
    </span>
  );
}
