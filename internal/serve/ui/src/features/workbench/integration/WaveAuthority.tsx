import { Pause, Play, RotateCcw } from "lucide-react";
import { ActionResultLine } from "@/components/ui/action-feedback";
import { useWaveControl, useWaveReview } from "@/lib/queries";
import { cn } from "@/lib/cn";
import { ProductLabel, ProductLoading, ProductSection, ProductStatus, ProductUnavailable, phaseTone } from "@/features/product/shared";
import type { DirectStartBlocker, WaveReview, WaveReviewMember } from "@/types/domain";

const WAVE_CONTROL_LABEL: Record<string, string> = {
  "wave start": "Start",
  "wave pause": "Pause",
  "wave resume": "Resume",
};
const WAVE_CONTROL_PENDING: Record<string, string> = {
  "wave start": "Starting…",
  "wave pause": "Pausing…",
  "wave resume": "Resuming…",
};
const WAVE_CONTROL_ACTION: Record<string, "start" | "pause" | "resume"> = {
  "wave start": "start",
  "wave pause": "pause",
  "wave resume": "resume",
};
const WAVE_CONTROL_ICON: Record<string, typeof Play> = {
  "wave start": Play,
  "wave pause": Pause,
  "wave resume": RotateCcw,
};

function routeBlockerCode(code: string): boolean {
  return code.startsWith("ROUTE_") || code === "RUNTIME_UNAVAILABLE";
}

