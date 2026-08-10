/*
  Shared action feedback primitives.

  Two things every mutating control needs and none should hand-roll:

    - `useConfirm` / `ConfirmProvider` — a promise-based confirm dialog. High
      blast-radius actions (land, waive, stop the daemon) pass `typeToConfirm`
      so the operator must type the exact target id before the button arms.
      Never `window.confirm` — that blocks the page and can't match the app.

    - `ActionResultLine` — the one place mutation feedback is rendered: a
      spinner while pending, the transport error on failure, the refusal reason
      when the daemon refused in-band (ok:false / refused:true), or the success
      reason. Components read `m.isPending` / `m.error` / `m.data` straight off
      the TanStack mutation, so a failure is never silent.
*/

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { Loader2 } from "lucide-react";
import { cn } from "@/lib/cn";
import { ApiError } from "@/lib/api";
import { Mono } from "@/components/ui/primitives";

export interface ConfirmOptions {
  title: string;
  body?: string;
  confirmLabel: string;
  cancelLabel?: string;
  tone?: "danger" | "default";
  /** When set, the operator must type this exact string to arm the confirm. */
  typeToConfirm?: string;
}

type ConfirmFn = (opts: ConfirmOptions) => Promise<boolean>;

interface PendingRequest extends ConfirmOptions {
  resolve: (ok: boolean) => void;
}

const ConfirmContext = createContext<ConfirmFn | null>(null);

/**
 * Hosts the single confirm dialog for the app. Wrap the tree once (see
 * `main.tsx`) so `useConfirm` resolves everywhere below it.
 */
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [request, setRequest] = useState<PendingRequest | null>(null);

  const confirm = useCallback<ConfirmFn>(
    (opts) =>
      new Promise<boolean>((resolve) => {
        // Guard against React's StrictMode double-invoking the state updater —
        // a promise resolves once; the second call is a no-op regardless.
        let settled = false;
        const resolveOnce = (ok: boolean) => {
          if (settled) return;
          settled = true;
          resolve(ok);
        };
        setRequest({ ...opts, resolve: resolveOnce });
      }),
    [],
  );

  const settle = useCallback((ok: boolean) => {
    setRequest((current) => {
      current?.resolve(ok);
      return null;
    });
  }, []);

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {request ? <ConfirmDialog request={request} onSettle={settle} /> : null}
    </ConfirmContext.Provider>
  );
}

/** Promise-based confirm. Resolves `true` on confirm, `false` on cancel/dismiss. */
export function useConfirm(): ConfirmFn {
  const ctx = useContext(ConfirmContext);
  if (!ctx) throw new Error("useConfirm must be used within a <ConfirmProvider>");
  return ctx;
}

const confirmBtn =
  "rounded-lg px-4 py-2 text-[12.5px] font-semibold leading-none transition-colors disabled:cursor-not-allowed disabled:opacity-45";

function ConfirmDialog({
  request,
  onSettle,
}: {
  request: PendingRequest;
  onSettle: (ok: boolean) => void;
}) {
  const {
    title,
    body,
    confirmLabel,
    cancelLabel = "Cancel",
    tone = "default",
    typeToConfirm,
  } = request;

  const [typed, setTyped] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const openerRef = useRef<HTMLElement | null>(null);
  const titleId = useId();
  const bodyId = useId();

  const matches = !typeToConfirm || typed === typeToConfirm;

  // Land focus on the safest actionable control: the type-to-confirm field
  // when present (nothing is armed yet), otherwise the confirm button.
  useEffect(() => {
    openerRef.current = document.activeElement as HTMLElement | null;
    if (typeToConfirm) inputRef.current?.focus();
    else confirmRef.current?.focus();
  }, [typeToConfirm]);

  // Escape always cancels; Enter confirms when armed.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        settle(false);
      } else if (event.key === "Tab") {
        const root = dialogRef.current;
        const focusable = root ? Array.from(root.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), [href], [tabindex]:not([tabindex="-1"])')) : [];
        if (focusable.length) {
          const first = focusable[0]!; const last = focusable[focusable.length - 1]!;
          if (!root?.contains(document.activeElement)) { event.preventDefault(); first.focus(); }
          else if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
          else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
        }
      } else if (event.key === "Enter" && matches) {
        event.preventDefault();
        settle(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [matches, onSettle, typeToConfirm]);

  const settle = (ok: boolean) => {
    onSettle(ok);
    requestAnimationFrame(() => openerRef.current?.focus());
  };

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/50" onClick={() => settle(false)} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={body ? bodyId : undefined}
        className="relative w-full max-w-[420px] animate-rise rounded-xl border border-line bg-panel p-5 shadow-2xl"
      >
        <h2 id={titleId} className="mb-2 font-serif text-[17px] font-semibold leading-snug text-ink">
          {title}
        </h2>
        {body ? (
          <p id={bodyId} className="mb-3 text-[13px] leading-[1.55] text-muted">
            {body}
          </p>
        ) : null}

        {typeToConfirm ? (
          <div className="mb-4">
            <p className="mb-1.5 text-[12px] leading-snug text-faint">
              Type{" "}
              <Mono className="rounded bg-surface px-1 py-0.5 text-[11.5px] font-semibold text-ink-soft">
                {typeToConfirm}
              </Mono>{" "}
              to confirm.
            </p>
            <input
              ref={inputRef}
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              placeholder={typeToConfirm}
              autoComplete="off"
              spellCheck={false}
              className="h-8.5 w-full rounded-lg border border-line bg-surface px-3 font-mono text-[13px] text-ink placeholder:text-faint focus-visible:border-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/20"
            />
          </div>
        ) : null}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={() => settle(false)}
            className={cn(confirmBtn, "border border-line text-ink-soft hover:bg-hover")}
          >
            {cancelLabel}
          </button>
          <button
            ref={confirmRef}
            type="button"
            disabled={!matches}
            onClick={() => settle(true)}
            className={cn(
              confirmBtn,
              "text-surface hover:opacity-90",
              tone === "danger" ? "bg-fail" : "bg-ink",
            )}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

/**
 * Standard mutation feedback line. Feed it a TanStack mutation's `isPending`,
 * `error`, and `data`:
 *
 *   <ActionResultLine pending={m.isPending} error={m.error} result={m.data} />
 *
 * Precedence: pending → transport error → in-band refusal → success. Idle → null.
 */
export function ActionResultLine({
  pending,
  error,
  result,
  className,
}: {
  pending?: boolean;
  error?: unknown;
  result?: { ok?: boolean; refused?: boolean; reason?: string } | null;
  className?: string;
}) {
  if (pending) {
    return (
      <div className={cn("flex items-center gap-1.5 text-[12px] text-faint", className)}>
        <Loader2 size={12} className="animate-spin" />
        Working…
      </div>
    );
  }

  if (error) {
    const message =
      error instanceof ApiError || error instanceof Error ? error.message : String(error);
    return (
      <div className={cn("text-[12px] leading-snug text-fail", className)}>{message}</div>
    );
  }

  if (result) {
    const refused = result.refused || result.ok === false;
    return (
      <div
        className={cn("text-[12px] leading-snug", refused ? "text-fail" : "text-pass", className)}
      >
        {result.reason || (refused ? "Refused." : "Done.")}
      </div>
    );
  }

  return null;
}
