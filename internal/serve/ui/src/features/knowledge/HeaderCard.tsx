/*
  The front-matter header, editable in place.

  Front-matter is shown as typed facts, never as raw YAML. Status, keywords, and
  part_of are edited here through typed controls; the document's title lives in
  the body editor as its leading `# ` heading, so it stays editable inline rather
  than being duplicated as a card title. Any edit here marks the document dirty.
*/

import { useState, type KeyboardEvent } from "react";
import { ChevronDown, ChevronRight, Plus, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { Select, TextInput } from "@/components/ui/controls";
import { ConformanceChip, DocStatusChip, KindBadge } from "./bits";
import { CONFORMANCE_VALUES, LIFECYCLE_BY_KIND, type DocgraphKind } from "./types";

const CUSTOM_SENTINEL = "__custom__";

export function HeaderCard({
  kind,
  subject,
  path,
  status,
  onStatusChange,
  conformance,
  onConformanceChange,
  lastVerified,
  onLastVerifiedChange,
  keywords,
  onAddKeyword,
  onRemoveKeyword,
  partOf,
  onPartOfChange,
  subjects,
}: {
  kind: DocgraphKind;
  subject: string;
  path: string;
  status: string;
  onStatusChange: (v: string) => void;
  conformance: string;
  onConformanceChange: (v: string) => void;
  lastVerified: string;
  onLastVerifiedChange: (v: string) => void;
  keywords: string[];
  onAddKeyword: (v: string) => void;
  onRemoveKeyword: (v: string) => void;
  partOf: string;
  onPartOfChange: (v: string) => void;
  /** Corpus subjects (from the docgraph list) that part_of may point at. */
  subjects: string[];
}) {
  return (
    <section aria-label="Document details" className="mt-1">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <KindBadge kind={kind} />
        <DocStatusChip status={status} />
        <ConformanceChip conformance={conformance} />
        <Mono className="min-w-0 truncate text-[11px] text-faint" title={subject}>{subject}</Mono>
      </div>

      <details open className="group mt-1">
        <summary className="flex cursor-pointer list-none items-center gap-2 py-1 font-mono text-[11.5px] text-faint outline-none transition-colors hover:text-ink focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent/40 [&::-webkit-details-marker]:hidden">
          <ChevronRight size={13} strokeWidth={2} className="flex-none transition-transform group-open:rotate-90" />
          <span>Details</span>
          <Mono className="ml-auto min-w-0 truncate text-[10.5px] text-fainter" title={path}>{path}</Mono>
        </summary>
        <div className="mt-2 grid gap-3 rounded-lg border border-line px-3 py-3">
          <div className="flex min-w-0 items-center gap-2">
            <span className="w-[62px] flex-none font-mono text-[10px] uppercase tracking-[0.08em] text-fainter">Status</span>
            <StatusEditor kind={kind} value={status} onChange={onStatusChange} />
          </div>
          <div className="flex min-w-0 items-center gap-2">
            <span className="w-[62px] flex-none font-mono text-[10px] uppercase tracking-[0.08em] text-fainter">Part of</span>
            <PartOfEditor value={partOf} onChange={onPartOfChange} subjects={subjects.filter((s) => s !== subject)} />
          </div>
          <div className="flex min-w-0 items-center gap-2">
            <span className="w-[62px] flex-none font-mono text-[10px] uppercase tracking-[0.08em] text-fainter">Verified</span>
            <ConformanceEditor value={conformance} onChange={onConformanceChange} />
          </div>
          <div className="flex min-w-0 items-center gap-2">
            <span className="w-[62px] flex-none font-mono text-[10px] uppercase tracking-[0.08em] text-fainter">Checked</span>
            <TextInput
              value={lastVerified}
              onChange={(e) => onLastVerifiedChange(e.target.value)}
              placeholder="YYYY-MM-DD @ commit"
              aria-label="Last verified stamp"
              className="h-7 min-w-0 flex-1 font-mono text-[11.5px]"
            />
          </div>
          <div className="flex min-w-0 items-start gap-2">
            <span className="mt-1 w-[62px] flex-none font-mono text-[10px] uppercase tracking-[0.08em] text-fainter">Keywords</span>
            <KeywordsEditor keywords={keywords} onAdd={onAddKeyword} onRemove={onRemoveKeyword} />
          </div>
          <Mono className="truncate text-[10.5px] text-fainter" title={path}>{path}</Mono>
        </div>
      </details>
    </section>
  );
}

/**
 * Lifecycle select over the kind-specific values (docs: current/superseded;
 * proposals: proposed/accepted/implemented/superseded; decisions:
 * proposed/accepted/superseded), with a free-text fallback. Acceptance
 * records intent — it never implies the code was checked.
 */
function StatusEditor({ kind, value, onChange }: { kind: DocgraphKind; value: string; onChange: (v: string) => void }) {
  const known = LIFECYCLE_BY_KIND[kind] ?? [];
  const isKnown = known.includes(value);
  const [custom, setCustom] = useState(false);

  if (custom || (!isKnown && value !== "")) {
    return (
      <div className="inline-flex items-center gap-1.5">
        <TextInput
          autoFocus={custom}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="status"
          className="h-7 w-32 text-[12px]"
        />
        <button
          type="button"
          onClick={() => {
            setCustom(false);
            if (!known.includes(value)) onChange(known[0] ?? "");
          }}
          className="rounded-md px-1.5 py-1 text-[11px] text-faint transition-colors hover:bg-hover hover:text-ink"
        >
          known…
        </button>
      </div>
    );
  }

  return (
    <div className="relative inline-flex items-center">
      <Select
        value={value}
        onChange={(e) => {
          if (e.target.value === CUSTOM_SENTINEL) {
            setCustom(true);
            return;
          }
          onChange(e.target.value);
        }}
        className="h-7 pr-7 text-[12px]"
      >
        {value === "" && <option value="">—</option>}
        {known.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
        <option value={CUSTOM_SENTINEL}>Custom…</option>
      </Select>
    </div>
  );
}

/** Conformance select over the independent verification states. */
function ConformanceEditor({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const normalized = value === "" ? "unverified" : value;
  return (
    <div className="relative inline-flex items-center">
      <Select
        value={CONFORMANCE_VALUES.includes(normalized as (typeof CONFORMANCE_VALUES)[number]) ? normalized : "unverified"}
        onChange={(e) => onChange(e.target.value)}
        aria-label="Code conformance"
        className="h-7 pr-7 font-mono text-[11.5px]"
      >
        {CONFORMANCE_VALUES.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
      </Select>
      <ChevronDown size={12} className="pointer-events-none absolute right-2 text-faint" />
    </div>
  );
}

/** part_of select over corpus subjects (or none). */
function PartOfEditor({
  value,
  onChange,
  subjects,
}: {
  value: string;
  onChange: (v: string) => void;
  subjects: string[];
}) {
  // Preserve an unknown current value as a selectable option so it is not lost.
  const options = value !== "" && !subjects.includes(value) ? [value, ...subjects] : subjects;
  return (
    <div className="relative inline-flex items-center">
      <Select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-7 max-w-[240px] pr-7 font-mono text-[11.5px]"
      >
        <option value="">— none —</option>
        {options.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
      </Select>
      <ChevronDown size={12} className="pointer-events-none absolute right-2 text-faint" />
    </div>
  );
}

/** Chip editor: existing keywords with a remove control, plus an add field. */
function KeywordsEditor({
  keywords,
  onAdd,
  onRemove,
}: {
  keywords: string[];
  onAdd: (v: string) => void;
  onRemove: (v: string) => void;
}) {
  const [draft, setDraft] = useState("");
  const commit = () => {
    const t = draft.trim();
    if (t !== "") onAdd(t);
    setDraft("");
  };
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      commit();
    } else if (e.key === "Backspace" && draft === "" && keywords.length > 0) {
      onRemove(keywords[keywords.length - 1]!);
    }
  };
  return (
    <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
      {keywords.map((k) => (
        <span
          key={k}
          className="inline-flex items-center gap-1 rounded bg-hover px-1.5 py-0.5 font-mono text-[10.5px] text-muted"
        >
          {k}
          <button
            type="button"
            onClick={() => onRemove(k)}
            aria-label={`Remove ${k}`}
            className="text-faint transition-colors hover:text-fail"
          >
            <X size={10} strokeWidth={2.5} />
          </button>
        </span>
      ))}
      <label className={cn("inline-flex items-center gap-1 rounded border border-dashed border-line px-1.5 py-0.5")}>
        <Plus size={11} className="flex-none text-faint" />
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={commit}
          placeholder="add"
          className="w-16 bg-transparent font-mono text-[10.5px] text-ink placeholder:text-faint focus:outline-none"
        />
      </label>
    </div>
  );
}
