import { useEffect, useState, type ReactNode } from "react";
import { useParams } from "@tanstack/react-router";
import { CheckCircle2, CircleAlert, Play, RefreshCw, ShieldCheck } from "lucide-react";
import { PageScroll } from "@/components/ui/page";
import { DeliveryError } from "@/lib/api";
import { useDeliveryPlans, useDeliveryReview, useDeliveryStart } from "@/lib/queries";
import { cn } from "@/lib/cn";
import type { DeliveryCrossScopeDependency, DeliveryPlanSummary, DeliveryReview, DeliveryStartResult } from "@/types/domain";

const defaultPlan = "";

export function DeliveryReviewPage() {
  const projectId = useParams({ strict: false }).projectId as string;
  const [plan, setPlan] = useState(defaultPlan);
  const [submittedPlan, setSubmittedPlan] = useState(defaultPlan);
  const [confirmation, setConfirmation] = useState("");
  const plans = useDeliveryPlans(projectId);
  const review = useDeliveryReview(submittedPlan, projectId);
  const start = useDeliveryStart(projectId);
  const data = review.data;
  const reviewedFingerprint = data?.startBoundary.planFingerprint;
  const reviewedIdentity = data?.startBoundary.planIdentity;
  const inputMatchesReview = plan.trim() === submittedPlan;
  const mutationMatchesReview =
    plan.trim() === submittedPlan &&
    start.variables?.plan === submittedPlan &&
    start.variables?.confirm === reviewedFingerprint &&
    start.variables?.planIdentity === reviewedIdentity;

  useEffect(() => {
    start.reset();
    setConfirmation("");
  }, [plan, submittedPlan, reviewedFingerprint, reviewedIdentity, start.reset]);

  const refresh = () => {
    if (!plan.trim()) return;
    setSubmittedPlan(plan.trim());
  };

  const choosePlan = (candidate: DeliveryPlanSummary) => {
    if (candidate.state !== "available") return;
    setPlan(candidate.path);
    setSubmittedPlan(candidate.path);
  };

  return (
    <PageScroll>
      <main className="mx-auto w-full max-w-6xl" data-delivery-review-page>
        <header className="mb-6 border-b border-line pb-5">
          <div>
            <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-accent">Delivery</p>
            <h1 className="mt-1 font-serif text-3xl font-semibold tracking-tight text-ink">{data?.title || "Choose work to deliver"}</h1>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">{data?.summary || "Choose a reviewed plan. Tusker will show its scope, routing, and proof before you authorize anything."}</p>
            <p className="mt-2 text-xs font-medium text-faint">This review is read-only.</p>
          </div>
        </header>

        <section className="mb-5 rounded-[3px] border border-line bg-surface p-4 sm:p-5" aria-label="Delivery inbox">
          <div className="flex items-baseline justify-between gap-3">
            <div><p className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-accent">Ready for review</p><h2 className="mt-1 text-lg font-semibold text-ink">Delivery inbox</h2></div>
            {plans.data && <p className="text-xs text-faint">{plans.data.plans.length} plan{plans.data.plans.length === 1 ? "" : "s"}</p>}
          </div>
          {plans.isLoading && <p className="mt-4 text-sm text-muted">Finding project delivery plans…</p>}
          {plans.error && <StateCard tone="fail" title="Could not load delivery plans" detail={plans.error instanceof Error ? plans.error.message : "Refresh this project and try again."} />}
          {plans.data?.plans.length === 0 && <StateCard tone="info" title="No delivery plans yet" detail="When a planner writes a delivery plan, it will appear here. Nothing can start until you review and authorize it." />}
          {plans.data && plans.data.plans.length > 0 && <div className="mt-4 grid gap-3 md:grid-cols-2">{plans.data.plans.map((candidate) => <DeliveryPlanCard key={candidate.path} plan={candidate} selected={submittedPlan === candidate.path} onChoose={() => choosePlan(candidate)} />)}</div>}
          <details className="mt-4 border-t border-line pt-3">
            <summary className="cursor-pointer text-xs font-medium text-muted hover:text-ink">Open a plan by repository path</summary>
            <form onSubmit={(event) => { event.preventDefault(); refresh(); }} className="mt-3 flex w-full gap-2" aria-label="Advanced delivery plan path">
              <input aria-label="Repo-relative plan path" value={plan} onChange={(event) => setPlan(event.target.value)} placeholder=".tusker/scratch/delivery-plan.yaml" autoComplete="off" spellCheck={false} className="min-w-0 flex-1 rounded-[3px] border border-line bg-panel px-3 py-2 font-mono text-xs text-ink outline-none focus:border-accent" />
              <button type="submit" disabled={!plan.trim()} className="inline-flex items-center gap-1.5 rounded-[3px] border border-line px-3 py-2 text-sm font-medium text-ink hover:bg-hover disabled:cursor-not-allowed disabled:opacity-45"><RefreshCw size={14} />Review</button>
            </form>
          </details>
        </section>

        {!plan.trim() && <StateCard tone="info" title="Choose a delivery plan" detail="Select a reviewed plan from the inbox to inspect its exact scope and proof." />}
        {plan.trim() && !inputMatchesReview && <StateCard tone="info" title="Review this plan" detail="Choose Review to load the exact delivery contract for this path." />}
        {inputMatchesReview && review.isLoading && <StateCard tone="info" title="Loading delivery review" detail="Reading the canonical product projection." />}
        {inputMatchesReview && review.error && <DeliveryFailure error={review.error} phase="review" />}
        {inputMatchesReview && data && <ReviewBody review={data} confirmation={confirmation} setConfirmation={setConfirmation} startResult={mutationMatchesReview ? start.data : undefined} startError={mutationMatchesReview ? start.error : null} starting={mutationMatchesReview && start.isPending} onStart={() => start.mutate({ plan: submittedPlan, confirm: reviewedFingerprint ?? "", planIdentity: reviewedIdentity ?? "" })} />}
      </main>
    </PageScroll>
  );
}

