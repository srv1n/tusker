import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { LayoutGrid, List, Play, ShieldCheck, Trash2 } from "lucide-react";
import { cn } from "@/lib/cn";
import { harnessLabel } from "@/lib/harness";
import { RECOVERY_ACTION_LABEL, RECOVERY_EXPLANATION, RECOVERY_STATE_LABEL } from "@/lib/recovery";
import { QueryBoundary } from "@/components/ui/states";
import { ActionResultLine, useConfirm } from "@/components/ui/action-feedback";
import { api } from "@/lib/api";
import { useDiscardTask, useRecovery, useReviewBatch, useRun, useRuns, useTask, useTaskRoute, useTaskStart, useTasks } from "@/lib/queries";
import { Markdown } from "@/features/docs/Markdown";
import { AgentAccessApprovalList, HumanActionCard } from "@/features/human-action/HumanActionCard";
import { isBatchSelectable, projectLiveExecution } from "@/features/work/work-utils";
import { BatchBar, type BatchAction, type BatchItemResult, type BatchProgress, WaveReviewGroups } from "@/features/work/WaveReview";
import type { DiscardImpact, RunDetail, TaskCapsule, TaskDetail, TaskRoutePreview } from "@/types/domain";
import {
  phaseTone,
  ProductButton,
  ProductEmpty,
  ProductLabel,
  ProductLoading,
  ProductPage,
  ProductRow,
  ProductSection,
  ProductStatus,
  ProductUnavailable,
} from "@/features/product/shared";

const statusOrder = ["in_progress", "review", "ready", "blocked", "backlog", "done"] as const;
const TIERS = [["light", "Tier 1 · Light"], ["standard", "Tier 2 · Standard"], ["demanding", "Tier 3 · Demanding"]] as const;

export function tierLabel(workLevel?: string): string {
  return TIERS.find(([value]) => value === workLevel)?.[1] ?? workLevel ?? "Unclassified";
}

export function routeSummary(route?: TaskRoutePreview): string {
  if (!route?.profile) return `Blocked — ${route?.blockers.join("; ") || "configuration unavailable"}`;
  return [route.profile, route.model, route.effort, harnessLabel(route.harness)].filter(Boolean).join(" · ");
}
function accessLabel(route?: TaskRoutePreview) {
  const access = route?.resolved_access?.requested && typeof route.resolved_access.requested !== "string" ? route.resolved_access.requested : route?.access;
  if (!access) return route?.resolved_access?.state ? `Access ${route.resolved_access.state.replaceAll("_", " ")}` : "Access unavailable";
  return `${access.mode === "review_only" ? "Review only" : "Work in projects"} · Internet ${access.network ? "on" : "off"}${route?.resolved_access?.state ? ` · ${route.resolved_access.state.replaceAll("_", " ")}` : ""}`;
}
function actualRouteLabel(run?: RunDetail | null) { return run?.runnerProfile && run.model && run.runnerHarness ? [run.runnerProfile, run.model, run.runnerEffort, harnessLabel(run.runnerHarness)].filter(Boolean).join(" · ") : "Unavailable"; }

export function RouteFact({ label, route }: { label: string; route?: TaskRoutePreview }) {
  return <div><span className="block font-mono text-[9px] uppercase tracking-[0.12em] text-faint">{label}</span><span className="mt-1 block text-ink">{routeSummary(route)}</span><span className="mt-0.5 block text-[11px] text-muted">{accessLabel(route)}</span>{route?.source && <span className="block text-[10px] text-faint">Source: {route.source}{route.reason ? ` · ${route.reason}` : ""}</span>}{route?.fallbacks?.length ? <span className="block text-[10px] text-faint">Fallbacks: {route.fallbacks.join(" · ")}</span> : null}{route?.blockers?.length ? <span role="alert" className="mt-1 block text-[11px] text-warn">Blocked: {route.blockers.join("; ")}</span> : null}</div>;
}

export function routeBlockers(detail: Pick<TaskDetail, "effectiveExecute" | "effectiveReview">): string[] {
  const blockers: string[] = [];
  const check = (label: string, route?: TaskRoutePreview) => {
    if (!route) {
      blockers.push(`${label}: route preview unavailable`);
      return;
    }
    if (!route.profile) blockers.push(`${label}: profile unavailable`);
    for (const blocker of route.blockers) blockers.push(`${label}: ${blocker}`);
  };
  check("Worker", detail.effectiveExecute);
  check("Reviewer", detail.effectiveReview);
  return [...new Set(blockers)];
}

