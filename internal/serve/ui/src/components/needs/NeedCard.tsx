import { useState, type KeyboardEvent, type MouseEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  ClipboardCheck,
  FileCheck2,
  HelpCircle,
  KeyRound,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { ProofChip } from "@/components/ui/chips";
import { relativeTime } from "@/lib/time";
import { gateKindLabel, gateKindTone } from "@/components/ui/tone";
import { useConfirm, ActionResultLine } from "@/components/ui/action-feedback";
import { HumanActionCard } from "@/features/human-action/HumanActionCard";
import {
  useAcknowledgeRun,
  useCloseTask,
  useGateAction,
  useRedrive,
  useTaskStatusAction,
} from "@/lib/queries";
import type {
  AcceptanceRow,
  ActionResult,
  FailedNeed,
  GateKind,
  NeedItem,
  RedriveResult,
  ReviewNeed,
} from "@/types/domain";

const kindIcon: Record<GateKind, LucideIcon> = {
  clarify: HelpCircle,
  provision: KeyRound,
  "approve-spec": FileCheck2,
  review: ClipboardCheck,
  failed: XCircle,
};

const priorityRank: Record<string, number> = { p0: 0, p1: 1, p2: 2, p3: 3 };

/** Rank: blocking-the-most first, then priority, then age (packet §4.1). */
export function rankNeeds(needs: NeedItem[]): NeedItem[] {
  return [...needs].sort(
    (a, b) =>
      b.blocking - a.blocking ||
      (priorityRank[a.priority] ?? 9) - (priorityRank[b.priority] ?? 9) ||
      new Date(a.since).getTime() - new Date(b.since).getTime(),
  );
}

const GATE_NEED_ID_PREFIX = "need-gate-";
const CARD_INTERACTIVE_TARGET = "a,button,input,textarea,select,label,[contenteditable='true']";

/**
 * The backing gate id for clarify/provision/approve-spec needs.
 *
 * Live needs carry gateId directly. The encoded-id fallback keeps old cached
 * payloads actionable across a rolling daemon/UI restart.
 */
function gateIdOf(need: NeedItem): string | null {
  return need.gateId ?? (need.id.startsWith(GATE_NEED_ID_PREFIX)
    ? need.id.slice(GATE_NEED_ID_PREFIX.length)
    : null);
}

const NO_GATE_ERROR = () =>
  new Error("This item has no gate id — resolve it from the task page.");

function countProof(rows: AcceptanceRow[], proof: AcceptanceRow["proof"]): number {
  return rows.filter((row) => row.proof === proof).length;
}

function plural(count: number, singular: string, pluralForm = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : pluralForm}`;
}

export function reviewProofSummary(rows: AcceptanceRow[]): string {
  if (rows.length === 0) return "No acceptance proof rows yet";
  const passed = countProof(rows, "pass");
  const failed = countProof(rows, "fail");
  const pending = countProof(rows, "pending");
  return [
    passed > 0 ? plural(passed, "passing check") : "",
    failed > 0 ? plural(failed, "failed check") : "",
    pending > 0 ? plural(pending, "pending check") : "",
  ]
    .filter(Boolean)
    .join(", ");
}

export function reviewDecisionLine(need: Pick<ReviewNeed, "acceptance">): string {
  const failed = countProof(need.acceptance, "fail");
  if (failed > 0) {
    return `${plural(failed, "acceptance check")} still failed. Send it back unless the detail page explains why that is acceptable.`;
  }
  const pending = countProof(need.acceptance, "pending");
  if (pending > 0) {
    return `${plural(pending, "acceptance check")} still needs proof. Inspect the task before accepting.`;
  }
  if (need.acceptance.length > 0) {
    return "All acceptance proof is passing. Inspect the task detail if you need the full context before accepting.";
  }
  return "No acceptance proof is attached yet. Inspect the task before closing it.";
}

export function failedNeedNextAction(need: Pick<FailedNeed, "attempts">): string {
  return `The latest run failed after ${plural(need.attempts, "attempt")}. Redrive only if the failure is understood; inspect the run or task detail first if it is not.`;
}

/**
 * A single needs-me card (packet §4.1). The card body opens the canonical task
 * detail; the action row keeps the fast-path gate/review mutations available.
 * Renders per gate kind; the left rail shows how much work it blocks.
 *
 * Every action calls the real mutation in lib/queries. The card only slides away
 * on a genuine success (ok && !refused); a refusal keeps the card and surfaces
 * the reason, a transport error keeps the card and shows the error.
 */
export function NeedCard({ need, showProject = false }: { need: NeedItem; showProject?: boolean }) {
  const navigate = useNavigate();
  const [resolving, setResolving] = useState(false);
  const [gone, setGone] = useState(false);
  const [checked, setChecked] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<ActionResult | RedriveResult | null>(null);
  const [error, setError] = useState<Error | null>(null);

  const askConfirm = useConfirm();
  const closeTask = useCloseTask(need.taskId, need.projectId);
  const statusAction = useTaskStatusAction(need.taskId, need.projectId);
  const redrive = useRedrive(need.taskId, need.projectId);
  const acknowledge = useAcknowledgeRun(need.taskId, need.projectId);
  const gateAction = useGateAction();

  if (gone) return null;

  if (need.humanAction) {
    return (
      <HumanActionCard
        action={need.humanAction}
        taskId={need.taskId}
        taskTitle={need.taskTitle}
        projectId={need.projectId}
        blockedTaskIds={need.blockedTaskIds}
      />
    );
  }

  const t = gateKindTone[need.kind];
  const Icon = kindIcon[need.kind];
  const colorVar = `var(--k-${t === "neutral" ? "faint" : t})`;
  const gateId = gateIdOf(need);

  /** Trigger the existing slide-away animation, then unmount the card. */
  function slideAway() {
    setResolving(true);
    setTimeout(() => setGone(true), 380);
  }

  /**
   * Run a mutation and route its outcome: transport failure → error, in-body
   * refusal → keep card + show reason, real success → slide away.
   */
  async function run(action: () => Promise<ActionResult | RedriveResult>) {
    if (pending) return;
    setError(null);
    setResult(null);
    setPending(true);
    try {
      const res = await action();
      setResult(res);
      if (res.ok && !res.refused) slideAway();
    } catch (e) {
      setError(e instanceof Error ? e : new Error(String(e)));
    } finally {
      setPending(false);
    }
  }

  async function onPrimary() {
    if (pending) return;
    switch (need.kind) {
      case "review": {
        const ok = await askConfirm({
          title: "Accept & close this task?",
          body: `${need.taskId} — ${need.taskTitle}. This closes the task as accepted.`,
          confirmLabel: "Accept & close",
          cancelLabel: "Keep reviewing",
        });
        if (!ok) return;
        await run(() => closeTask.mutateAsync({ reason: "Accepted from the needs-me review inbox." }));
        return;
      }
      case "approve-spec": {
        if (!gateId) return void setError(NO_GATE_ERROR());
        await run(() =>
          gateAction.mutateAsync({
            gateId,
            action: "satisfy",
            body: { evidence: `Spec approved: ${need.specTitle}` },
            taskId: need.taskId,
            projectId: need.projectId,
          }),
        );
        return;
      }
      case "provision": {
        if (!checked) return; // primary is disabled until "I've set it" is ticked
        if (!gateId) return void setError(NO_GATE_ERROR());
        await run(() =>
          gateAction.mutateAsync({
            gateId,
            action: "satisfy",
            body: { evidence: "Provisioned by operator; resuming." },
            taskId: need.taskId,
            projectId: need.projectId,
          }),
        );
        return;
      }
      case "clarify":
        // Primary just opens the answer box; the send button runs the mutation.
        setComposeOpen((v) => !v);
        return;
      case "failed":
        await run(() => redrive.mutateAsync());
        return;
    }
  }

  /** Secondary for review / approve-spec: open the note box; send runs the bounce. */
  function onSecondary() {
    if (pending) return;
    setComposeOpen((v) => !v);
  }

  /**
   * Acknowledge a settled failed run: retire the record so it clears from
   * attention everywhere. A confirm guards the terminal action; the shared run()
   * helper slides the card away on success and restores it (with the refusal
   * reason) if the run is still active.
   */
  async function onAcknowledge() {
    if (pending) return;
    const ok = await askConfirm({
      title: "Acknowledge this failed run?",
      body: `${need.taskId} — ${need.taskTitle}. This retires the run and clears it from attention.`,
      confirmLabel: "Acknowledge",
      cancelLabel: "Keep it",
    });
    if (!ok) return;
    await run(() => acknowledge.mutateAsync());
  }

  /** The compose box send button — clarify answer, or a review/spec send-back. */
  async function onSendCompose() {
    if (pending) return;
    if (need.kind === "clarify") {
      const answer = draft.trim();
      if (!answer) return;
      if (!gateId) return void setError(NO_GATE_ERROR());
      await run(() =>
        gateAction.mutateAsync({
          gateId,
          action: "satisfy",
          body: { evidence: answer },
          taskId: need.taskId,
          projectId: need.projectId,
        }),
      );
      return;
    }

    // review "Send back" / approve-spec "Request changes" → bounce to rework.
    const note = draft.trim();
    const isSpec = need.kind === "approve-spec";
    const ok = await askConfirm({
      title: isSpec ? "Request changes on this spec?" : "Send this task back for rework?",
      body: note ? undefined : "No note added — send it back anyway?",
      confirmLabel: isSpec ? "Request changes" : "Send back",
      cancelLabel: "Keep reviewing",
    });
    if (!ok) return;
    await run(() =>
      statusAction.mutateAsync({
        status: "rework",
        reason: note || (isSpec ? "Changes requested on spec." : "Sent back for rework."),
      }),
    );
  }

  const primaryDisabled = pending || (need.kind === "provision" && !checked);
  const sendDisabled = pending || (need.kind === "clarify" && draft.trim() === "");

  function openTaskDetail() {
    void navigate({
      to: "/p/$projectId/docs",
      params: { projectId: need.projectId },
      search: { path: need.taskId },
    });
  }

  function onCardClick(event: MouseEvent<HTMLDivElement>) {
    const target = event.target instanceof Element ? event.target : null;
    if (target?.closest(CARD_INTERACTIVE_TARGET)) return;
    if (window.getSelection()?.toString()) return;
    openTaskDetail();
  }

  function onCardKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.target !== event.currentTarget || (event.key !== "Enter" && event.key !== " ")) return;
    event.preventDefault();
    openTaskDetail();
  }

  return (
    <div
      data-need-card
      data-need-focus-target
      role="article"
      tabIndex={0}
      aria-label={`${gateKindLabel[need.kind]} · ${need.taskId} · ${need.taskTitle}`}
      aria-keyshortcuts="Enter Space"
      onClick={onCardClick}
      onKeyDown={onCardKeyDown}
      className={cn(
        "grid cursor-pointer grid-cols-[92px_1fr] gap-4 rounded-lg border-b border-line px-3 pb-6 pt-[22px] outline-none transition-all duration-[380ms] ease-out hover:bg-hover/30 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent/40 sm:gap-6",
        resolving && "pointer-events-none translate-y-[-6px] opacity-0",
      )}
    >
      {/* Left rail: how much this blocks + kind */}
      <div
        className="block border-l-[3px] pl-[15px]"
        style={{ borderColor: colorVar }}
      >
        <div
          className="font-serif text-[38px] font-semibold leading-[0.9] tracking-[-0.03em]"
          style={{ color: need.blocking > 0 ? colorVar : "var(--k-faint)" }}
        >
          {need.blocking}
        </div>
        <div className="mt-[7px] whitespace-pre-line font-mono text-[9px] uppercase leading-[1.5] tracking-[0.1em] text-faint">
          {need.blocking === 1 ? "task\nblocked" : "tasks\nblocked"}
        </div>
        <div
          className="mt-[13px] flex items-center gap-1 font-mono text-[9px] font-semibold uppercase tracking-[0.1em]"
          style={{ color: colorVar }}
        >
          <Icon size={11} />
          {gateKindLabel[need.kind]}
        </div>
      </div>

      {/* Right: meta, title, body, actions */}
      <div className="min-w-0">
        <div className="-m-2 rounded-lg p-2">
          <div className="mb-1 flex flex-wrap items-center gap-2.5">
            {showProject && (
              <span className="rounded-md bg-hover px-1.5 py-0.5 font-mono text-[10px] font-semibold text-ink-soft">
                ◇ {need.projectName}
              </span>
            )}
            <Mono className="text-[11px] text-faint">{need.taskId}</Mono>
            <Mono className="text-[11px] text-faint">
              {relativeTime(need.since)} · {need.priority}
            </Mono>
          </div>

          <h3 className="mb-2 font-serif text-[20px] font-semibold leading-tight tracking-[-0.015em] text-ink">
            {need.taskTitle}
          </h3>

          <NeedBody need={need} />
        </div>

        {need.kind === "provision" && (
          <label className="mt-3 flex cursor-pointer items-center gap-2.5 text-[13px] text-ink-soft">
            <input
              type="checkbox"
              checked={checked}
              onChange={(e) => setChecked(e.target.checked)}
              className="h-[15px] w-[15px] accent-accent"
            />
            I’ve set it
          </label>
        )}

        <div className="mt-4 flex flex-wrap items-center gap-3">
          <PrimaryAction
            need={need}
            colorVar={colorVar}
            disabled={primaryDisabled}
            onClick={onPrimary}
          />
          <SecondaryAction need={need} disabled={pending} onClick={onSecondary} />
          {need.kind === "failed" && (
            <button
              onClick={() => void onAcknowledge()}
              disabled={pending}
              className="rounded-lg border border-line px-3.5 py-2 text-[13px] font-medium text-muted transition-colors hover:border-fainter hover:text-ink-soft disabled:opacity-40"
            >
              Acknowledge
            </button>
          )}
        </div>

        {composeOpen && (
          <div className="mt-3 max-w-[66ch]">
            <textarea
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder={
                need.kind === "clarify"
                  ? "Type your answer — the agent resumes on send"
                  : "Add a note for the runner"
              }
              className="tk-scroll min-h-[78px] w-full resize-y rounded-lg border border-line bg-panel px-3 py-2.5 text-[14px] leading-relaxed text-ink outline-none focus:border-accent/50"
            />
            <div className="mt-2.5">
              <button
                onClick={() => void onSendCompose()}
                disabled={sendDisabled}
                className="rounded-lg px-3.5 py-2 text-[12.5px] font-semibold text-surface transition-opacity disabled:opacity-40"
                style={{ backgroundColor: colorVar }}
              >
                {need.kind === "clarify" ? "Send answer" : "Send back with note"}
              </button>
            </div>
          </div>
        )}

        <ActionResultLine
          pending={pending}
          error={error ?? undefined}
          result={result ?? undefined}
          className="mt-2.5"
        />
      </div>
    </div>
  );
}

function NeedBody({ need }: { need: NeedItem }) {
  const bodyCls = "font-serif text-[15.5px] leading-[1.55] text-ink-soft max-w-[66ch]";
  switch (need.kind) {
    case "clarify":
      return <p className={bodyCls}>{need.question}</p>;
    case "provision":
      return (
        <p className={bodyCls}>
          {need.ask}
          {need.path && (
            <>
              {" "}
              at <Mono className="text-[13px] text-ink">{need.path}</Mono>
            </>
          )}
          .
        </p>
      );
    case "approve-spec":
      return (
        <p className={bodyCls}>
          Spec <span className="font-semibold">{need.specTitle}</span> awaits approval before
          expensive work starts. <Mono className="text-[13px] text-muted">{need.specPath}</Mono>
        </p>
      );
    case "review":
      return (
        <div className="max-w-[66ch]">
          <p className={bodyCls}>Review whether this task is ready to close. {reviewDecisionLine(need)}</p>
          <div className="mt-2 font-mono text-[10.5px] uppercase tracking-[0.1em] text-fainter">
            Proof detail · {reviewProofSummary(need.acceptance)}
          </div>
          <div className="mt-2 overflow-hidden rounded-lg border border-line bg-panel/35">
            {need.acceptance.map((a, i) => (
              <div
                key={a.id}
                className={cn(
                  "flex items-start justify-between gap-3 px-3 py-2 text-[12.5px] leading-[1.45] text-muted",
                  i > 0 && "border-t border-line-soft",
                )}
              >
                <span>{a.text}</span>
                <ProofChip proof={a.proof} />
              </div>
            ))}
          </div>
        </div>
      );
    case "failed":
      return (
        <div className="max-w-[66ch]">
          <p className={bodyCls}>{failedNeedNextAction(need)}</p>
          <div className="mt-2 rounded-lg border border-fail/30 bg-fail-soft px-3 py-2 font-mono text-[12px] leading-relaxed text-fail">
            {need.lastError}
          </div>
          <p className="mt-2 text-[13px] text-muted">
            Next action: redrive the task, inspect the failed run, or open the task detail before deciding.
          </p>
        </div>
      );
  }
}

function PrimaryAction({
  need,
  colorVar,
  disabled,
  onClick,
}: {
  need: NeedItem;
  colorVar: string;
  disabled: boolean;
  onClick: () => void;
}) {
  const label =
    need.kind === "clarify"
      ? "Answer"
      : need.kind === "provision"
        ? "Resume"
        : need.kind === "approve-spec"
          ? "Approve spec"
          : need.kind === "review"
            ? "Accept & close"
            : "Redrive";
  return (
    <button
      onClick={() => void onClick()}
      disabled={disabled}
      className="rounded-lg px-4 py-2 text-[13px] font-semibold text-surface transition-opacity disabled:opacity-40"
      style={{ backgroundColor: colorVar }}
    >
      {label}
    </button>
  );
}

function SecondaryAction({
  need,
  disabled,
  onClick,
}: {
  need: NeedItem;
  disabled: boolean;
  onClick: () => void;
}) {
  if (need.kind === "review" || need.kind === "approve-spec") {
    return (
      <button
        onClick={() => void onClick()}
        disabled={disabled}
        className="rounded-lg border border-line px-3.5 py-2 text-[13px] font-medium text-ink-soft transition-colors hover:border-fainter disabled:opacity-40"
      >
        {need.kind === "review" ? "Send back" : "Request changes"}
      </button>
    );
  }
  if (need.kind === "failed") {
    return (
      <Link
        to="/p/$projectId/runs/$taskId"
        params={{ projectId: need.projectId, taskId: need.taskId }}
        className="rounded-lg border border-line px-3.5 py-2 text-[13px] font-medium text-ink-soft transition-colors hover:border-fainter"
      >
        Inspect run
      </Link>
    );
  }
  return null;
}