function DeliveryPlanCard({ plan, selected, onChoose }: { plan: DeliveryPlanSummary; selected: boolean; onChoose: () => void }) {
  const available = plan.state === "available";
  return <button type="button" onClick={onChoose} disabled={!available} className={cn("rounded-xl border p-4 text-left transition shadow-2xs hover:shadow-xs", selected ? "border-accent bg-accent/5 ring-1 ring-accent/30" : "border-line bg-panel hover:border-ink/30", !available && "cursor-not-allowed opacity-60")}>
    <div className="flex items-start justify-between gap-3"><div><p className="text-sm font-semibold text-ink">{plan.title}</p><p className="mt-1 text-xs leading-5 text-muted">{plan.summary || `${plan.taskCount} task${plan.taskCount === 1 ? "" : "s"} prepared for review.`}</p></div><span className={cn("shrink-0 rounded-full px-2.5 py-0.5 font-mono text-[10.5px] font-medium", available ? "bg-info-soft text-info" : "bg-fail-soft text-fail")}>{available ? "Review" : "Needs repair"}</span></div>
    <div className="mt-3 flex flex-wrap gap-1.5 text-[11px] text-faint"><span className="rounded-md border border-line bg-surface px-2 py-0.5">{plan.taskCount} task{plan.taskCount === 1 ? "" : "s"}</span><span className="rounded-md border border-line bg-surface px-2 py-0.5">up to {plan.expectedConcurrency} at once</span>{plan.runnerProfile && <span className="rounded-md border border-line bg-surface px-2 py-0.5">{plan.runnerProfile}</span>}</div>
    {available && <ul className="mt-3 space-y-1 border-t border-line-soft pt-3 text-xs text-muted">{plan.tasks.slice(0, 3).map((task) => <li key={task.sourceKey}><span className="font-medium text-ink-soft">{task.title}</span>{task.runnerProfile && <span className="text-faint"> · {task.runnerProfile}</span>}</li>)}{plan.tasks.length > 3 && <li className="text-faint">+ {plan.tasks.length - 3} more</li>}</ul>}
    {!available && <p className="mt-3 border-t border-line-soft pt-3 text-xs leading-5 text-fail">{plan.issue || "This plan needs repair before review."}</p>}
  </button>;
}

