import { useState } from "react";
import { Link } from "@tanstack/react-router";
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
import {
  useCloseTask,
  useGateAction,
  useRedrive,
  useTaskStatusAction,
} from "@/lib/queries";
import type { ActionResult, GateKind, NeedItem, RedriveResult } from "@/types/domain";

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

/**
 * The backing gate id for clarify/provision/approve-spec needs.
 *
 * SEAM / BACKEND-GAP: `NeedItem` does not yet carry a first-class `gateId`
 * field (see types/domain.ts). The needs feed encodes it into the need id as
 * `need-gate-<gateId>` (features/inbox/deriveNeeds.ts), so we parse it back out
 * here. When /api/needs surfaces `gateId` directly on the gate-kind variants,
 * read that field instead and delete this parse. If the id is not in that
 * shape we return null and the action refuses visibly rather than firing a
 * satisfy against the wrong gate.
 */
function gateIdOf(need: NeedItem): string | null {
  return need.id.startsWith(GATE_NEED_ID_PREFIX)
    ? need.id.slice(GATE_NEED_ID_PREFIX.length)
    : null;
}

const NO_GATE_ERROR = () =>
  new Error("This item has no gate id — resolve it from the task page.");

/**
 * A single needs-me card (packet §4.1). One human gate, actionable on the card:
 * the operator should clear most items without navigating. Renders per gate
 * kind; the left rail shows how much work it blocks (the primary ranking key).
 *
 * Every action calls the real mutation in lib/queries. The card only slides away
 * on a genuine success (ok && !refused); a refusal keeps the card and surfaces
 * the reason, a transport error keeps the card and shows the error.
 */
export function NeedCard({ need, showProject = false }: { need: NeedItem; showProject?: boolean }) {
  const [resolving, setResolving] = useState(false);
  const [gone, setGone] = useState(false);
  const [checked, setChecked] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<ActionResult | RedriveResult | null>(null);
  const [error, setError] = useState<Error | null>(null);

  const askConfirm = useConfirm();
  const closeTask = useCloseTask(need.taskId);
  const statusAction = useTaskStatusAction(need.taskId);
  const redrive = useRedrive(need.taskId);
  const gateAction = useGateAction();

  if (gone) return null;

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

  return (
    <div
      data-need-card
      role="article"
      aria-label={`${gateKindLabel[need.kind]} · ${need.taskId} · ${need.taskTitle}`}
      tabIndex={0}
      onKeyDown={(e) => {
        // Enter on a roving-focused card fires its primary action (with confirm).
        if (e.key === "Enter" && e.target === e.currentTarget && !pending) {
          e.preventDefault();
          void onPrimary();
        }
      }}
      className={cn(
        "grid grid-cols-[92px_1fr] gap-6 rounded-lg border-b border-line px-3 pb-6 pt-[22px] outline-none transition-all duration-[380ms] ease-out focus-visible:ring-2 focus-visible:ring-accent/40",
        resolving && "pointer-events-none translate-y-[-6px] opacity-0",
      )}
    >
      {/* Left rail: how much this blocks + kind */}
      <div className="border-l-[3px] pl-[15px]" style={{ borderColor: colorVar }}>
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
        <div className="mb-1 flex items-center gap-2.5">
          {showProject && (
            <Link
              to="/p/$projectId/needs"
              params={{ projectId: need.projectId }}
              className="rounded-md bg-hover px-1.5 py-0.5 font-mono text-[10px] font-semibold text-ink-soft transition-colors hover:bg-active"
            >
              ◇ {need.projectName}
            </Link>
          )}
          <Mono className="text-[11px] text-faint">{need.taskId}</Mono>
          <Mono className="ml-auto text-[11px] text-faint">
            {relativeTime(need.since)} · {need.priority}
          </Mono>
        </div>

        <h3 className="mb-2 font-serif text-[20px] font-semibold leading-tight tracking-[-0.015em] text-ink">
          {need.taskTitle}
        </h3>

        <NeedBody need={need} />

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

        <div className="mt-4 flex items-center gap-3">
          <PrimaryAction
            need={need}
            colorVar={colorVar}
            disabled={primaryDisabled}
            onClick={onPrimary}
          />
          <SecondaryAction need={need} disabled={pending} onClick={onSecondary} />
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
        <div className="mt-1 max-w-[66ch] overflow-hidden rounded-lg border border-line">
          {need.acceptance.map((a, i) => (
            <div
              key={a.id}
              className={cn(
                "flex items-center justify-between gap-3 px-3 py-2 text-[13px] text-ink-soft",
                i > 0 && "border-t border-line-soft",
              )}
            >
              <span>{a.text}</span>
              <ProofChip proof={a.proof} />
            </div>
          ))}
        </div>
      );
    case "failed":
      return (
        <div className="max-w-[66ch]">
          <div className="rounded-lg border border-fail/30 bg-fail-soft px-3 py-2 font-mono text-[12px] leading-relaxed text-fail">
            {need.lastError}
          </div>
          <p className="mt-2 text-[13px] text-muted">
            Exhausted after <Mono className="text-ink-soft">{need.attempts}</Mono> attempts.
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
            : "Retry";
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
  return (
    <Link
      to="/p/$projectId/runs/$taskId"
      params={{ projectId: need.projectId, taskId: need.taskId }}
      className="rounded-lg border border-line px-3.5 py-2 text-[13px] font-medium text-ink-soft transition-colors hover:border-fainter"
    >
      Open task
    </Link>
  );
}
