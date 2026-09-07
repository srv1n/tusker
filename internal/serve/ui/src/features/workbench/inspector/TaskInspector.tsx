import { useEffect, useRef, type Ref } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { X } from "lucide-react";
import type { RunDetail, TaskDetail } from "@/types/domain";
import { HumanActionCard } from "@/features/human-action/HumanActionCard";
import { isSafeHref } from "@/features/editor/sanitize";
import {
  acceptedDelivery,
  actualStage,
  failingRows,
  identityDisplay,
  identitySummary,
  resolveVisibleTask,
  type InspectorExecutionIdentity,
} from "./inspectorLogic";
import "./inspector.css";

export type { InspectorExecutionIdentity };

export interface TaskInspectorProps {
  task: TaskDetail | null;
  run: RunDetail | null;
  selectedTaskId: string | null;
  loading: boolean;
  error?: string;
  executionIdentity?: InspectorExecutionIdentity;
  onClose: () => void;
  onOpenTask: (id: string) => void;
}

const STAGE_TONE: Record<string, string> = {
  neutral: "border-line bg-panel/60 text-muted",
  info: "border-info/30 bg-info-soft text-info",
  pass: "border-pass/30 bg-pass-soft text-pass",
  warn: "border-warn/30 bg-warn-soft text-warn",
  fail: "border-fail/30 bg-fail-soft text-fail",
};

