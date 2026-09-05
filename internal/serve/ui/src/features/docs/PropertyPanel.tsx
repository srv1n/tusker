import { useState, type KeyboardEvent, type ReactNode } from "react";
import { ChevronDown, Lock } from "lucide-react";
import { cn } from "@/lib/cn";
import {
  frontmatterControlValue,
  frontmatterFieldDefinition,
  lockedFrontmatterReason,
  validateFrontmatterValue,
} from "@/lib/frontmatter";
import { Select, TextInput } from "@/components/ui/controls";
import type { DocMeta } from "@/types/domain";

type Frontmatter = DocMeta["frontmatter"];
type FrontmatterField = Frontmatter[number];

export type FrontmatterCommit = (key: string, value: string) => void | Promise<void>;

/**
 * Frontmatter is structured data. This panel edits free fields through typed
 * controls only; locked fields explain their owner instead of dead-clicking.
 */
export function PropertyPanel({
  frontmatter,
  onCommit,
  pendingKey,
  readOnly = false,
}: {
  frontmatter: Frontmatter;
  onCommit?: FrontmatterCommit;
  pendingKey?: string | null;
  readOnly?: boolean;
}) {
  if (frontmatter.length === 0) return null;
  return (
    <div className="mb-7 rounded-xl border border-line bg-panel/50 px-4 py-3 shadow-2xs">
      <div className="mb-2.5 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-faint">
        Properties <span className="text-fainter font-normal">· structured fields</span>
      </div>
      <div className="flex flex-wrap gap-2">
        {frontmatter.map((field) => (
          <div key={field.key} className="inline-flex items-center gap-1.5 rounded-lg border border-line bg-surface px-2.5 py-1 text-ink shadow-2xs">
            <span className="font-mono text-[10.5px] font-medium text-faint">{field.key}:</span>
            <FrontmatterInlineControl
              field={field}
              onCommit={onCommit}
              readOnly={readOnly}
              pending={pendingKey === field.key}
              className="font-mono text-[11px]"
            >
              <span className="font-medium text-ink-soft">{field.value}</span>
            </FrontmatterInlineControl>
          </div>
        ))}
      </div>
    </div>
  );
}

export function FrontmatterInlineControl({
  field,
  onCommit,
  pending = false,
  readOnly = false,
  showChevron = true,
  className,
  children,
}: {
  field: FrontmatterField;
  onCommit?: FrontmatterCommit;
  pending?: boolean;
  readOnly?: boolean;
  showChevron?: boolean;
  className?: string;
  children: ReactNode;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(frontmatterControlValue(field.key, field.value));
  const [message, setMessage] = useState<string | null>(null);
  const def = frontmatterFieldDefinition(field.key);

  const cancel = () => {
    setEditing(false);
    setMessage(null);
    setDraft(frontmatterControlValue(field.key, field.value));
  };

  const commit = (nextValue: string) => {
    const result = validateFrontmatterValue(field.key, nextValue);
    if (!result.ok) {
      setMessage(result.reason);
      return;
    }
    if (readOnly || !onCommit) {
      setMessage("No structured action is wired for this field yet.");
      return;
    }
    setMessage(null);
    setEditing(false);
    void Promise.resolve(onCommit(field.key, result.value)).catch((err: unknown) => {
      setEditing(true);
      setMessage(err instanceof Error ? err.message : "Structured action failed.");
    });
  };

  const onInputKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      cancel();
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      commit(draft);
    }
  };

  const onLockedClick = () => {
    setEditing(false);
    setMessage(lockedFrontmatterReason(field));
  };

  if (field.locked) {
    return (
      <span className="relative inline-flex">
        <button
          type="button"
          aria-label={`${field.key} (read-only)`}
          disabled={readOnly}
          className={cn(
            "inline-flex items-center gap-1 rounded-md bg-hover px-2 py-[3px] text-muted transition-colors hover:bg-active focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30",
            className,
          )}
          onClick={onLockedClick}
        >
          {children}
          <Lock size={9} strokeWidth={2.25} className="text-fainter" />
        </button>
        {message && <InlineMessage>{message}</InlineMessage>}
      </span>
    );
  }

  return (
    <span className="relative inline-flex">
      <button
        type="button"
        aria-label={readOnly ? `${field.key} (read-only)` : field.key}
        disabled={readOnly || pending}
        className={cn(
          "inline-flex items-center gap-1 rounded-md border border-line bg-raised px-2 py-[3px] text-ink-soft transition-colors hover:border-line-soft hover:bg-hover disabled:cursor-wait disabled:opacity-60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30",
          className,
        )}
        onClick={() => {
          if (readOnly) return;
          setMessage(null);
          setDraft(frontmatterControlValue(field.key, field.value));
          setEditing((open) => !open);
        }}
      >
        {children}
        {pending ? (
          <span className="text-faint">saving</span>
        ) : (
          showChevron && <ChevronDown size={11} strokeWidth={2} className="text-faint" />
        )}
      </button>
      {editing && (
        <span className="absolute left-0 top-[calc(100%+6px)] z-30 min-w-[210px] rounded-lg border border-line bg-surface p-2 shadow-xl">
          {def.kind === "enum" && def.options ? (
            <Select
              autoFocus
              value={field.value}
              onChange={(event) => commit(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Escape") {
                  event.preventDefault();
                  cancel();
                }
              }}
              className="w-full"
            >
              {def.options.map((option) => (
                <option key={option} value={option}>
                  {def.labels?.[option] ?? option}
                </option>
              ))}
            </Select>
          ) : (
            <TextInput
              autoFocus
              type={def.kind === "date" ? "date" : "text"}
              value={draft}
              onChange={(event) => {
                setDraft(event.target.value);
                if (def.kind === "date") commit(event.target.value);
              }}
              onKeyDown={onInputKeyDown}
              className="w-full"
            />
          )}
          {message && <div className="mt-1.5 text-[11px] leading-snug text-fail">{message}</div>}
        </span>
      )}
      {message && !editing && <InlineMessage>{message}</InlineMessage>}
    </span>
  );
}

function InlineMessage({ children }: { children: ReactNode }) {
  return (
    <span
      role="status"
      className="absolute left-0 top-[calc(100%+6px)] z-30 w-[230px] rounded-lg border border-line bg-surface px-2.5 py-2 text-[11px] leading-snug text-muted shadow-xl"
    >
      {children}
    </span>
  );
}