function ReviewBody({ review, confirmation, setConfirmation, startResult, startError, starting, onStart }: { review: DeliveryReview; confirmation: string; setConfirmation: (value: string) => void; startResult?: DeliveryStartResult; startError: unknown; starting: boolean; onStart: () => void }) {
  const start = review.startBoundary;
  const reviewedFingerprint = start.planFingerprint;
  const canStart = review.ready && start.state === "held" && !!start.planIdentity && confirmation.trim() === reviewedFingerprint && !starting;
  const nextAction = starting ? "Wait while Tusker imports and authorizes this exact fingerprint." : start.nextAction;
  return <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_22rem]">
    <div className="grid gap-5">
      <ReviewSection number="1" title="What will be delivered">
        <div className="grid gap-3 sm:grid-cols-2">{review.whatWillBeDelivered.map((item) => <article key={item.requirement || item.outcome} className="rounded-[3px] border border-line bg-panel p-4"><p className="font-mono text-[10px] font-semibold text-faint">{item.links[0] ? <a href={item.links[0].href} className="hover:text-accent hover:underline">{item.requirement || "Outcome"}</a> : item.requirement || "Outcome"}</p><p className="mt-1 text-sm font-medium leading-5 text-ink">{item.outcome}</p><RecordLinks links={item.links} />{item.nonGoals.map((goal) => <p key={goal} className="mt-2 text-xs text-muted">Not included: {goal}</p>)}</article>)}</div>
        {review.nonGoals.length > 0 && <p className="text-sm text-muted">Not included: {review.nonGoals.join(" · ")}</p>}
      </ReviewSection>
      <ReviewSection number="2" title="How it will be proven"><div className="grid gap-3">{review.howItWillBeProven.map((proof) => <article key={proof.sourceKey} className="rounded-[3px] border border-line p-4"><p className="font-medium text-ink">{proof.taskHref ? <a href={proof.taskHref} className="hover:text-accent hover:underline">{proof.title}</a> : proof.title}</p><p className="mt-1 text-sm text-muted">{proof.outcome}</p><p className="mt-2 font-mono text-[10px] text-faint">Requirements: {proof.requirements.join(", ")}</p><p className="mt-2 text-xs text-muted">Acceptance: {proof.acceptance.join(" · ") || "No acceptance stated"}</p><div className="mt-2 space-y-1">{proof.checks.map((check) => <p key={`${check.covers}-${check.check}`} className="font-mono text-[11px] leading-5 text-muted"><span className="text-ink-soft">{check.covers}</span> · {check.href ? <a href={check.href} className="hover:text-accent hover:underline">{check.check}</a> : check.check}</p>)}</div><div className="mt-2 space-y-1">{proof.artifactRefs.map((artifact) => <p key={`${artifact.kind}-${artifact.path}`} className="text-xs text-muted">{artifact.href ? <a href={artifact.href} className="font-medium text-ink-soft hover:text-accent hover:underline">{artifact.summary}</a> : <span className="font-medium text-ink-soft">{artifact.summary}</span>} · <span className="font-mono text-[10px]">{artifact.path}</span> · covers {artifact.acceptanceIds.join(", ")}</p>)}</div>{(proof.resourceRefs ?? []).length > 0 && <List label="Shared resources" values={proof.resourceRefs ?? []} />}</article>)}</div></ReviewSection>
      <ReviewSection number="3" title="How work flows">
        <div className="grid gap-4 lg:grid-cols-2">
          <div>
            <p className="text-sm text-muted">{review.howWorkFlows.integration}</p>
            <p className="mt-2 text-sm text-ink">Expected concurrency: <strong>{review.howWorkFlows.expectedConcurrency}</strong></p>
            {review.howWorkFlows.waveHref && <a href={review.howWorkFlows.waveHref} className="mt-2 inline-block font-mono text-xs text-accent hover:underline">{review.howWorkFlows.waveId}</a>}
          </div>
          <ol className="space-y-2">{review.howWorkFlows.frontiers.map((frontier, index) => <li key={index} className="rounded-[3px] bg-panel px-3 py-2 text-sm text-ink"><span className="mr-2 font-mono text-xs text-faint">{index + 1}</span>{frontier.join(" → ")}</li>)}</ol>
        </div>
        {review.howWorkFlows.crossScopeDependencies.length > 0 && <CrossScopeDependencies dependencies={review.howWorkFlows.crossScopeDependencies} />}
        {review.howWorkFlows.sharedResources.length > 0 && <div className="mt-4"><p className="text-xs font-semibold uppercase tracking-wide text-faint">Shared resources</p><div className="mt-2 grid gap-2 sm:grid-cols-2">{review.howWorkFlows.sharedResources.map((resource) => <article key={resource.sourceKey} className="rounded-[3px] border border-line p-3"><p className="font-mono text-xs font-semibold text-ink">{resource.sourceKey}</p><p className="mt-1 text-xs text-muted">{resource.kind} · {resource.capacityStatus}{resource.capacity ? ` (${resource.capacity})` : ""}</p><RecordLinks links={resource.taskLinks} /><List label="Referenced by" values={resource.referencedBy} /><List label="Constraints" values={resource.constraints} /></article>)}</div></div>}
        {(review.howWorkFlows.warnings ?? []).map((warning) => <p key={warning} className="rounded-[3px] bg-warn-soft p-3 text-sm text-warn">{warning}</p>)}
      </ReviewSection>
      <ReviewSection number="4" title="What needs your decision">{review.whatNeedsYourDecision.length === 0 ? <p className="text-sm text-muted">No product decision is needed to start this exact delivery.</p> : <div className="grid gap-3">{review.whatNeedsYourDecision.map((decision) => <article key={decision.sourceKey || decision.title} className="rounded-[3px] border border-warn/30 bg-warn-soft p-4"><p className="font-medium text-ink">{decision.gateHref ? <a href={decision.gateHref} className="hover:underline">{decision.gateId} · {decision.title}</a> : decision.title}</p><p className="mt-1 text-sm text-muted">{decision.action}</p><p className="mt-2 text-xs text-warn">Why: {decision.why}</p>{decision.acceptanceIds.length > 0 && <List label="Acceptance" values={decision.acceptanceIds} />}{decision.verification && <p className="mt-2 text-xs text-muted">Verification: {decision.verification}</p>}</article>)}</div>}</ReviewSection>
    </div>
    <aside className="h-fit rounded-xl border border-line bg-panel p-4 shadow-2xs xl:sticky xl:top-4">
      <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-accent">5 · Start boundary</p>
      <h2 aria-live="polite" className="mt-1 text-lg font-semibold text-ink" data-delivery-state={starting ? "importing" : start.state}>{starting ? "Importing and authorizing" : start.stateLabel}</h2>
      <p className="mt-2 text-sm leading-5 text-muted">{nextAction}</p>
      {!starting && start.actionHref && <a href={start.actionHref} className="mt-2 inline-block text-xs font-semibold text-accent hover:underline">Open the canonical record</a>}
      <dl className="mt-4 space-y-3 text-xs"><Fingerprint label="Plan fingerprint" value={start.planFingerprint} /><Fingerprint label="Planning context" value={start.contextFingerprint} /><div><dt className="text-faint">Authorization</dt><dd className="mt-1 font-medium text-ink">{start.authorization}</dd></div></dl>
      {(start.blockers ?? []).length > 0 && <div className="mt-4 rounded-xl border border-warn/30 bg-warn-soft p-3"><p className="text-sm font-semibold text-warn">Blocked</p><ul className="mt-1 list-disc pl-4 text-xs leading-5 text-warn">{(start.blockers ?? []).map((blocker) => <li key={blocker}>{blocker}</li>)}</ul></div>}
      {review.ready && start.state === "held" && <div className="mt-5 border-t border-line pt-4"><label className="block text-xs leading-5 text-muted"><div className="flex items-center justify-between"><strong className="text-ink">Confirm the exact reviewed plan.</strong>{reviewedFingerprint && <button type="button" onClick={() => setConfirmation(reviewedFingerprint)} className="rounded border border-line bg-surface px-1.5 py-0.5 font-mono text-[10px] text-ink-soft hover:bg-hover hover:text-ink shadow-2xs">Fill fingerprint</button>}</div><span className="mt-1 block">Paste the fingerprint below. Tusker re-checks the plan, context, routing, and project opt-in before anything is armed.</span><input aria-label="Exact plan fingerprint confirmation" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} placeholder={reviewedFingerprint} spellCheck={false} className="mt-2.5 w-full rounded-lg border border-line bg-surface px-2.5 py-2 font-mono text-[10.5px] text-ink shadow-2xs outline-none focus:border-accent focus:ring-2 focus:ring-accent/20 transition-all" /></label><button type="button" onClick={onStart} disabled={!canStart} className={cn("mt-3 flex w-full items-center justify-center gap-2 rounded-lg px-3 py-2.5 text-sm font-semibold shadow-2xs transition-all active:scale-[0.99]", canStart ? "bg-ink text-surface hover:opacity-90" : "cursor-not-allowed bg-hover text-faint")}><Play size={14} />{starting ? "Starting delivery…" : "Start delivery"}</button><p className="mt-2 text-[11px] leading-4 text-faint">This cannot enable automation, start a daemon, satisfy a gate, authorize a release, or approve spend.</p><details className="mt-3 text-[11px] text-faint"><summary className="cursor-pointer">Technical receipt</summary><p className="mt-2 break-all font-mono">Plan version: {start.planFingerprint}</p><p className="mt-1 break-all font-mono">Planning context: {start.contextFingerprint || "Not recorded"}</p></details></div>}
      {startResult && <StateCard tone="pass" title={startResult.replayed ? "Already started" : "Delivery started"} detail={`Wave ${startResult.waveId} is ${startResult.replayed ? "already armed for this fingerprint." : "armed for this exact fingerprint."}`} link={startResult.statusLink} />}
      {startError != null && <DeliveryFailure error={startError} phase="start" compact />}
    </aside>
  </div>;
}

