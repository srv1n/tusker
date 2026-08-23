/*
  The front-matter header, editable in place.

  Front-matter is shown as typed facts, never as raw YAML. Status, keywords, and
  part_of are edited here through typed controls; the document's title lives in
  the body editor as its leading `# ` heading, so it stays editable inline rather
  than being duplicated as a card title. Any edit here marks the document dirty.
*/

import { useState, type KeyboardEvent } from "react";
import { ChevronDown, Plus, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { Card, Mono } from "@/components/ui/primitives";
import { Select, TextInput } from "@/components/ui/controls";
import { KindBadge } from "./bits";
import type { DocgraphKind } from "./types";

const KNOWN_STATUSES = ["canonical", "active", "draft", "accepted", "superseded"];
const CUSTOM_SENTINEL = "__custom__";

export function HeaderCard({
  kind,
  subject,
  path,
  status,
  onStatusChange,
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
  keywords: string[];
  onAddKeyword: (v: string) => void;
  onRemoveKeyword: (v: string) => void;
  partOf: string;
  onPartOfChange: (v: string) => void;
  /** Corpus subjects (from the docgraph list) that part_of may point at. */
  subjects: string[];
}) {
  return (
    <Card className="mb-8 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <KindBadge kind={kind} />
        <StatusEditor value={status} onChange={onStatusChange} />
      </div>
      <Mono className="mt-3 block text-[11.5px] text-faint">{subject}</Mono>

      <div className="mt-3 flex flex-col gap-2.5">
        <div className="flex items-center gap-2">
          <span className="w-[52px] flex-none font-mono text-[10.5px] uppercase tracking-[0.08em] text-fainter">
            Part of
          </span>
          <PartOfEditor value={partOf} onChange={onPartOfChange} subjects={subjects.filter((s) => s !== subject)} />
        </div>
        <div className="flex items-start gap-2">
          <span className="mt-1 w-[52px] flex-none font-mono text-[10.5px] uppercase tracking-[0.08em] text-fainter">
            Keywords
          </span>
          <KeywordsEditor keywords={keywords} onAdd={onAddKeyword} onRemove={onRemoveKeyword} />
        </div>
      </div>

      <Mono className="mt-3 block truncate text-[10.5px] text-fainter">{path}</Mono>
    </Card>
  );
}

/** Status select over the known values, with a free-text fallback. */
function StatusEditor({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const isKnown = KNOWN_STATUSES.includes(value);
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
            if (!KNOWN_STATUSES.includes(value)) onChange(KNOWN_STATUSES[0]);
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
        {KNOWN_STATUSES.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
        <option value={CUSTOM_SENTINEL}>Custom…</option>
      </Select>
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
