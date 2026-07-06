import type { ReactNode } from "react";
import { AlertTriangle, FileQuestion, Inbox, Terminal } from "lucide-react";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { ApiError } from "@/lib/api";

/** Shimmer block. Loading states use skeletons, never spinners (packet §5). */
export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse-soft rounded-md bg-hover", className)} />;
}

/** A stack of skeleton rows sized like list cards. */
export function SkeletonRows({ rows = 4, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn("flex flex-col gap-2.5", className)}>
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className="h-[68px] w-full" />
      ))}
    </div>
  );
}

/** Calm empty state — the good state, with a helpful next action. */
export function EmptyState({
  icon,
  title,
  hint,
  action,
}: {
  icon?: ReactNode;
  title: string;
  hint?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-line px-6 py-16 text-center">
      <div className="text-faint">{icon ?? <Inbox size={22} strokeWidth={1.5} />}</div>
      <div className="text-[15px] font-medium text-ink-soft">{title}</div>
      {hint && <div className="max-w-sm text-[13px] leading-relaxed text-muted">{hint}</div>}
      {action && <div className="mt-1">{action}</div>}
    </div>
  );
}

/**
 * Error state. The daemon may simply not be running — say so plainly and show
 * the command to start it (packet §5).
 */
export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const isConnRefused =
    error instanceof ApiError ? error.status >= 500 || error.status === 0 : true;
  const isNotFound = error instanceof ApiError && error.status === 404;

  if (isNotFound) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-line bg-panel px-6 py-16 text-center">
        <div className="text-faint">
          <FileQuestion size={22} strokeWidth={1.5} />
        </div>
        <div className="text-[15px] font-medium text-ink-soft">Nothing here yet</div>
        <div className="max-w-md text-[13px] leading-relaxed text-muted">
          {error instanceof Error ? error.message : "Not found."}
        </div>
        {onRetry && (
          <button
            onClick={onRetry}
            className="mt-1 rounded-lg border border-line px-3 py-1.5 text-[12.5px] font-medium text-ink-soft transition-colors hover:bg-hover"
          >
            Retry
          </button>
        )}
      </div>
    );
  }

  if (isConnRefused) {
    return (
      <div className="flex flex-col items-center justify-center gap-4 rounded-xl border border-line bg-panel px-6 py-16 text-center">
        <div className="text-warn">
          <Terminal size={22} strokeWidth={1.5} />
        </div>
        <div className="text-[15px] font-medium text-ink-soft">The daemon isn’t responding</div>
        <div className="max-w-md text-[13px] leading-relaxed text-muted">
          Serve talks to the tusker daemon on <Mono>localhost:7420</Mono>. It may not be
          running. Start it with:
        </div>
        <Mono className="rounded-lg border border-line bg-surface px-3 py-2 text-[12.5px] text-ink">
          tusker daemon up
        </Mono>
        {onRetry && (
          <button
            onClick={onRetry}
            className="mt-1 rounded-lg border border-line px-3 py-1.5 text-[12.5px] font-medium text-ink-soft transition-colors hover:bg-hover"
          >
            Retry
          </button>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-fail/30 bg-fail-soft px-6 py-14 text-center">
      <div className="text-fail">
        <AlertTriangle size={22} strokeWidth={1.5} />
      </div>
      <div className="text-[15px] font-medium text-ink-soft">Something went wrong</div>
      <div className="max-w-md font-mono text-[12px] text-fail">
        {error instanceof Error ? error.message : String(error)}
      </div>
      {onRetry && (
        <button
          onClick={onRetry}
          className="mt-1 rounded-lg border border-line px-3 py-1.5 text-[12.5px] font-medium text-ink-soft transition-colors hover:bg-hover"
        >
          Retry
        </button>
      )}
    </div>
  );
}

/**
 * Thin wrapper turning a TanStack Query result into the right state. Screens
 * call `<QueryBoundary q={q}>{(data) => …}</QueryBoundary>`.
 */
export function QueryBoundary<T>({
  q,
  loading,
  children,
}: {
  q: {
    data: T | undefined;
    isLoading: boolean;
    isError: boolean;
    error: unknown;
    refetch: () => void;
  };
  loading?: ReactNode;
  children: (data: T) => ReactNode;
}) {
  if (q.isLoading) return <>{loading ?? <SkeletonRows />}</>;
  // A nil Go slice marshals to JSON `null`, so a 200 body can be `null` even
  // though the type says `T`. Guard it alongside `undefined` — otherwise the
  // null reaches `children(data)` and white-screens on `data.length`/`.map`.
  if (q.isError || q.data === undefined || q.data === null)
    return <ErrorState error={q.error} onRetry={() => q.refetch()} />;
  return <>{children(q.data)}</>;
}