function WaveBlockerList({ blockers, projectId }: { blockers: DirectStartBlocker[]; projectId: string }) {
  if (blockers.length === 0) return null;
  return (
    <div role="alert" className="mt-2 rounded-lg border border-warn/30 bg-warn-soft px-3 py-2.5 text-left text-[12px] leading-5 text-warn" data-wave-blockers>
      <ul className="space-y-1.5">
        {blockers.map((blocker, index) => (
          <li key={`${blocker.code}-${blocker.taskId ?? ""}-${index}`}>
            <span className="font-mono text-[10.5px] font-semibold">{blocker.code}</span>
            {blocker.taskId ? (
              <a href={`/p/${projectId}/tasks/${blocker.taskId}`} className="ml-1 font-mono text-[10.5px] font-semibold underline">{blocker.taskId}</a>
            ) : null}
            <span className="block">{blocker.reason}</span>
            <span className="block font-medium">
              {blocker.action}
              {routeBlockerCode(blocker.code) ? (
                <a href="/settings" className="ml-1 font-semibold underline">Open Models in Settings</a>
              ) : null}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function WaveAuthorityControls({ projectId, waveId, compact }: { projectId: string; waveId: string; compact?: boolean }) {
  const review = useWaveReview(waveId, projectId);
  const control = useWaveControl(projectId, waveId);
  const data = review.data;
  const enabled = (data?.controls ?? []).find((candidate) => candidate.enabled && candidate.action !== "task start" && WAVE_CONTROL_ACTION[candidate.action]);
  const disabledReason = (data?.controls ?? []).find((candidate) => candidate.action !== "task start")?.reason;
  const Icon = enabled ? WAVE_CONTROL_ICON[enabled.action] : Play;
  return (
    <div className={cn("text-left", compact ? "max-w-[24rem]" : "w-full")} data-wave-authority data-wave-state={data?.state ?? "loading"}>
      {review.isLoading ? <p role="status" className="text-[11px] text-faint">Loading wave review…</p> : null}
      {review.error ? <p role="alert" className="text-[11px] leading-4 text-fail">{review.error instanceof Error ? review.error.message : "Wave review is unavailable."}</p> : null}
      {data ? (
        <div className="flex flex-wrap items-center gap-2">
          <ProductStatus tone={phaseTone(data.state)}>{data.state}</ProductStatus>
          <span className="font-mono text-[10.5px] text-faint">Authorization: {data.authorization}</span>
        </div>
      ) : null}
      {data ? <WaveBlockerList blockers={data.blockers} projectId={projectId} /> : null}
      {enabled ? (
        <button
          type="button"
          data-wave-control={enabled.action}
          aria-label={`${WAVE_CONTROL_LABEL[enabled.action]} wave ${waveId}`}
          disabled={control.isPending}
          onClick={() => control.mutate(WAVE_CONTROL_ACTION[enabled.action])}
          className="mt-2 inline-flex items-center gap-1.5 rounded-md bg-ink px-3 py-2 text-[12px] font-semibold text-surface disabled:opacity-50"
        >
          <Icon size={13} aria-hidden="true" />
          {control.isPending ? WAVE_CONTROL_PENDING[enabled.action] : WAVE_CONTROL_LABEL[enabled.action]}
        </button>
      ) : data && disabledReason ? (
        <p role="status" className="mt-2 text-[11px] leading-4 text-muted">{disabledReason}</p>
      ) : null}
      <div aria-live="polite" className="mt-2">
        <ActionResultLine pending={control.isPending} error={control.error} result={control.data} />
        {control.data ? (
          <p role="status" className="text-[11px] leading-4 text-muted">
            {control.data.queuedTaskIds?.length ? `Queued ${control.data.queuedTaskIds.length} task${control.data.queuedTaskIds.length === 1 ? "" : "s"}.` : null}
            {control.data.replayed ? "Already authorized for this material." : null}
          </p>
        ) : null}
      </div>
    </div>
  );
}

export function WaveInstructions({ member }: { member: WaveReviewMember }) {
  const detail = [member.instructions, ...(member.acceptance?.length ? [`Acceptance: ${member.acceptance.join(" · ")}`] : []), ...(member.verification?.length ? [`Verification: ${member.verification.join(" · ")}`] : [])].filter(Boolean).join("\n\n");
  if (!detail.trim()) return null;
  return (
    <details className="mt-2 rounded-md border border-line bg-panel/60 px-3 py-2" data-wave-instructions={member.taskId}>
      <summary className="cursor-pointer text-[11.5px] font-medium text-muted hover:text-ink">Instructions</summary>
      <div className="mt-2 whitespace-pre-wrap text-[12px] leading-5 text-ink">{detail}</div>
    </details>
  );
}

export function WaveMemberList({ review, projectId }: { review: WaveReview; projectId?: string }) {
  return (
    <div className="space-y-3" data-wave-members>
      {review.members.map((member) => (
        <div key={member.taskId} className="rounded-lg border border-line bg-panel p-3" data-wave-member={member.taskId}>
          <div className="mb-2 flex flex-wrap items-center gap-2">
            {projectId ? (
              <a href={`/p/${projectId}/tasks/${member.taskId}`} className="font-mono text-[10.5px] text-faint hover:text-ink">{member.taskId}</a>
            ) : (
              <span className="font-mono text-[10.5px] text-faint">{member.taskId}</span>
            )}
            <span className="text-[12.5px] font-semibold text-ink">{member.title}</span>
            <ProductStatus tone={phaseTone(member.state)}>{member.state}</ProductStatus>
            {member.waitingReason ? <span className="text-[11px] text-warn">{member.waitingReason}</span> : null}
          </div>
          <div className="grid gap-2 text-[11.5px] text-muted sm:grid-cols-2">
            {member.executeRoute ? <div><span className="font-mono text-[9px] uppercase tracking-[0.12em] text-faint">Will execute</span><span className="mt-0.5 block text-ink">{member.executeRoute}</span></div> : null}
            {member.reviewRoute ? <div><span className="font-mono text-[9px] uppercase tracking-[0.12em] text-faint">Will review</span><span className="mt-0.5 block text-ink">{member.reviewRoute}</span></div> : null}
          </div>
          {member.dependencies?.length ? <p className="mt-2 text-[11px] text-muted">Depends on {member.dependencies.join(", ")}</p> : null}
          <WaveInstructions member={member} />
        </div>
      ))}
    </div>
  );
}

export function WaveReviewDetail({ projectId, waveId, showControls = true }: { projectId: string; waveId: string; showControls?: boolean }) {
  const review = useWaveReview(waveId, projectId);
  if (review.isLoading) return <ProductLoading rows={3} />;
  if (review.error) return <ProductUnavailable>Wave review is unavailable. {review.error instanceof Error ? review.error.message : "Try refreshing this project."}</ProductUnavailable>;
  const data = review.data;
  if (!data) return null;
  return (
    <ProductSection title="Wave authority">
      <div className="rounded-lg border border-line bg-panel p-4" data-wave-review={data.waveId}>
        {showControls !== false ? <WaveAuthorityControls projectId={projectId} waveId={waveId} /> : null}
        {data.outcome ? <p className="mt-3 text-[12.5px] leading-5 text-ink">{data.outcome}</p> : null}
        <details className="mt-3 text-[11px] text-faint">
          <summary className="cursor-pointer">Material fingerprint</summary>
          <p className="mt-1 break-all font-mono">{data.materialFingerprint}</p>
        </details>
        {data.frontiers.length > 0 ? (
          <div className="mt-3">
            <ProductLabel>Frontiers</ProductLabel>
            <ol className="mt-1 space-y-1">
              {data.frontiers.map((frontier, index) => (
                <li key={index} className="rounded-md bg-surface px-3 py-1.5 text-[12px] text-ink">
                  <span className="mr-2 font-mono text-[10px] text-faint">{index + 1}</span>{frontier.join(" → ")}
                </li>
              ))}
            </ol>
          </div>
        ) : null}
        <div className="mt-4">
          <WaveMemberList review={data} projectId={projectId} />
        </div>
      </div>
    </ProductSection>
  );
}
