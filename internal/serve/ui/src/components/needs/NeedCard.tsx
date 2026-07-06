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
import type { GateKind, NeedItem } from "@/types/domain";

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

/**
 * A single needs-me card (packet §4.1). One human gate, actionable on the card:
 * the operator should clear most items without navigating. Renders per gate
 * kind; the left rail shows how much work it blocks (the primary ranking key).
 */
export function NeedCard({ need, showProject = false }: { need: NeedItem; showProject?: boolean }) {
  const [resolving, setResolving] = useState(false);
  const [gone, setGone] = useState(false);
  const [checked, setChecked] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
  const [draft, setDraft] = useState("");

  if (gone) return null;

  const t = gateKindTone[need.kind];
  const Icon = kindIcon[need.kind];
  const colorVar = `var(--k-${t === "neutral" ? "faint" : t})`;

  function resolve() {
    setResolving(true);
    setTimeout(() => setGone(true), 380);
  }

  return (
    <div
      className={cn(
        "grid grid-cols-[92px_1fr] gap-6 rounded-lg border-b border-line px-3 pb-6 pt-[22px] transition-all duration-[380ms] ease-out",
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
            disabled={need.kind === "provision" && !checked}
            onResolve={resolve}
            onCompose={() => setComposeOpen((v) => !v)}
          />
          <SecondaryAction need={need} onCompose={() => setComposeOpen((v) => !v)} />
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
                onClick={resolve}
                className="rounded-lg px-3.5 py-2 text-[12.5px] font-semibold text-surface"
                style={{ backgroundColor: colorVar }}
              >
                {need.kind === "clarify" ? "Send answer" : "Send back with note"}
              </button>
            </div>
          </div>
        )}
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
  onResolve,
  onCompose,
}: {
  need: NeedItem;
  colorVar: string;
  disabled: boolean;
  onResolve: () => void;
  onCompose: () => void;
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
  const onClick = need.kind === "clarify" ? onCompose : onResolve;
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className="rounded-lg px-4 py-2 text-[13px] font-semibold text-surface transition-opacity disabled:opacity-40"
      style={{ backgroundColor: colorVar }}
    >
      {label}
    </button>
  );
}

function SecondaryAction({ need, onCompose }: { need: NeedItem; onCompose: () => void }) {
  if (need.kind === "review" || need.kind === "approve-spec") {
    return (
      <button
        onClick={onCompose}
        className="rounded-lg border border-line px-3.5 py-2 text-[13px] font-medium text-ink-soft transition-colors hover:border-fainter"
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