export function taskRunBlocker(detail: TaskDetail): string | undefined {
  const status = detail.rawStatus ?? detail.status;
  if (status === "review") return "This task is awaiting independent review.";
  if (status === "in_progress") return "This task is already in progress.";
  if (status === "done" || status === "cancelled" || status === "superseded") return `This task is ${status.replaceAll("_", " ")}.`;
  if (status !== "backlog" && status !== "ready" && status !== "rework") return `Task start accepts backlog, ready, or rework; this task is ${status.replaceAll("_", " ")}.`;
  if (detail.humanActions?.length || detail.humanAction) return "Complete the requested human action first.";
  if (detail.hasGate) return "Resolve the open gate first.";
  const dependencies = detail.deps.filter((dependency) => dependency.status !== "done");
  if (dependencies.length) return `Waiting for ${dependencies.map((dependency) => dependency.id).join(", ")}.`;
  const blockers = routeBlockers(detail);
  if (blockers.length) return `Choose a worker and reviewer model first: ${blockers.join("; ")}.`;
}

export function reviewOverrideNeedsReason({
  workLevel,
  effectiveWorkLevel,
  reviewLevel,
  initialWorkLevel,
  initialReviewLevel,
  authoredReviewReason,
  reviewReason,
}: {
  workLevel: string;
  effectiveWorkLevel?: string;
  reviewLevel: string;
  initialWorkLevel: string;
  initialReviewLevel: string;
  authoredReviewReason?: string;
  reviewReason: string;
}): boolean {
  const baseline = workLevel || effectiveWorkLevel || "";
  const override = Boolean(reviewLevel) && reviewLevel !== baseline;
  if (!override || reviewReason.trim()) return false;
  const classificationChanged = workLevel !== initialWorkLevel || reviewLevel !== initialReviewLevel;
  return Boolean(authoredReviewReason) || classificationChanged;
}

