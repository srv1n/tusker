import { useState, type ReactNode } from "react";
import { useParams } from "@tanstack/react-router";
import { CheckCircle2, CircleAlert, Play, RefreshCw, ShieldCheck } from "lucide-react";
import { DeliveryError } from "@/lib/api";
import { useDeliveryReview, useDeliveryStart } from "@/lib/queries";
import { cn } from "@/lib/cn";
import type { DeliveryReview, DeliveryStartResult } from "@/types/domain";

const defaultPlan = ".tusker/scratch/delivery-plan-v2.yaml";

export function DeliveryReviewPage() {
  const projectId = useParams({ strict: false }).projectId as string;
  const [plan, setPlan] = useState(defaultPlan);
  const [submittedPlan, setSubmittedPlan] = useState(defaultPlan);
  const [confirmation, setConfirmation] = useState("");
  const review = useDeliveryReview(submittedPlan, projectId);
  const start = useDeliveryStart(projectId);
  const data = review.data;
  const exactConfirmation = !!data && confirmation.trim() === data.startBoundary.planFingerprint;

  const refresh = () => {
    setConfirmation("");
    setSubmittedPlan(plan.trim());
  };

  return (
    <main className="mx-auto w-full max-w-6xl px-4 py-5 sm:px-6 lg:px-8" data-delivery-review-page>
      <header className="mb-6 flex flex-col gap-4 border-b border-line pb-5 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-accent">Delivery review</p>
          <h1 className="mt-1 font-serif text-3xl font-semibold tracking-tight text-ink">Review the exact delivery</h1>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">This review is read-only. Starting delivery only imports and arms the reviewed wave—it does not enable automation or change your daemon, permissions, gates, release, or spending.</p>
        </div>
        <form onSubmit={(event) => { event.preventDefault(); refresh(); }} className="flex w-full gap-2 sm:w-auto" aria-label="Delivery plan">
          <input aria-label="Repo-relative plan path" value={plan} onChange={(event) => setPlan(event.target.value)} className="min-w-0 flex-1 rounded-lg border border-line bg-panel px-3 py-2 font-mono text-xs text-ink outline-none focus:border-accent sm:w-80" />
          <button type="submit" className="inline-flex items-center gap-1.5 rounded-lg border border-line px-3 py-2 text-sm font-medium text-ink hover:bg-hover"><RefreshCw size={14} />Review</button>
        </form>
      </header>

      {review.isLoading && <StateCard tone="info" title="Loading delivery review" detail="Reading the canonical product projection." />}
      {review.error && <DeliveryFailure error={review.error} />}
      {data && <ReviewBody review={data} confirmation={confirmation} setConfirmation={setConfirmation} exactConfirmation={exactConfirmation} startResult={start.data} startError={start.error} starting={start.isPending} onStart={() => start.mutate({ plan: submittedPlan, confirm: confirmation.trim() })} />}
    </main>
  );
}