function CrossScopeDependencies({ dependencies }: { dependencies: DeliveryCrossScopeDependency[] }) {
  return <div className="mt-4" aria-label="Cross-scope hard dependencies">
    <p className="text-xs font-semibold uppercase tracking-wide text-faint">Producer must come first</p>
    <div className="mt-2 grid gap-2 sm:grid-cols-2">
      {dependencies.map((dependency) => <article key={`${dependency.consumerSourceKey}-${dependency.scope}-${dependency.sourceKey}-${dependency.taskId ?? "missing"}`} className="rounded-[3px] border border-line bg-panel p-3">
        <p className="font-mono text-xs font-semibold text-ink">{dependency.scope}/{dependency.sourceKey}</p>
        <p className="mt-1 text-xs text-muted">Hard dependency · {dependency.targetIntegrity} · producer {dependency.producerLifecycle}</p>
        <p className="mt-2 text-xs text-ink-soft">Durable target: {dependency.taskHref ? <a href={dependency.taskHref} className="font-mono text-accent hover:underline">{dependency.taskId}</a> : <span className="font-mono">{dependency.taskId || "missing"}</span>} · state {dependency.producerState}</p>
        <p className="mt-1 break-all font-mono text-[10px] text-faint">Persisted contract: {dependency.persistedContractFingerprint || "not recorded"} · {dependency.contractProvenance}</p>
        <p className="mt-2 text-xs leading-5 text-muted">{dependency.implication}</p>
        {dependency.blockerClass !== "none" && dependency.repair && <p className="mt-2 rounded-[3px] bg-warn-soft px-2 py-1.5 text-xs leading-5 text-warn"><span className="font-semibold">{dependency.blockerClass === "structural" ? "Repair structure:" : "Resolve producer:"}</span> {dependency.repair}</p>}
      </article>)}
    </div>
  </div>;
}

