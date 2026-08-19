import { Check, GitMerge, Loader2, X } from "lucide-react";
import { Button, IconButton } from "@/components/ui/controls";
import { Mono } from "@/components/ui/primitives";
import { useConfirm } from "@/components/ui/action-feedback";
import type { ReviewBatch, ReviewBatchWave } from "@/types/domain";
import { isBatchSelectable } from "@/features/work/work-utils";

export type BatchAction = "close" | "land";

export interface BatchItemResult {
  taskId: string;
  ok: boolean;
  reason: string;
}

export interface BatchProgress {
  action: BatchAction;
  done: number;
  total: number;
}

export function WaveReviewGroups({
  batch,
  disabled,
  onSelectWave,
}: {
  batch: ReviewBatch;
  disabled: boolean;
  onSelectWave: (wave: ReviewBatchWave) => void;
}) {
  if (batch.waves.length === 0 && batch.unwaved.length === 0) return null;
  return (
    <section className="mb-5 space-y-2" aria-label="Wave review batches">
      {batch.waves.map((wave) => {
        const selectable = wave.members.filter(isBatchSelectable);
        return (
          <div key={wave.waveId} className="rounded-lg border border-line bg-panel px-3.5 py-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">{wave.waveId}</div>
                <h2 className="text-[14px] font-semibold text-ink">
                  {wave.readyForReview ? `Wave ${wave.title} ready for your review` : `Wave ${wave.title} is still in progress`}
                </h2>
              </div>
              <Button size="sm" disabled={disabled || !wave.readyForReview || selectable.length === 0} onClick={() => onSelectWave(wave)}>
                Review wave ({selectable.length})
              </Button>
            </div>
            <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted">
              {wave.members.map((task) => <span key={task.id}><Mono className="text-[10px] text-faint">{task.id}</Mono> {task.title}</span>)}
            </div>
          </div>
        );
      })}
      {batch.unwaved.length > 0 && (
        <div className="rounded-lg border border-line-soft bg-panel/50 px-3.5 py-2.5">
          <div className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Unwaved review</div>
          <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted">
            {batch.unwaved.map((task) => <span key={task.id}><Mono className="text-[10px] text-faint">{task.id}</Mono> {task.title}</span>)}
          </div>
        </div>
      )}
    </section>
  );
}

export function BatchBar({
  activeIds,
  closeIds,
  landIds,
  progress,
  results,
  disabled,
  confirm,
  onRun,
  onClearSelection,
  onDismissResults,
}: {
  activeIds: string[];
  closeIds: string[];
  landIds: string[];
  progress: BatchProgress | null;
  results: { action: BatchAction; items: BatchItemResult[] } | null;
  disabled: boolean;
  confirm: ReturnType<typeof useConfirm>;
  onRun: (action: BatchAction, ids: string[]) => void;
  onClearSelection: () => void;
  onDismissResults: () => void;
}) {
  if (results) {
    const failed = results.items.filter((i) => !i.ok);
    const passed = results.items.length - failed.length;
    const verb = results.action === "close" ? "accepted" : "landed";
    return (
      <div className="fixed bottom-6 left-1/2 z-40 w-[min(92vw,460px)] -translate-x-1/2">
        <div className="animate-rise rounded-xl border border-line bg-raised p-3 shadow-lg">
          <div className="mb-2 flex items-center justify-between gap-3">
            <div className="text-[13px] font-semibold text-ink">
              {passed} {verb}
              {failed.length > 0 ? ` · ${failed.length} failed` : ""}
            </div>
            <IconButton onClick={onDismissResults} aria-label="Dismiss batch results">
              <X size={14} />
            </IconButton>
          </div>
          {failed.length === 0 ? (
            <div className="text-[12px] text-pass">All selected tasks {verb} cleanly.</div>
          ) : (
            <ul className="tk-scroll max-h-40 space-y-1 overflow-y-auto">
              {failed.map((f) => (
                <li key={f.taskId} className="flex items-start gap-2 text-[12px] leading-snug">
                  <Mono className="flex-none text-[10.5px] text-faint">{f.taskId}</Mono>
                  <span className="text-fail">{f.reason}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    );
  }

  if (progress) {
    const label = progress.action === "close" ? "Accepting" : "Landing";
    return (
      <div className="fixed bottom-6 left-1/2 z-40 -translate-x-1/2">
        <div className="animate-rise flex items-center gap-2.5 rounded-xl border border-line bg-raised px-3.5 py-2.5 shadow-lg">
          <Loader2 size={14} className="animate-spin text-accent" />
          <Mono className="text-[12px] text-ink-soft">
            {label} {progress.done}/{progress.total}…
          </Mono>
        </div>
      </div>
    );
  }

  if (activeIds.length === 0) return null;
  const plural = (value: number) => value === 1 ? "" : "s";
  const acceptClose = async () => {
    const count = closeIds.length;
    if (count === 0) return;
    const ok = await confirm({
      title: "Accept & close selected",
      body: `Accept and close ${count} review task${plural(count)}. This records acceptance and cannot be undone.`,
      confirmLabel: "Accept & close",
    });
    if (ok) onRun("close", closeIds);
  };
  const land = async () => {
    const count = landIds.length;
    if (count === 0) return;
    const ok = await confirm({
      title: "Land selected tasks",
      body: `Land ${count} accepted task${plural(count)} onto the base branch. Landing is irreversible.`,
      confirmLabel: `Land ${count}`,
      tone: "danger",
      typeToConfirm: "land",
    });
    if (ok) onRun("land", landIds);
  };

  return (
    <div className="fixed bottom-6 left-1/2 z-40 -translate-x-1/2">
      <div className="animate-rise flex max-w-[calc(100vw-1.5rem)] flex-wrap items-center justify-center gap-2.5 rounded-xl border border-line bg-raised px-3 py-2 shadow-lg">
        <Mono className="pl-1 text-[12px] text-ink-soft">{activeIds.length} selected</Mono>
        <span className="h-4 w-px flex-none bg-line" />
        <Button size="sm" variant="default" disabled={disabled || closeIds.length === 0} onClick={acceptClose}>
          <Check size={13} strokeWidth={2.25} />
          Accept &amp; close
        </Button>
        <Button size="sm" variant="danger" disabled={disabled || landIds.length === 0} onClick={land}>
          <GitMerge size={13} strokeWidth={2.25} />
          Land selected
        </Button>
        <IconButton onClick={onClearSelection} aria-label="Clear selection" disabled={disabled}>
          <X size={14} />
        </IconButton>
      </div>
    </div>
  );
}