export function TaskRouting({ detail, run, projectId }: { detail: TaskDetail; run: RunDetail | null | undefined; projectId: string }) {
  const route = useTaskRoute(detail.id, projectId);
  const profiles = useQuery({ queryKey: ["models", projectId], queryFn: () => api.modelLevels(projectId) });
  const effectiveTier = detail.authoredWorkLevel ?? detail.effectiveExecute?.work_level ?? "standard";
  const [workLevel, setWorkLevel] = useState(effectiveTier);
  const [executeProfile, setExecuteProfile] = useState(detail.authoredExecuteProfile ?? "");
  const [reviewProfile, setReviewProfile] = useState(detail.authoredReviewProfile ?? "");
  useEffect(() => { setWorkLevel(effectiveTier); setExecuteProfile(detail.authoredExecuteProfile ?? ""); setReviewProfile(detail.authoredReviewProfile ?? ""); }, [effectiveTier, detail.authoredExecuteProfile, detail.authoredReviewProfile]);
  const profileOptions = Object.entries(profiles.data?.profiles ?? {}).filter(([name]) => !["disabled", "unavailable"].includes(profiles.data?.profile_states[name] ?? "")).map(([name, profile]) => ({ name, label: [profile.display_name || name, profile.harness, profile.model, profile.effort].filter(Boolean).join(" · ") }));
  const active = run?.outcome === "running";
  const tierLabel = TIERS.find(([value]) => value === workLevel)?.[1] ?? "Tier";
  const blockers = routeBlockers(detail);
  const update = (patch: { workLevel?: string; executeProfile?: string | null; reviewProfile?: string | null }) => route.mutate({ revision: detail.stateRevision ?? "", ...patch });
  const selectClass = "w-full min-w-0 rounded-md border border-line bg-surface px-3 py-2 text-ink";
  const defaultLabel = (lane: "worker" | "reviewer") => {
    const preview = lane === "worker" ? detail.effectiveExecute : detail.effectiveReview;
    return preview?.profile ? `Tier default — ${routeSummary(preview)}` : "Choose a model";
  };
  return <ProductSection title={active ? "Running with" : "Routing"}><div className="space-y-4 rounded-lg border border-line bg-panel p-4 text-[12px]">
    <label className="grid gap-1.5 text-muted">Tier<select aria-label="Task work tier" value={workLevel} onChange={(event) => { const value = event.target.value; setWorkLevel(value); update({ workLevel: value }); }} disabled={route.isPending || !detail.stateRevision} className={selectClass}>{TIERS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <label className="grid gap-1.5 text-muted">Worker<select aria-label="Task worker profile" value={executeProfile} onChange={(event) => { const value = event.target.value; setExecuteProfile(value); update({ executeProfile: value || null }); }} disabled={route.isPending || !detail.stateRevision} className={selectClass}><option value="">{defaultLabel("worker")}</option>{executeProfile && !profileOptions.some((profile) => profile.name === executeProfile) && <option value={executeProfile}>{executeProfile} · unavailable</option>}{profileOptions.map((profile) => <option key={profile.name} value={profile.name}>{profile.label}</option>)}</select></label>
    <label className="grid gap-1.5 text-muted">Reviewer<select aria-label="Task reviewer profile" value={reviewProfile} onChange={(event) => { const value = event.target.value; setReviewProfile(value); update({ reviewProfile: value || null }); }} disabled={route.isPending || !detail.stateRevision} className={selectClass}><option value="">{defaultLabel("reviewer")}</option>{reviewProfile && !profileOptions.some((profile) => profile.name === reviewProfile) && <option value={reviewProfile}>{reviewProfile} · unavailable</option>}{profileOptions.map((profile) => <option key={profile.name} value={profile.name}>{profile.label}</option>)}</select></label>
    {blockers.length > 0 && <div role="alert" className="rounded-md bg-warn-soft px-3 py-2 text-warn"><p><strong>Play is blocked for {tierLabel}.</strong></p><ul className="mt-1 list-disc pl-5">{blockers.map((blocker) => <li key={blocker}>{blocker}</li>)}</ul><a href={`/p/${encodeURIComponent(projectId)}/settings`} className="mt-2 inline-block font-semibold underline">Open project settings</a></div>}
    <div className="grid gap-4 border-t border-line-soft pt-3 sm:grid-cols-2"><RouteFact label="Will execute" route={detail.effectiveExecute} /><RouteFact label="Will review" route={detail.effectiveReview} /></div>
    {active && <p className="border-t border-line-soft pt-3 text-ink"><strong>Current {run?.lane === "review" ? "reviewer" : "worker"}:</strong> {actualRouteLabel(run)}</p>}
    <ActionResultLine pending={route.isPending} error={route.error} result={route.data} />
  </div></ProductSection>;
}

/** Shared identity/coordination summary for the task and inspector surfaces. */
export function AgentCoordinationSummary({ task, run }: { task: TaskDetail; run?: RunDetail | null }) {
  const provenance = task.authoringProvenance;
  const bindings = task.contactBindings ?? [];
  const humanActions = [...(task.humanActions ?? []), ...(task.humanAction ? [task.humanAction] : [])].filter((action, index, all) => all.findIndex((candidate) => candidate.gateId === action.gateId) === index);
  const [claimCopied, setClaimCopied] = useState(false);
  const claimCommand = `tusker work start ${task.id} --by current-conversation --current-workspace`;
  const binding = run?.identity
    ? [run.identity.workspace_mode, run.identity.workspace_path, run.identity.branch ? `branch ${run.identity.branch}` : ""].filter(Boolean).join(" · ")
    : "No attempt binding observed";
  const executor = run
    ? run.handRun ? "Interactive implementation" : routeSummary({ profile: run.runnerProfile, model: run.model, effort: run.runnerEffort ?? "", harness: run.runnerHarness, blockers: [] })
    : "No attempt yet";
  const attempt = run ? `${run.lane} · ${run.outcome.replaceAll("-", " ")} · attempt ${run.attemptCount}` : "No attempt yet";
  return <dl className="space-y-2 text-[12px] leading-5">
    <div className="flex gap-3"><dt className="w-24 flex-none font-mono text-[9px] uppercase tracking-[0.12em] text-faint">Architect</dt><dd className="break-all text-ink">{task.architect || "Unbound provenance"}</dd></div>
    <div className="flex gap-3"><dt className="w-24 flex-none font-mono text-[9px] uppercase tracking-[0.12em] text-faint">Origin</dt><dd className="break-all text-ink">{task.origin || "Unbound provenance"}</dd></div>
    {provenance && <div className="flex gap-3"><dt className="w-24 flex-none font-mono text-[9px] uppercase tracking-[0.12em] text-faint">Authoring</dt><dd className="break-all text-muted">{[provenance.source, provenance.conversation_id, provenance.host].filter(Boolean).join(" · ")}</dd></div>}
    <div className="flex gap-3"><dt className="w-24 flex-none font-mono text-[9px] uppercase tracking-[0.12em] text-faint">Executor</dt><dd className="break-all text-ink">{executor}</dd></div>
    <div className="flex gap-3"><dt className="w-24 flex-none font-mono text-[9px] uppercase tracking-[0.12em] text-faint">Attempt</dt><dd className="break-all text-muted">{attempt}</dd></div>
    <div className="flex gap-3"><dt className="w-24 flex-none font-mono text-[9px] uppercase tracking-[0.12em] text-faint">Binding</dt><dd className="break-all text-muted">{binding}</dd></div>
    <div className="border-t border-line-soft pt-2"><dt className="font-mono text-[9px] uppercase tracking-[0.12em] text-faint">Current conversation</dt><dd className="mt-1 text-muted">{humanActions.length ? <p>Self implementation is unavailable while the named human action is open.</p> : <><p>Self implementation is a separate claim from configured Play. Run this in the trusted current Codex or Claude session when this conversation should own the implementation.</p><code className="mt-2 block break-all rounded border border-line bg-surface px-2 py-1.5 font-mono text-[10.5px] text-ink">{claimCommand}</code><button type="button" className="mt-2 border border-line bg-surface px-2.5 py-1.5 text-[11px] font-semibold text-ink hover:bg-hover" onClick={() => { const clipboard = navigator.clipboard; if (!clipboard) { setClaimCopied(false); return; } void clipboard.writeText(claimCommand).then(() => setClaimCopied(true)).catch(() => setClaimCopied(false)); }} aria-label="Copy current conversation self-claim command">{claimCopied ? "Command copied" : "Copy self-claim command"}</button></>}</dd></div>
    {(task.contacts?.length || bindings.length || task.identityError) ? <div className="border-t border-line-soft pt-2"><dt className="font-mono text-[9px] uppercase tracking-[0.12em] text-faint">Contacts and bindings</dt><dd className="mt-1 space-y-1">{bindings.length ? bindings.map((binding) => <div key={`${binding.contact.role}:${binding.contact.name || ""}:${binding.contact.address.id}`} className="break-all text-muted">{binding.contact.role === "peer" ? binding.contact.name || "Peer" : binding.contact.role}: {binding.contact.address.kind} · {binding.contact.address.id} · {binding.state}{binding.provider ? ` · ${binding.provider}` : ""}{binding.reason ? ` · ${binding.reason}` : ""}</div>) : task.contacts?.map((contact) => <div key={`${contact.role}:${contact.name || ""}:${contact.address.id}`} className="break-all text-muted">{contact.role === "peer" ? contact.name || "Peer" : contact.role}: {contact.address.kind} · {contact.address.id} · binding unavailable</div>)}{task.identityError && <div className="text-warn">Binding status unavailable: {task.identityError}</div>}</dd></div> : null}
  </dl>;
}

/** Show the canonical authored contract while keeping structured status below it. */
export function TaskContractDisclosure({ body, projectId, open = false }: { body?: string; projectId: string; open?: boolean }) {
  if (!body?.trim()) return null;
  return (
    <details open={open} className="mb-6 rounded-lg border border-line bg-panel/40 px-4 py-3" aria-label="Full task contract">
      <summary className="cursor-pointer text-[12px] font-medium text-muted hover:text-ink">Full task contract</summary>
      <Markdown markdown={body} projectId={projectId} className="mt-4" />
    </details>
  );
}

function TaskDiscardControl({ task, projectId }: { task: TaskDetail; projectId: string }) {
  const discard = useDiscardTask(task.id, projectId);
  const confirm = useConfirm();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [impact, setImpact] = useState<DiscardImpact | null>(null);
  const [resolution, setResolution] = useState<"" | "detach" | "discard">("");

  const preview = () => discard.mutate(
    { dryRun: true },
    { onSuccess: (result) => {
      setImpact(result.discard ?? null);
      setResolution("");
    } },
  );

  const submit = async () => {
    if (!impact || !reason.trim() || (impact.requiresResolution && !resolution)) return;
    const downstream = resolution === "discard"
      ? `${impact.cascadeDependents.length} downstream task${impact.cascadeDependents.length === 1 ? "" : "s"} will also be discarded.`
      : resolution === "detach"
        ? `${impact.directDependents.length} direct dependency edge${impact.directDependents.length === 1 ? "" : "s"} will be detached.`
        : "No active downstream tasks depend on it.";
    const gates = impact.openGates.length
      ? ` ${impact.openGates.length} open gate${impact.openGates.length === 1 ? "" : "s"} will be made obsolete.`
      : "";
    if (!await confirm({
      title: `Discard ${task.id}`,
      body: `${downstream}${gates} Audit history will be preserved.`,
      confirmLabel: "Discard task",
      tone: "danger",
      typeToConfirm: task.id,
    })) return;
    discard.mutate(
      { reason: reason.trim(), dependents: resolution || undefined },
      { onSuccess: () => void navigate({ to: "/p/$projectId/tasks", params: { projectId } }) },
    );
  };

  return (
    <ProductSection title="Danger zone">
      {!open ? (
        <ProductButton tone="danger" className="w-full" onClick={() => setOpen(true)}>
          <Trash2 size={13} /> Discard task
        </ProductButton>
      ) : (
        <div className="space-y-3 rounded-lg border border-fail/30 bg-fail-soft p-3 text-[12px]">
          <p className="leading-5 text-muted">Remove this task from active work without deleting its audit history.</p>
          <input
            aria-label="Discard reason"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder="Why is this work being discarded?"
            className="w-full rounded-lg border border-line bg-surface px-3 py-2 text-ink"
          />
          {!impact ? (
            <ProductButton tone="danger" className="w-full" disabled={discard.isPending || !reason.trim()} onClick={preview}>
              Review discard impact
            </ProductButton>
          ) : (
            <>
              <p className="font-mono text-[10.5px] text-faint">
                {impact.directDependents.length} direct dependent{impact.directDependents.length === 1 ? "" : "s"} · {impact.openGates.length} open gate{impact.openGates.length === 1 ? "" : "s"}
              </p>
              {impact.directDependents.length > 0 && <p className="text-muted">{impact.directDependents.map((item) => item.id).join(", ")}</p>}
              {impact.requiresResolution && (
                <select
                  aria-label="Resolve downstream dependencies"
                  value={resolution}
                  onChange={(event) => setResolution(event.target.value as "" | "detach" | "discard")}
                  className="w-full rounded-lg border border-line bg-surface px-3 py-2 text-ink"
                >
                  <option value="">Resolve downstream tasks…</option>
                  <option value="detach">Keep dependents and detach this prerequisite</option>
                  <option value="discard">Discard the downstream closure too</option>
                </select>
              )}
              <ProductButton tone="danger" className="w-full" disabled={discard.isPending || (impact.requiresResolution && !resolution)} onClick={submit}>
                Discard task
              </ProductButton>
            </>
          )}
          <ActionResultLine pending={discard.isPending} error={discard.error} result={discard.data} />
          <ProductButton tone="text" className="w-full" disabled={discard.isPending} onClick={() => { setOpen(false); setImpact(null); setResolution(""); }}>
            Cancel
          </ProductButton>
        </div>
      )}
    </ProductSection>
  );
}

function columnLabel(status: (typeof statusOrder)[number]) {
  return status === "in_progress" ? "Working now" : status.replaceAll("_", " ");
}

function statusCopy(task: TaskCapsule): string {
  if (task.liveRun) return "Building now";
  if (task.openGates?.length) return "Waiting for your decision";
  switch (task.status) {
    case "review":
      return "Checking the work";
    case "ready":
      return task.readiness === "ready" ? "Ready to start" : "Waiting for prerequisites";
    case "blocked":
      return "Blocked";
    case "done":
      return "Delivered";
    case "in_progress":
      return "Building";
    default:
      return "Planned";
  }
}

function TaskLink({ projectId, task }: { projectId: string; task: TaskCapsule }) {
  const label = statusCopy(task);
  const priorityTone =
    task.priority === "p0"
      ? "text-fail bg-fail-soft border-fail/30"
      : task.priority === "p1"
        ? "text-warn bg-warn-soft border-warn/30"
        : "text-muted bg-panel border-line";

  return (
    <Link
      to="/p/$projectId/tasks/$taskId"
      params={{ projectId, taskId: task.id }}
      className="group block rounded-xl border border-line bg-raised p-3.5 shadow-2xs transition-all hover:border-line hover:shadow-xs active:scale-[0.99]"
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="font-mono text-[10.5px] font-medium text-faint">{task.id}</span>
        <ProductStatus tone={phaseTone(label)}>{label}</ProductStatus>
      </div>
      <div className="text-[13.5px] font-semibold leading-snug text-ink group-hover:text-accent transition-colors">
        {task.title}
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px]">
        {task.epicId && (
          <span className="rounded bg-panel px-1.5 py-0.5 font-mono text-[10px] text-muted">
            {task.epicId}
          </span>
        )}
        <span className={cn("rounded border px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wider", priorityTone)}>
          {task.priority.toUpperCase()}
        </span>
        <span className="text-faint font-mono text-[10.5px]">{task.risk} risk</span>
      </div>
    </Link>
  );
}