function ReviewSection({ number, title, children }: { number: string; title: string; children: ReactNode }) { return <section className="rounded-[3px] border border-line bg-surface p-4 sm:p-5"><div className="mb-4 flex items-baseline gap-2"><span className="font-mono text-xs text-faint">{number}</span><h2 className="text-lg font-semibold text-ink">{title}</h2></div>{children}</section>; }
function List({ label, values }: { label: string; values: string[] }) { return values.length ? <p className="mt-2 text-xs leading-5 text-muted"><span className="font-medium text-ink-soft">{label}:</span> {values.join(" · ")}</p> : null; }
function RecordLinks({ links }: { links: Array<{ label: string; href: string }> }) { return links.length ? <div className="mt-2 flex flex-wrap gap-1.5">{links.map((link) => <a key={`${link.label}-${link.href}`} href={link.href} className="rounded border border-line px-1.5 py-0.5 font-mono text-[10px] text-faint hover:bg-hover hover:text-accent">{link.label}</a>)}</div> : null; }
function Fingerprint({ label, value }: { label: string; value?: string }) { return <div><dt className="text-faint">{label}</dt><dd className="mt-1 break-all font-mono text-[10px] text-ink">{value || "Not recorded"}</dd></div>; }
function DeliveryFailure({ error, phase, compact = false }: { error: unknown; phase: "review" | "start"; compact?: boolean }) {
  const issue = error instanceof DeliveryError ? error.problem.error : undefined;
  const message = issue?.message ?? (error instanceof Error ? error.message : "");
  const stale = error instanceof DeliveryError && error.status === 409 && /\b(stale|changed|fingerprint differs)\b/i.test(message);
  const conflict = error instanceof DeliveryError && error.status === 409;
  const invalid = error instanceof DeliveryError && error.status === 422;
  const context = issue?.context && typeof issue.context === "object" ? issue.context as Record<string, unknown> : undefined;
  const projection = context?.delivery_review ?? context?.delivery_start;
  const nextAction = projection && typeof projection === "object"
    ? ((projection as Record<string, unknown>).nextAction ??
      ((projection as Record<string, unknown>).startBoundary as Record<string, unknown> | undefined)?.nextAction)
    : undefined;
  const title = stale
    ? `Delivery ${phase} is stale`
    : conflict
      ? `Delivery ${phase} is blocked`
    : invalid
      ? `Delivery ${phase} is invalid`
      : `Delivery ${phase} failed`;
  const detail = [
    message || `The delivery ${phase} failed.`,
    issue?.hint ? `Hint: ${issue.hint}` : "",
    typeof nextAction === "string" ? `Next: ${nextAction}` : "",
  ].filter(Boolean).join(" ");
  return <StateCard tone="fail" compact={compact} title={title} detail={detail} />;
}
function StateCard({ tone, title, detail, link, compact = false }: { tone: "info" | "pass" | "fail"; title: string; detail: string; link?: string; compact?: boolean }) { const Icon = tone === "pass" ? CheckCircle2 : tone === "fail" ? CircleAlert : ShieldCheck; const style = tone === "pass" ? "bg-pass-soft text-pass" : tone === "fail" ? "bg-fail-soft text-fail" : "bg-info-soft text-info"; return <div className={cn("mt-4 rounded-[3px] p-3", style, compact && "text-xs")}><div className="flex gap-2"><Icon size={16} className="mt-0.5 shrink-0" /><div><p className="text-sm font-semibold">{title}</p><p className="mt-1 text-xs leading-5">{detail}</p>{link && <a href={link} className="mt-2 inline-block text-xs font-semibold underline">See delivery status</a>}</div></div></div>; }
