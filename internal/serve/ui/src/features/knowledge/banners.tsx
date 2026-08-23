/*
  Save-outcome banners for the corpus editor. Each surfaces one
  save result inline above the document: a validated save (with any advisory
  warnings), an on-disk conflict with a reload path, refused header defects, or a
  transport error. Colors resolve through the shared tone tokens only.
*/

import { AlertTriangle, Check, RotateCcw, X } from "lucide-react";
import { Mono } from "@/components/ui/primitives";
import type { DocSaveDefect } from "./types";

/** 200 with warnings, or a plain confirmation — dismissible. */
export function SavedNotice({ warnings, onDismiss }: { warnings: string[]; onDismiss: () => void }) {
  const hasWarnings = warnings.length > 0;
  return (
    <div
      className={
        hasWarnings
          ? "mb-6 animate-rise rounded-xl border border-warn/30 bg-warn-soft px-4 py-3"
          : "mb-6 flex animate-rise items-center gap-2.5 rounded-xl border border-pass/30 bg-pass-soft px-4 py-3"
      }
    >
      {!hasWarnings ? (
        <>
          <span className="flex h-4 w-4 flex-none items-center justify-center rounded-full bg-pass text-surface">
            <Check size={10} strokeWidth={3} />
          </span>
          <span className="flex-1 text-[13.5px] text-ink-soft">Saved · written to disk.</span>
          <DismissButton onDismiss={onDismiss} />
        </>
      ) : (
        <>
          <div className="mb-2 flex items-center gap-2.5">
            <span className="flex h-4 w-4 flex-none items-center justify-center rounded-full bg-pass text-surface">
              <Check size={10} strokeWidth={3} />
            </span>
            <span className="flex-1 text-[13.5px] font-semibold text-ink">Saved · with warnings</span>
            <DismissButton onDismiss={onDismiss} />
          </div>
          <ul className="flex flex-col gap-1.5 pl-6.5">
            {warnings.map((w, i) => (
              <li key={i} className="flex gap-2 text-[12.5px] text-ink-soft">
                <AlertTriangle size={13} className="mt-0.5 flex-none text-warn" strokeWidth={2} />
                <span>{w}</span>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

/** 409 — the file changed on disk since it was loaded. */
export function ConflictNotice({ currentRev, onReload }: { currentRev: string; onReload: () => void }) {
  return (
    <div className="mb-6 animate-rise rounded-xl border border-warn/30 bg-warn-soft px-4.5 py-4">
      <div className="mb-1.5 flex items-center gap-2.5">
        <span className="h-2 w-2 flex-none rounded-full bg-warn" />
        <span className="text-[14px] font-semibold text-ink">This document changed on disk</span>
      </div>
      <p className="mb-3 text-[13px] leading-[1.5] text-muted">
        It was edited elsewhere since you opened it{" "}
        {currentRev && (
          <>
            (on-disk rev <Mono className="text-ink-soft">{currentRev.slice(0, 12)}</Mono>)
          </>
        )}
        . Your save was refused so nothing is overwritten — reload to pick up the latest, then
        reapply your change.
      </p>
      <button
        onClick={onReload}
        className="inline-flex items-center gap-1.5 rounded-lg bg-warn px-3.5 py-2 text-[12.5px] font-semibold leading-none text-surface transition-opacity hover:opacity-90"
      >
        <RotateCcw size={13} strokeWidth={2} />
        Reload latest
      </button>
    </div>
  );
}

/** 422 — refused header-rule defects, named. */
export function DefectsNotice({ defects, onDismiss }: { defects: DocSaveDefect[]; onDismiss: () => void }) {
  return (
    <div className="mb-6 animate-rise rounded-xl border border-fail/30 bg-fail-soft px-4 py-3.5">
      <div className="mb-2 flex items-center gap-2.5">
        <span className="h-2 w-2 flex-none rounded-full bg-fail" />
        <span className="flex-1 text-[14px] font-semibold text-ink">
          Save refused · {defects.length} {defects.length === 1 ? "defect" : "defects"}
        </span>
        <DismissButton onDismiss={onDismiss} />
      </div>
      <div className="flex flex-col gap-1.5 text-[12.5px]">
        {defects.map((d, i) => (
          <div key={i} className="flex gap-2.5">
            <Mono className="flex-none font-semibold text-fail">{d.code}</Mono>
            <span className="text-ink-soft">{d.message}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

/** Any other transport failure — the standard error surface, inline. */
export function ErrorNotice({ message, onDismiss }: { message: string; onDismiss: () => void }) {
  return (
    <div className="mb-6 flex animate-rise items-start gap-2.5 rounded-xl border border-fail/30 bg-fail-soft px-4 py-3.5">
      <AlertTriangle size={15} className="mt-0.5 flex-none text-fail" strokeWidth={2} />
      <div className="min-w-0 flex-1">
        <div className="text-[13.5px] font-semibold text-ink">Save failed</div>
        <div className="mt-0.5 break-words font-mono text-[12px] text-fail">{message}</div>
      </div>
      <DismissButton onDismiss={onDismiss} />
    </div>
  );
}

function DismissButton({ onDismiss }: { onDismiss: () => void }) {
  return (
    <button
      onClick={onDismiss}
      aria-label="Dismiss"
      className="flex h-5 w-5 flex-none items-center justify-center rounded-md text-faint transition-colors hover:bg-hover hover:text-ink"
    >
      <X size={13} strokeWidth={2} />
    </button>
  );
}