export function Tasks() {
  const { projectId } = useParams({ strict: false }) as { projectId: string };
  const tasks = useTasks(projectId);
  const runs = useRuns(projectId);
  const reviewBatch = useReviewBatch(projectId);
  const qc = useQueryClient();
  const confirm = useConfirm();
  const [view, setView] = useState<"board" | "list">("board");
  const [epic, setEpic] = useState("all");
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set());
  const [progress, setProgress] = useState<BatchProgress | null>(null);
  const [results, setResults] = useState<{ action: BatchAction; items: BatchItemResult[] } | null>(null);
  const running = progress !== null;

  useEffect(() => {
    setSelectedIds(new Set());
    setResults(null);
  }, [projectId]);

  // Runtime ownership is projected into the board from fresh leases. It is
  // intentionally not written back as a durable task lifecycle transition.
  const all = useMemo(
    () => projectLiveExecution(tasks.data ?? [], runs.data ?? []),
    [tasks.data, runs.data],
  );
  const epics = useMemo(() => [...new Set(all.map((task) => task.epicId))].sort(), [all]);
  const filtered = epic === "all" ? all : all.filter((task) => task.epicId === epic);
  const selectedTasks = [...selectedIds]
    .map((id) => all.find((task) => task.id === id))
    .filter((task): task is TaskCapsule => task !== undefined && isBatchSelectable(task));
  const activeIds = selectedTasks.map((task) => task.id);
  const closeIds = selectedTasks.filter((task) => task.status === "review").map((task) => task.id);
  const landIds = selectedTasks.filter((task) => task.status === "done").map((task) => task.id);

  const runBatch = async (action: BatchAction, ids: string[]) => {
    setResults(null);
    setProgress({ action, done: 0, total: ids.length });
    const items: BatchItemResult[] = [];
    for (const id of ids) {
      try {
        const res = action === "close"
          ? await api.closeTask(id, {}, projectId)
          : await api.landTask(id, {}, projectId);
        items.push({ taskId: id, ok: res.ok && !res.refused, reason: res.reason });
      } catch (err) {
        items.push({ taskId: id, ok: false, reason: err instanceof Error ? err.message : "request failed" });
      }
      setProgress({ action, done: items.length, total: ids.length });
    }
    for (const key of ["projects", "needs", "tasks", "runs", "waves", "review", "gates", "evidence", "decisions", "feedback", "daemon"]) {
      void qc.invalidateQueries({ queryKey: [key] });
    }
    setSelectedIds(new Set());
    setProgress(null);
    setResults({ action, items });
  };

  return (
    <ProductPage
      title="Tasks"
      eyebrow={projectId}
      intro="The technical work behind each delivery. Runtime activity is projected from leases and attempts; it is never invented as a durable task state."
      wide
      actions={
        <div className="flex border border-line bg-raised">
          <ProductButton tone={view === "board" ? "primary" : "text"} className="rounded-none border-0" onClick={() => setView("board")}>
            <LayoutGrid size={13} /> Board
          </ProductButton>
          <ProductButton tone={view === "list" ? "primary" : "text"} className="rounded-none border-0" onClick={() => setView("list")}>
            <List size={13} /> List
          </ProductButton>
        </div>
      }
    >
      <div className="mb-8 flex flex-wrap items-center gap-2">
        <ProductLabel className="mr-2">Epic</ProductLabel>
        {["all", ...epics].map((value) => (
          <button
            key={value}
            type="button"
            onClick={() => setEpic(value)}
            className={`border px-3 py-1.5 text-[11px] font-medium ${
              epic === value ? "border-ink bg-ink text-surface" : "border-line bg-raised text-muted hover:border-ink"
            }`}
          >
            {value === "all" ? "All work" : value}
          </button>
        ))}
        <span className="ml-auto font-mono text-[10px] text-faint">{filtered.length} tasks</span>
      </div>

      <QueryBoundary q={reviewBatch} loading={null}>
        {(batch) => (
          <WaveReviewGroups
            batch={batch}
            disabled={running || tasks.isLoading || runs.isLoading}
            onSelectWave={(wave) => setSelectedIds(new Set(wave.members.filter(isBatchSelectable).map((task) => task.id)))}
          />
        )}
      </QueryBoundary>

      <QueryBoundary q={tasks} loading={<ProductLoading rows={5} />}>
        {() => <QueryBoundary q={runs} loading={<ProductLoading rows={5} />}>
          {() =>
          filtered.length === 0 ? (
            <ProductEmpty title="No tasks in this view" detail="Change the epic filter or author a task contract for this project." />
          ) : view === "list" ? (
            <div>
              {filtered
                .slice()
                .sort((a, b) => statusOrder.indexOf(a.status) - statusOrder.indexOf(b.status))
                .map((task) => (
                  <ProductRow
                    key={task.id}
                    meta={`${task.id} · ${task.epicId}`}
                    title={task.title}
                    detail={`${task.priority.toUpperCase()} · ${task.risk} risk · ${task.readiness.replaceAll("_", " ")}`}
                    status={<ProductStatus tone={phaseTone(statusCopy(task))}>{statusCopy(task)}</ProductStatus>}
                    action={
                      <Link
                        to="/p/$projectId/tasks/$taskId"
                        params={{ projectId, taskId: task.id }}
                        className="text-[12px] font-semibold text-ink underline underline-offset-4"
                      >
                        Open
                      </Link>
                    }
                  />
                ))}
            </div>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 items-start">
              {statusOrder.map((status) => {
                const column = filtered.filter((task) => task.status === status);
                return (
                  <section key={status} className="flex min-w-0 flex-col rounded-xl border border-line bg-panel/40 p-3 shadow-2xs">
                    <div className="mb-3 flex items-center justify-between pb-1">
                      <span className="text-[12.5px] font-semibold text-ink">{columnLabel(status)}</span>
                      <span className="rounded-full border border-line bg-surface px-2 py-0.5 font-mono text-[10.5px] font-semibold text-muted shadow-2xs">{column.length}</span>
                    </div>
                    <div className="flex flex-col gap-2.5">
                      {column.length === 0 ? (
                        <div className="rounded-lg border border-dashed border-line-soft bg-surface/50 py-7 text-center font-mono text-[11px] text-faint">
                          Empty
                        </div>
                      ) : (
                        column.map((task) => <TaskLink key={task.id} projectId={projectId} task={task} />)
                      )}
                    </div>
                  </section>
                );
              })}
            </div>
          )
          }
        </QueryBoundary>}
      </QueryBoundary>
      <BatchBar
        activeIds={activeIds}
        closeIds={closeIds}
        landIds={landIds}
        progress={progress}
        results={results}
        disabled={running}
        confirm={confirm}
        onRun={runBatch}
        onClearSelection={() => setSelectedIds(new Set())}
        onDismissResults={() => setResults(null)}
      />
    </ProductPage>
  );
}