function ReviewBody({ review, confirmation, setConfirmation, exactConfirmation, startResult, startError, starting, onStart }: { review: DeliveryReview; confirmation: string; setConfirmation: (value: string) => void; exactConfirmation: boolean; startResult?: DeliveryStartResult; startError: unknown; starting: boolean; onStart: () => void }) {
  const start = review.startBoundary;
  const canStart = review.ready && exactConfirmation && !starting;
  return <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_22rem]">
    <div className="grid gap-5">
      <ReviewSection number="1" title="What will be delivered">
        <div className="grid gap-3 sm:grid-cols-2">{review.whatWillBeDelivered.map((item) => <article key={item.requirement || item.outcome} className="rounded-xl border border-line bg-panel p-4"><p className="font-mono text-[10px] font-semibold text-faint">{item.requirement || "Outcome"}</p><p className="mt-1 text-sm font-medium leading-5 text-ink">{item.outcome}</p>{item.nonGoals.map((goal) => <p key={goal} className="mt-2 text-xs text-muted">Not included: {goal}</p>)}</article>)}</div>
        {review.nonGoals.length > 0 && <p className="text-sm text-muted">Not included: {review.nonGoals.join(" · ")}</p>}
      </ReviewSection>
      <ReviewSection number="2" title="How it will be proven"><div className="grid gap-3">{review.howItWillBeProven.map((proof) => <article key={proof.outcome} className="rounded-xl border border-line p-4"><p className="font-medium text-ink">{proof.outcome}</p><p className="mt-2 text-xs text-muted">Acceptance: {proof.acceptance.join(" · ") || "No acceptance stated"}</p><List label="Tests" values={proof.tests} /><List label="Artifacts" values={proof.artifacts} /></article>)}</div></ReviewSection>
      <ReviewSection number="3" title="How work flows"><div className="grid gap-4 lg:grid-cols-2"><div><p className="text-sm text-muted">{review.howWorkFlows.integration}</p><p className="mt-2 text-sm text-ink">Expected concurrency: <strong>{review.howWorkFlows.expectedConcurrency}</strong></p></div><ol className="space-y-2">{review.howWorkFlows.frontiers.map((frontier, index) => <li key={index} className="rounded-lg bg-panel px-3 py-2 text-sm text-ink"><span className="mr-2 font-mono text-xs text-faint">{index + 1}</span>{frontier.join(" → ")}</li>)}</ol></div>{review.howWorkFlows.warnings.map((warning) => <p key={warning} className="rounded-lg bg-warn-soft p-3 text-sm text-warn">{warning}</p>)}</ReviewSection>
      <ReviewSection number="4" title="What needs your decision">{review.whatNeedsYourDecision.length === 0 ? <p className="text-sm text-muted">No product decision is needed to start this exact delivery.</p> : <div className="grid gap-3">{review.whatNeedsYourDecision.map((decision) => <article key={decision.title} className="rounded-xl border border-warn/30 bg-warn-soft p-4"><p className="font-medium text-ink">{decision.title}</p><p className="mt-1 text-sm text-muted">{decision.action}</p><p className="mt-2 text-xs text-warn">Why: {decision.why}</p></article>)}</div>}</ReviewSection>
    </div>
    <aside className="h-fit rounded-2xl border border-line bg-panel p-4 xl:sticky xl:top-4">
      <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-accent">5 · Start boundary</p>
      <h2 className="mt-1 text-lg font-semibold text-ink">{start.readiness}</h2>
      <p className="mt-2 text-sm leading-5 text-muted">{start.nextAction}</p>
      <dl className="mt-4 space-y-3 text-xs"><Fingerprint label="Plan fingerprint" value={start.planFingerprint} /><Fingerprint label="Planning context" value={start.contextFingerprint} /><div><dt className="text-faint">Authorization</dt><dd className="mt-1 font-medium text-ink">{start.authorization}</dd></div></dl>
      {start.blockers.length > 0 && <div className="mt-4 rounded-xl bg-warn-soft p-3"><p className="text-sm font-semibold text-warn">Blocked</p><ul className="mt-1 list-disc pl-4 text-xs leading-5 text-warn">{start.blockers.map((blocker) => <li key={blocker}>{blocker}</li>)}</ul></div>}
      {review.ready && <div className="mt-5 border-t border-line pt-4"><label className="block text-xs font-medium text-ink">Type the exact plan fingerprint to confirm<input aria-label="Exact plan fingerprint confirmation" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} placeholder={start.planFingerprint} className="mt-2 w-full rounded-lg border border-line bg-surface px-2.5 py-2 font-mono text-[11px] text-ink outline-none focus:border-accent" /></label><button type="button" onClick={onStart} disabled={!canStart} className={cn("mt-3 flex w-full items-center justify-center gap-2 rounded-lg px-3 py-2.5 text-sm font-semibold", canStart ? "bg-accent text-white hover:opacity-90" : "cursor-not-allowed bg-hover text-faint")}><Play size={14} />{starting ? "Starting delivery…" : "Start exact delivery"}</button><p className="mt-2 text-[11px] leading-4 text-faint">This cannot enable automation, start a daemon, satisfy a gate, authorize a release, or approve spend.</p></div>}
      {startResult && <StateCard tone="pass" title={startResult.replayed ? "Already started" : "Delivery started"} detail={`Wave ${startResult.waveId} is ${startResult.replayed ? "already armed for this fingerprint." : "armed for this exact fingerprint."}`} link={startResult.statusLink} />}
      {startError && <DeliveryFailure error={startError} compact />}
    </aside>
  </div>;
}

function ReviewSection({ number, title, children }: { number: string; title: string; children: ReactNode }) { return <section className="rounded-2xl border border-line bg-surface p-4 sm:p-5"><div className="mb-4 flex items-baseline gap-2"><span className="font-mono text-xs text-faint">{number}</span><h2 className="text-lg font-semibold text-ink">{title}</h2></div>{children}</section>; }
function List({ label, values }: { label: string; values: string[] }) { return values.length ? <p className="mt-2 text-xs leading-5 text-muted"><span className="font-medium text-ink-soft">{label}:</span> {values.join(" · ")}</p> : null; }
function Fingerprint({ label, value }: { label: string; value?: string }) { return <div><dt className="text-faint">{label}</dt><dd className="mt-1 break-all font-mono text-[10px] text-ink">{value || "Not recorded"}</dd></div>; }
function DeliveryFailure({ error, compact = false }: { error: unknown; compact?: boolean }) { const delivery = error instanceof DeliveryError ? error.problem.error : undefined; const stale = error instanceof DeliveryError && error.status === 409; return <StateCard tone="fail" compact={compact} title={stale ? "Review is stale" : "Delivery could not start"} detail={delivery?.message ?? (error instanceof Error ? error.message : "Please review the delivery state and try again.")} />; }
function StateCard({ tone, title, detail, link, compact = false }: { tone: "info" | "pass" | "fail"; title: string; detail: string; link?: string; compact?: boolean }) { const Icon = tone === "pass" ? CheckCircle2 : tone === "fail" ? CircleAlert : ShieldCheck; const style = tone === "pass" ? "bg-pass-soft text-pass" : tone === "fail" ? "bg-fail-soft text-fail" : "bg-info-soft text-info"; return <div className={cn("mt-4 rounded-xl p-3", style, compact && "text-xs")}><div className="flex gap-2"><Icon size={16} className="mt-0.5 shrink-0" /><div><p className="text-sm font-semibold">{title}</p><p className="mt-1 text-xs leading-5">{detail}</p>{link && <a href={link} className="mt-2 inline-block text-xs font-semibold underline">See delivery status</a>}</div></div></div>; }