function StageChip({ label, tone }: { label: string; tone: keyof typeof STAGE_TONE }) {
  return (
    <span
      data-testid="inspector-stage"
      className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-[11px] font-medium whitespace-nowrap ${STAGE_TONE[tone] ?? STAGE_TONE.neutral}`}
    >
      {label}
    </span>
  );
}

/**
 * TaskInspector — contextual task inspection in a temporary side panel.
 *
 * Contract notes for the integration owner:
 * - Mount only while a task is selected (selectedTaskId != null). The panel
 *   reports close via onClose; the host owns prior focus, the originating
 *   node/card, and the graph transform, and must restore focus on close.
 * - Fetch per selection and pass the payload through unchanged: when the
 *   payload id does not match selectedTaskId (rapid A -> B, late A
 *   response) the panel renders loading, never the other task's data.
 * - executionIdentity must be observed, stage-specific facts. Unobserved or
 *   partial identity renders as Unavailable — a past worker's model is never
 *   shown as the current reviewer's model.
 */
export function TaskInspector({
  task,
  run,
  selectedTaskId,
  loading,
  error,
  executionIdentity,
  onClose,
  onOpenTask,
}: TaskInspectorProps) {
  const panelRef = useRef<HTMLElement>(null);

  useEffect(() => {
    panelRef.current?.focus();
  }, [selectedTaskId]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation();
        onClose();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  if (!selectedTaskId) return null;

  const visible = resolveVisibleTask(task, selectedTaskId);

  if (error && !visible) {
    return (
      <div className="wux-inspector" role="dialog" aria-modal="false" aria-label="Task details unavailable">
        <InspectorHeader eyebrow={selectedTaskId} title="Task details unavailable" onClose={onClose} />
        <div className="wux-inspector-body">
          <p role="alert" data-testid="inspector-error" className="rounded-lg border border-fail/30 bg-fail-soft px-4 py-3 text-[12.5px] leading-relaxed text-fail">
            Could not load this task: {error}
          </p>
          <button type="button" onClick={onClose} className="wux-inspector-action">
            Back to work
          </button>
        </div>
      </div>
    );
  }

  if (loading || !visible) {
    return (
      <div className="wux-inspector" role="dialog" aria-modal="false" aria-label="Loading task details">
        <InspectorHeader eyebrow={selectedTaskId} title="Loading task…" onClose={onClose} />
        <div className="wux-inspector-body" aria-label="Loading" data-testid="inspector-loading">
          <div className="wux-inspector-skeleton" />
          <div className="wux-inspector-skeleton" />
          <div className="wux-inspector-skeleton wux-inspector-skeleton-short" />
        </div>
      </div>
    );
  }

  return <ReadyInspector task={visible} run={run} executionIdentity={executionIdentity} panelRef={panelRef} onClose={onClose} onOpenTask={onOpenTask} />;
}

function InspectorHeader({
  eyebrow,
  title,
  onClose,
}: {
  eyebrow: string;
  title: string;
  onClose: () => void;
}) {
  return (
    <div className="wux-inspector-head">
      <div className="min-w-0 flex-1">
        <div className="truncate font-mono text-[10.5px] font-semibold uppercase tracking-[0.14em] text-faint">
          {eyebrow}
        </div>
        <h2 className="mt-1 text-[17px] font-semibold leading-snug tracking-[-0.015em] text-ink">
          {title}
        </h2>
      </div>
      <button
        type="button"
        onClick={onClose}
        aria-label="Close task panel"
        data-testid="inspector-close"
        className="wux-inspector-close"
      >
        <X size={16} aria-hidden="true" />
      </button>
    </div>
  );
}

function ReadyInspector({
  task,
  run,
  executionIdentity,
  panelRef,
  onClose,
  onOpenTask,
}: {
  task: TaskDetail;
  run: RunDetail | null;
  executionIdentity?: InspectorExecutionIdentity;
  panelRef: Ref<HTMLElement>;
  onClose: () => void;
  onOpenTask: (id: string) => void;
}) {
  const stage = actualStage(task, run);
  const identity = identityDisplay(executionIdentity);
  const failures = failingRows(task);
  const accepted = acceptedDelivery(run);
  const blockers = task.deps.filter((dep) => dep.status !== "done");
  const lastEvent = run?.events?.length ? run.events[run.events.length - 1] : null;

  return (
    <div className="wux-inspector-scrim-wrap">
      <button type="button" aria-label="Close task panel" data-testid="inspector-scrim" className="wux-inspector-scrim" onClick={onClose} tabIndex={-1} />
      <section
        ref={panelRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="false"
        aria-label={`Task ${task.id}: ${task.title}`}
        data-testid="inspector-panel"
        data-task-id={task.id}
        data-inspector-state="ready"
        className="wux-inspector"
      >
        <InspectorHeader eyebrow={task.id} title={task.title} onClose={onClose} />

        <div className="wux-inspector-body">
          <div className="flex flex-wrap items-center gap-2">
            <StageChip label={stage.label} tone={stage.tone} />
            {stage.live && (
              <span className="font-mono text-[10.5px] text-faint">live run fact, durable status kept</span>
            )}
          </div>

          {task.humanAction ? (
            <div className="mt-4" data-testid="inspector-decision">
              <HumanActionCard
                action={task.humanAction}
                taskId={task.id}
                taskTitle={task.title}
                projectId={task.projectId}
                compact
              />
            </div>
          ) : blockers.length > 0 ? (
            <p data-testid="inspector-decision" className="mt-4 rounded-lg border border-warn/30 bg-warn-soft px-4 py-3 text-[12.5px] leading-relaxed text-warn">
              <strong>Blocked by prerequisites.</strong>{" "}
              {blockers.map((dep) => `${dep.id} (${dep.status})`).join(" · ")}
            </p>
          ) : null}
          {run?.outcome === "failed" && (
            <p data-testid="inspector-failure" className="mt-4 rounded-lg border border-fail/30 bg-fail-soft px-4 py-3 text-[12.5px] leading-relaxed text-fail">
              <strong>Last attempt failed.</strong>
              {run.error ? ` ${run.error}` : " See the run log before retrying."}
            </p>
          )}
          {(failures.acceptance.length > 0 || failures.verification.length > 0) && (
            <p data-testid="inspector-failure" className="mt-4 rounded-lg border border-fail/30 bg-fail-soft px-4 py-3 text-[12.5px] leading-relaxed text-fail">
              <strong>Failing checks need action.</strong>{" "}
              {[
                failures.acceptance.length > 0 && `${failures.acceptance.length} acceptance`,
                failures.verification.length > 0 && `${failures.verification.length} verification`,
              ]
                .filter(Boolean)
                .join(" · ")}
              {" — "}details in the disclosures below.
            </p>
          )}

          <section aria-label="What this task achieves" className="mt-6">
            <h3 className="wux-inspector-h">What this task achieves</h3>
            <div data-testid="inspector-intent" className="wux-inspector-intent">
              <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={{
                  a: ({ href, children }) =>
                    typeof href === "string" && isSafeHref(href) ? (
                      <a href={href}>{children}</a>
                    ) : (
                      <span>{children}</span>
                    ),
                }}
              >
                {task.intent || "No intent recorded."}
              </ReactMarkdown>
            </div>
          </section>

          <section aria-label="Active attempt" className="mt-6">
            <h3 className="wux-inspector-h">Active attempt</h3>
            <dl className="space-y-2 text-[12.5px] leading-5">
              <div className="flex gap-2">
                <dt className="w-20 flex-none font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Identity</dt>
                <dd data-testid="inspector-identity" className="text-ink">{identitySummary(identity)}</dd>
              </div>
              <div className="flex gap-2">
                <dt className="w-20 flex-none font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Run</dt>
                <dd data-testid="inspector-run" className="text-muted">
                  {run ? `${run.lane} · ${run.outcome.replaceAll("-", " ")} · attempt ${run.attemptCount}` : "Unavailable — no runtime record"}
                </dd>
              </div>
              <div className="flex gap-2">
                <dt className="w-20 flex-none font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Last event</dt>
                <dd data-testid="inspector-event" className="text-muted">
                  {lastEvent ? lastEvent.text : "Unavailable"}
                </dd>
              </div>
            </dl>
          </section>

          {accepted ? (
            <section aria-label="Accepted result" className="mt-6">
              <h3 className="wux-inspector-h">Accepted result</h3>
              <p data-testid="inspector-accepted" className="rounded-lg border border-pass/30 bg-pass-soft px-4 py-3 text-[12.5px] leading-relaxed text-ink">
                {accepted.summary}
              </p>
            </section>
          ) : null}

          <section aria-label="Available evidence" className="mt-6">
            <h3 className="wux-inspector-h">Available evidence</h3>
            {task.evidence.length === 0 ? (
              <p data-testid="inspector-evidence-empty" className="text-[12.5px] leading-5 text-muted">
                No evidence attached yet.
              </p>
            ) : (
              <>
                <ul className="space-y-2">
                  {task.evidence.map((item) => (
                    <li
                      key={item.id}
                      data-testid="inspector-evidence-item"
                      className="rounded-lg border border-line bg-raised px-3 py-2.5"
                    >
                      <div className="truncate text-[12.5px] font-medium text-ink-soft">{item.label}</div>
                      <div className="mt-0.5 truncate font-mono text-[10.5px] text-faint">
                        {item.kind} · {item.ref}
                      </div>
                    </li>
                  ))}
                </ul>
                <p className="mt-2 text-[11.5px] leading-5 text-faint">
                  Appearing here is not acceptance — see the acceptance proofs below.
                </p>
              </>
            )}
          </section>

          <div className="mt-6 space-y-2">
            <details data-testid="inspector-acceptance">
              <summary>Acceptance ({task.acceptance.length})</summary>
              {task.acceptance.length === 0 ? (
                <p className="mt-2 text-[12.5px] text-muted">No acceptance rows recorded.</p>
              ) : (
                <ul className="mt-2 space-y-2">
                  {task.acceptance.map((row) => (
                    <li key={row.id} className="flex items-start gap-2 text-[12.5px] leading-5">
                      <span className="font-mono text-[10.5px] text-faint">{row.id}</span>
                      <span className="flex-1 text-ink">{row.text}</span>
                      <span
                        className={`inline-flex rounded-full border px-2 py-0.5 text-[10.5px] font-medium ${
                          row.proof === "pass"
                            ? "border-pass/30 bg-pass-soft text-pass"
                            : row.proof === "fail"
                              ? "border-fail/30 bg-fail-soft text-fail"
                              : "border-line bg-panel/60 text-muted"
                        }`}
                      >
                        {row.proof}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </details>

            <details data-testid="inspector-verification">
              <summary>Exact verification ({task.verification.length})</summary>
              {task.verification.length === 0 ? (
                <p className="mt-2 text-[12.5px] text-muted">No verification commands recorded.</p>
              ) : (
                <ul className="mt-2 space-y-3">
                  {task.verification.map((row) => (
                    <li key={row.id} className="text-[12px]">
                      <code className="block break-words font-mono text-[11px] leading-5 text-ink-soft">{row.command}</code>
                      <span className="mt-1 inline-flex rounded-full border border-line bg-panel/60 px-2 py-0.5 text-[10.5px] font-medium text-muted">
                        {row.result}
                      </span>
                      {row.detail && <p className="mt-1 leading-5 text-muted">{row.detail}</p>}
                    </li>
                  ))}
                </ul>
              )}
            </details>

            {task.nonGoals.length > 0 && (
              <details data-testid="inspector-nongoals">
                <summary>Non-goals ({task.nonGoals.length})</summary>
                <ul className="mt-2 list-disc space-y-1 pl-5 text-[12.5px] leading-5 text-muted">
                  {task.nonGoals.map((goal) => (
                    <li key={goal}>{goal}</li>
                  ))}
                </ul>
              </details>
            )}

            <details data-testid="inspector-metadata">
              <summary>Technical metadata</summary>
              <dl className="mt-2 space-y-2 text-[12px]">
                {[
                  ["Epic", `${task.epicId} · ${task.epicTitle}`],
                  ["Priority", task.priority.toUpperCase()],
                  ["Risk", task.risk],
                  ["Readiness", task.readiness.replaceAll("_", " ")],
                  ["Updated", task.updatedAt],
                ].map(([name, value]) => (
                  <div key={name} className="flex gap-2 border-b border-line-soft pb-1.5">
                    <dt className="w-20 flex-none font-mono text-[10px] uppercase tracking-[0.12em] text-faint">{name}</dt>
                    <dd className="text-ink">{value}</dd>
                  </div>
                ))}
              </dl>
            </details>
          </div>

          <div className="mt-6 flex flex-wrap gap-2 border-t border-line pt-4">
            <button
              type="button"
              data-testid="inspector-open-task"
              onClick={() => onOpenTask(task.id)}
              className="wux-inspector-action wux-inspector-action-primary"
            >
              Open full task <span aria-hidden="true">→</span>
            </button>
            <button type="button" onClick={onClose} className="wux-inspector-action">
              Close panel
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}