export function TaskDetail() {
  const { projectId, taskId } = useParams({ strict: false }) as { projectId: string; taskId: string };
  const task = useTask(taskId, projectId);
  const run = useRun(taskId, false, projectId);
  const taskStart = useTaskStart(taskId, projectId);
  const recovery = useRecovery(taskId, projectId);

  return (
    <QueryBoundary q={task} loading={<div className="h-full bg-surface p-12"><ProductLoading rows={6} /></div>}>
      {(detail) => {
        const outcomeUnknown = run.data?.outcome === "outcome-unknown";
        const label = outcomeUnknown ? RECOVERY_STATE_LABEL : statusCopy(detail);
        const blockers = detail.deps.filter((dependency) => dependency.status !== "done");
        const humanActions = detail.humanActions?.length ? detail.humanActions : detail.humanAction ? [detail.humanAction] : [];
        const currentStatus = detail.rawStatus ?? detail.status;
        const runBlocker = taskRunBlocker(detail);
        const runnable = !runBlocker && !outcomeUnknown;
        const directiveQueued = detail.runDirective?.state === "queued";
        const busy = taskStart.isPending || recovery.isPending || directiveQueued;
        return (
          <ProductPage
            title={detail.title}
            eyebrow={`${projectId} / Tasks / ${detail.id}`}
            intro={detail.intent || "Task contract and objective proof."}
			actions={<div className="flex flex-wrap items-center justify-end gap-2"><ProductStatus tone={outcomeUnknown ? "warn" : phaseTone(label)}>{label}</ProductStatus>{outcomeUnknown ? <ProductButton tone="primary" disabled={busy} onClick={() => recovery.mutate("recover_unknown")} aria-label={`${RECOVERY_ACTION_LABEL} ${detail.id}`}><Play size={13} />{recovery.isPending ? "Verifying…" : RECOVERY_ACTION_LABEL}</ProductButton> : runnable && <ProductButton tone="primary" disabled={busy} onClick={() => taskStart.mutate()} aria-label={`Run task ${detail.id}`}><Play size={13} />{directiveQueued ? "Queued" : taskStart.isPending ? "Queuing…" : "Run task"}</ProductButton>}</div>}
          >
			<div className="mb-6 rounded-lg border border-line bg-panel px-4 py-3" aria-live="polite"><p className="text-[12px] text-muted">{outcomeUnknown ? RECOVERY_EXPLANATION : "Run queues this task. A paused wave stays paused."}</p>{!outcomeUnknown && runBlocker && <p role="status" className="mt-2 text-[12px] text-warn">{runBlocker}</p>}<ActionResultLine className="mt-2" pending={outcomeUnknown ? recovery.isPending : taskStart.isPending} error={outcomeUnknown ? recovery.error : taskStart.error} result={outcomeUnknown ? recovery.data : taskStart.data} />{detail.runDirective && <p className="mt-2 text-[11px] leading-4 text-muted">{detail.runDirective.state === "queued" && `Queued by ${detail.runDirective.actor} · expires ${detail.runDirective.expiresAt}`}{detail.runDirective.state === "lapsed" && (detail.runDirective.reason ?? "The queued run lapsed before dispatch.")}{detail.runDirective.state === "consumed" && `Claimed by ${detail.runDirective.actor}.`}</p>}</div>
            <TaskContractDisclosure body={detail.body} projectId={projectId} open />
            <TaskRouting detail={detail} run={run.data} projectId={projectId} />
            <AgentAccessApprovalList projectId={projectId} taskId={detail.id} approvals={detail.agentAccessApprovals} onRetry={() => taskStart.mutate()} />
            {humanActions.map((action) => <div key={action.gateId} className="space-y-2"><p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Owner: {detail.gates.find((gate) => gate.id === action.gateId)?.owner || "Human owner"}</p><HumanActionCard action={action} taskId={detail.id} taskTitle={detail.title} projectId={projectId} approvals={detail.agentAccessApprovals} onRetry={() => taskStart.mutate()} /></div>)}
            {!humanActions.length && blockers.length > 0 && <ProductUnavailable><strong>Blocked by prerequisites.</strong> {blockers.map((dependency) => `${dependency.id} (${dependency.status})`).join(" · ")}</ProductUnavailable>}

            <ProductSection title="Agent coordination">
              <AgentCoordinationSummary task={detail} run={run.data} />
              {detail.messages?.length ? <ol className="mt-4 space-y-2">{detail.messages.map((message) => <li key={message.id} className="rounded-lg border border-line bg-panel px-3 py-2.5"><p className="text-[12.5px] text-ink">{message.body}</p><p className="mt-1 font-mono text-[10px] text-faint">{message.kind.replaceAll("_", " ")} · {message.sender} · {message.transportState} · {message.state}{message.yieldSender ? " · sender yields" : ""}</p></li>)}</ol> : <p className="mt-3 text-[11.5px] text-faint">No questions or replies.</p>}
            </ProductSection>

            <div className="mt-10 grid gap-12 lg:grid-cols-[minmax(0,1fr)_320px]">
              <div>
                <ProductSection title="What we agreed" count={detail.acceptance.length}>
                  {detail.acceptance.map((row) => (
                    <div key={row.id} className="grid grid-cols-[52px_1fr_auto] items-start gap-4 border-b border-line-soft py-4">
                      <span className="font-mono text-[10px] text-faint">{row.id}</span>
                      <div className="text-[14px] leading-5 text-ink">{row.text}</div>
                      <ProductStatus tone={row.proof === "pass" ? "pass" : row.proof === "fail" ? "fail" : "neutral"}>{row.proof}</ProductStatus>
                    </div>
                  ))}
                </ProductSection>

                <ProductSection title="Proof" count={detail.verification.length}>
                  {detail.verification.map((row) => (
                    <div key={row.id} className="border-b border-line-soft py-4">
                      <div className="flex items-start gap-3">
                        <ShieldCheck size={16} className={row.result === "pass" ? "text-pass" : row.result === "fail" ? "text-fail" : "text-faint"} />
                        <code className="min-w-0 flex-1 break-words font-mono text-[11px] leading-5 text-ink-soft">{row.command}</code>
                        <ProductStatus tone={row.result === "pass" ? "pass" : row.result === "fail" ? "fail" : "neutral"}>{row.result}</ProductStatus>
                      </div>
                      {row.detail && <p className="ml-7 mt-2 text-[12px] leading-5 text-muted">{row.detail}</p>}
                    </div>
                  ))}
                </ProductSection>
              </div>

              <aside>
                <ProductSection title="Exact details">
                  <dl className="space-y-4 text-[12px]">
                    {[
                      ["Epic", `${detail.epicId} · ${detail.epicTitle}`],
                      ["Priority", detail.priority.toUpperCase()],
                      ["Risk", detail.risk],
                      ["Readiness", detail.readiness.replaceAll("_", " ")],
                      ["Updated", detail.updatedAt],
                    ].map(([name, value]) => (
                      <div key={name} className="border-b border-line-soft pb-3">
                        <dt className="mb-1 font-mono text-[9px] uppercase tracking-[0.12em] text-faint">{name}</dt>
                        <dd className="text-ink">{value}</dd>
                      </div>
                    ))}
                  </dl>
                </ProductSection>

                <ProductSection title="Runtime">
                  {run.isLoading ? (
                    <ProductLoading rows={2} />
                  ) : run.data ? (
                    <div className="space-y-3 text-[12px]">
                      <ProductStatus tone={phaseTone(run.data.outcome)}>{run.data.outcome.replaceAll("-", " ")}</ProductStatus>
                      <p className="leading-5 text-muted">
                        {run.data.runner} · {run.data.model} · attempt {run.data.attemptCount}
                      </p>
                      {run.data.workspacePath && <code className="block break-all font-mono text-[10px] leading-4 text-faint">{run.data.workspacePath}</code>}
                      <Link to="/p/$projectId/runs/$taskId" params={{ projectId, taskId }} className="inline-flex text-[12px] font-semibold text-info hover:text-ink">Open logs and result <span aria-hidden="true">→</span></Link>
                      {run.data.delivery?.summary && <p className="border-t border-line-soft pt-3 text-[12px] leading-5 text-ink">{run.data.delivery.summary}</p>}
                    </div>
                  ) : (
                    <p className="text-[12px] leading-5 text-muted">No runtime record exists for this task.</p>
                  )}
                </ProductSection>
                {!(["done", "cancelled", "superseded"] as string[]).includes(currentStatus) && <TaskDiscardControl key={detail.id} task={detail} projectId={projectId} />}
              </aside>
            </div>
          </ProductPage>
        );
      }}
    </QueryBoundary>
  );
}
