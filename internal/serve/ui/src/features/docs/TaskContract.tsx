import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentType,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
} from "react";
import {
  ExternalLink,
  FileDiff,
  FileText,
  Image as ImageIcon,
  Link2,
  Lock,
  ScrollText,
  GitMerge,
  X,
} from "lucide-react";
import { Link, useNavigate } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { Dot, Mono } from "@/components/ui/primitives";
import {
  GateKindChip,
  OutcomeChip,
  PriorityChip,
  ProofChip,
  ReadinessChip,
  RiskChip,
  RunnerBadge,
  StatusChip,
} from "@/components/ui/chips";
import { SectionLabel } from "@/components/ui/page";
import { Skeleton, ErrorState } from "@/components/ui/states";
import { livenessTone, statusTone } from "@/components/ui/tone";
import {
  useCloseTask,
  useDoc,
  useEvidenceAdd,
  useFeedbackAdd,
  useGateAction,
  useLandTask,
  useTask,
  useTaskStatusAction,
} from "@/lib/queries";
import { Button, Select, TextInput } from "@/components/ui/controls";
import { ActionResultLine, useConfirm } from "@/components/ui/action-feedback";
import { compactNumber, duration, relativeTime } from "@/lib/time";
import type { EvidenceCard, RunSummary, TaskDetail } from "@/types/domain";
import { DocEditor, type EditorRuntimeConfig } from "@/features/editor";
import { DocShell } from "./DocShell";
import { PropertyPanel } from "./PropertyPanel";
import { KindEyebrow, ResultChip } from "./bits";
import { ConflictBanner, MergeReadiness, SavedBanner, ValidationStrip } from "./banners";
import { useDocEditor, type DocEditor as DocEditorState } from "./editor";
import { resolveWikilink, wikilinkTargets } from "./mock";
import type { MergeCheck } from "./types";
import {
  readMarkdownSection,
  replaceMarkdownSection,
  taskDetailToDocContent,
  taskDocPath,
} from "./taskMarkdown";

const barBtn =
  "rounded-lg px-3.5 py-1.5 text-[12.5px] font-semibold leading-none transition-colors";

export function TaskContract({ projectId, taskId }: { projectId: string; taskId: string }) {
  const q = useTask(taskId, projectId);
  const task = q.data;

  if (!task) {
    if (q.isLoading) return <ContractSkeleton />;
    return (
      <div className="p-8">
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      </div>
    );
  }
  return <ContractBody key={task.id} projectId={projectId} task={task} />;
}

function ContractBody({ projectId, task }: { projectId: string; task: TaskDetail }) {
  const docPath = taskDocPath(task.id);
  const docQuery = useDoc(docPath, projectId);
  const doc = docQuery.data ?? taskDetailToDocContent(task);
  const ed = useDocEditor(doc);
  const navigate = useNavigate();
  const editorHostRef = useRef<HTMLDivElement>(null);
  const draftRef = useRef(ed.draft);
  const [activeProse, setActiveProse] = useState<string | null>(null);
  const [focusAt, setFocusAt] = useState<{ x: number; y: number } | null>(null);

  const editing = ed.phase === "editing";
  const confirm = useConfirm();
  const closeReview = useCloseTask(task.id, projectId);

  // Merge-readiness checklist, derived from the real acceptance criteria + their
  // proof state — never a fixture. A criterion that isn't passing is a blocker,
  // and MergeReadiness gates "Accept & close" on all-green.
  const checks: MergeCheck[] =
    task.status === "review"
      ? task.acceptance.map((row) => ({
          id: row.id,
          label: row.id.toUpperCase(),
          detail: row.text,
          state: row.proof,
        }))
      : [];

  const onAcceptClose = async () => {
    const ok = await confirm({
      title: `Accept & close ${task.id}`,
      confirmLabel: "Accept & close",
      tone: "default",
    });
    if (ok) closeReview.mutate({});
  };

  const frontmatter = [
    { key: "id", value: task.id, locked: true },
    { key: "status", value: task.status, locked: true },
    { key: "readiness", value: task.readiness, locked: true },
    { key: "priority", value: task.priority, locked: true },
    { key: "risk", value: task.risk, locked: true },
    { key: "epic", value: task.epicId, locked: true },
  ];

  useEffect(() => {
    draftRef.current = ed.draft;
  }, [ed.draft]);

  useEffect(() => {
    if (editing) return;
    setActiveProse(null);
    setFocusAt(null);
  }, [editing]);

  const editorConfig = useMemo<EditorRuntimeConfig>(
    () => ({
      resolveWikilink,
      wikilinkIndex: Object.values(wikilinkTargets),
      placeholder: "Write...",
      onOpenWikilink: ({ target, resolved }) =>
        navigate({
          to: "/p/$projectId/docs",
          params: { projectId },
          search: { path: resolved?.path ?? resolved?.id ?? target },
        }),
    }),
    [projectId, navigate],
  );

  const startProseEdit = (heading: string, point: { x: number; y: number } | null) => {
    if (editing) return;
    setActiveProse(heading);
    setFocusAt(point);
    ed.startEdit();
  };

  const updateProseSection = (heading: string, markdown: string) => {
    const next = replaceMarkdownSection(draftRef.current, heading, markdown);
    draftRef.current = next;
    ed.setDraft(next);
  };

  const cancelEdit = () => {
    ed.cancelEdit();
    setActiveProse(null);
    setFocusAt(null);
  };

  const focusEditor = () =>
    editorHostRef.current?.querySelector<HTMLElement>(".tk-prose")?.focus();

  const actions = editing ? (
    <>
      <Mono className="mr-1 text-[10.5px] text-warn">editing</Mono>
      {ed.isDirty && <Mono className="text-[10px] text-warn">unsaved</Mono>}
      <button className={cn(barBtn, "border border-line text-muted hover:bg-hover")} onClick={cancelEdit}>
        Cancel
      </button>
      <button className={cn(barBtn, "bg-pass text-surface hover:opacity-90")} onClick={ed.save}>
        Save
      </button>
    </>
  ) : (
    <Link
      to="/p/$projectId/docs"
      params={{ projectId }}
      search={{ path: doc.path, view: "source" }}
      className="flex items-center gap-1.5 rounded-lg border border-line bg-raised px-3.5 py-1.5 text-[12.5px] font-semibold text-ink-soft transition-colors hover:border-line-soft hover:bg-hover"
    >
      <FileText size={13} strokeWidth={2} />
      View source
    </Link>
  );

  return (
    <DocShell
      projectId={projectId}
      path={task.id}
      actions={actions}
    >
      <div className="mx-auto grid w-full max-w-[1080px] grid-cols-1 gap-9 px-4 pb-24 pt-7 sm:px-6 lg:grid-cols-[minmax(0,1fr)_280px] lg:px-11">
        <div ref={editorHostRef} className="min-w-0">
          {checks.length > 0 && (
            <>
              <MergeReadiness checks={checks} onAccept={onAcceptClose} />
              <ActionResultLine
                pending={closeReview.isPending}
                error={closeReview.error}
                result={closeReview.data}
                className="mb-6 -mt-4"
              />
            </>
          )}
          {ed.banner.type === "conflict" && (
            <ConflictBanner conflict={ed.banner.conflict} onReconcile={ed.reconcile} />
          )}
          {ed.banner.type === "invalid" && (
            <ValidationStrip issues={ed.banner.issues} onFix={focusEditor} onDiscard={cancelEdit} />
          )}
          {ed.banner.type === "saved" && <SavedBanner rev={String(ed.banner.rev)} />}

          <KindEyebrow kind="task" className="mb-1.5" />
          <Mono className="mb-1 block text-[11.5px] text-faint">{task.id}</Mono>
          <h1 className="mb-4 font-serif text-[32px] font-semibold leading-[1.08] tracking-[-0.02em] text-ink">
            {task.title}
          </h1>

          <PropertyPanel frontmatter={frontmatter} />

          <Section label="Intent">
            <TaskProseBlock
              heading="Intent"
              fallbackMarkdown={task.intent}
              ed={ed}
              config={editorConfig}
              editing={editing}
              active={activeProse === "Intent"}
              focusAt={activeProse === "Intent" ? focusAt : undefined}
              onStart={startProseEdit}
              onChange={updateProseSection}
            />
          </Section>

          <Section label="Acceptance">
            <div className="overflow-hidden rounded-xl border border-line">
              {task.acceptance.map((row) => (
                <div
                  key={row.id}
                  className="flex items-start gap-3 border-b border-line-soft px-4 py-3 last:border-0"
                >
                  <div className="mt-0.5 flex-none">
                    <ProofChip proof={row.proof} />
                  </div>
                  <span className="text-[14px] leading-[1.5] text-ink-soft">{row.text}</span>
                </div>
              ))}
            </div>
          </Section>

          {task.nonGoals.length > 0 && (
            <Section label="Non-goals">
              <ul className="flex flex-col gap-2">
                {task.nonGoals.map((g, i) => (
                  <li key={i} className="flex items-start gap-2.5 text-[14px] leading-[1.5] text-muted">
                    <X size={14} strokeWidth={2.25} className="mt-1 flex-none text-faint" />
                    <span>{g}</span>
                  </li>
                ))}
              </ul>
            </Section>
          )}

          <Section label="Verification">
            <div className="overflow-hidden rounded-xl border border-line">
              {task.verification.map((v) => (
                <div key={v.id} className="border-b border-line-soft px-4 py-2.5 last:border-0">
                  <div className="flex items-center gap-3">
                    <Mono className="min-w-0 flex-1 truncate text-[12px] text-ink-soft">
                      {v.command}
                    </Mono>
                    <ResultChip result={v.result} />
                  </div>
                  {v.detail && <div className="mt-1 text-[12px] text-faint">{v.detail}</div>}
                </div>
              ))}
            </div>
          </Section>

          {task.evidence.length > 0 && (
            <Section label="Evidence">
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {task.evidence.map((e) => (
                  <EvidenceItem key={e.id} evidence={e} projectId={projectId} />
                ))}
              </div>
            </Section>
          )}

          {task.runHistory.length > 0 && (
            <Section label="Run history">
              <div className="flex flex-col gap-2">
                {task.runHistory.map((run) => (
                  <RunHistoryItem
                    key={`${run.taskId}:${run.lane}:${run.attemptCount}:${run.outcome}`}
                    run={run}
                    projectId={projectId}
                  />
                ))}
              </div>
            </Section>
          )}

          {task.knowledgeDelta && (
            <Section label="Knowledge delta">
              <div className="flex gap-3 rounded-xl border border-accent/25 bg-accent-soft/40 px-4 py-3">
                <Link2 size={15} strokeWidth={2} className="mt-1 flex-none text-accent" />
                <div className="min-w-0 flex-1 [&_.tk-prose_p]:my-0 [&_.tk-prose_p]:text-[13.5px] [&_.tk-prose_p]:leading-[1.55] [&_.tk-prose_p]:text-muted">
                  <TaskProseBlock
                    heading="Knowledge delta"
                    fallbackMarkdown={task.knowledgeDelta}
                    ed={ed}
                    config={editorConfig}
                    editing={editing}
                    active={activeProse === "Knowledge delta"}
                    focusAt={activeProse === "Knowledge delta" ? focusAt : undefined}
                    onStart={startProseEdit}
                    onChange={updateProseSection}
                    compact
                  />
                </div>
              </div>
            </Section>
          )}
        </div>

        {/* Right rail: locked facts + deps + gates + actions */}
        <aside className="lg:sticky lg:top-6 lg:self-start">
          <div className="mb-4 overflow-hidden rounded-xl border border-line">
            <FactRow k="status" locked>
              <StatusChip status={task.status} />
            </FactRow>
            <FactRow k="readiness" locked>
              <ReadinessChip readiness={task.readiness} />
            </FactRow>
            <FactRow k="priority" locked>
              <PriorityChip priority={task.priority} />
            </FactRow>
            <FactRow k="risk" locked>
              <RiskChip risk={task.risk} />
            </FactRow>
            <FactRow k="epic">
              <Mono className="text-[11.5px] text-ink-soft">{task.epicId}</Mono>
            </FactRow>
            <FactRow k="updated" last>
              <Mono className="text-[11.5px] text-faint">{relativeTime(task.updatedAt)}</Mono>
            </FactRow>
          </div>

          {task.deps.length > 0 && (
            <>
              <RailLabel>Depends on</RailLabel>
              <div className="mb-4 flex flex-col gap-1.5">
                {task.deps.map((d) => (
                  <Link
                    key={d.id}
                    to="/p/$projectId/docs"
                    params={{ projectId }}
                    search={{ path: d.id }}
                    className="flex items-center gap-2 rounded-lg border border-line bg-raised px-2.5 py-2 transition-colors hover:border-line-soft hover:bg-hover"
                  >
                    <Dot tone={statusTone[d.status]} size={6} />
                    <Mono className="flex-none text-[10.5px] text-faint">{d.id}</Mono>
                    <span className="truncate text-[12px] text-ink-soft">{d.title}</span>
                  </Link>
                ))}
              </div>
            </>
          )}

          {task.gates.length > 0 && (
            <>
              <RailLabel>Gates</RailLabel>
              <div className="mb-4 flex flex-col gap-1.5">
                {task.gates.map((g) => (
                  <div
                    key={g.id}
                    className="flex items-center gap-2 rounded-lg border border-line px-2.5 py-2"
                  >
                    <GateKindChip kind={g.kind} />
                    <Mono className="ml-auto text-[10.5px] text-faint">{g.owner}</Mono>
                  </div>
                ))}
              </div>
            </>
          )}

          <div className="flex flex-col gap-2">
            <TaskActionPanel task={task} projectId={projectId} />
            {task.runHistory.length > 0 && (
              <Link
                to="/p/$projectId/runs/$taskId"
                params={{ projectId, taskId: task.id }}
                className="w-full rounded-lg border border-line py-2 text-center text-[13px] font-medium text-ink-soft transition-colors hover:bg-hover"
              >
                View latest run
              </Link>
            )}
            <button
              className="w-full rounded-lg py-1.5 text-[12px] text-faint transition-colors hover:text-ink"
              onClick={() => navigator.clipboard?.writeText(task.id).catch(() => {})}
            >
              Copy task id
            </button>
          </div>
        </aside>
      </div>
    </DocShell>
  );
}

function TaskProseBlock({
  heading,
  fallbackMarkdown,
  ed,
  config,
  editing,
  active,
  focusAt,
  onStart,
  onChange,
  compact = false,
}: {
  heading: string;
  fallbackMarkdown: string;
  ed: DocEditorState;
  config: EditorRuntimeConfig;
  editing: boolean;
  active: boolean;
  focusAt?: { x: number; y: number } | null;
  onStart: (heading: string, point: { x: number; y: number } | null) => void;
  onChange: (heading: string, markdown: string) => void;
  compact?: boolean;
}) {
  const activeEditing = editing && active;
  const source = readMarkdownSection(activeEditing ? ed.draft : ed.content, heading) ?? fallbackMarkdown;

  const startFromPointer = (event: MouseEvent<HTMLDivElement>) => {
    if (editing) return;
    onStart(heading, { x: event.clientX, y: event.clientY });
  };

  const startFromKeyboard = (event: KeyboardEvent<HTMLDivElement>) => {
    if (editing || (event.key !== "Enter" && event.key !== " ")) return;
    event.preventDefault();
    onStart(heading, null);
  };

  return (
    <div
      data-task-prose-section={heading}
      tabIndex={editing ? undefined : 0}
      onMouseDown={startFromPointer}
      onKeyDown={startFromKeyboard}
      className={cn(
        "rounded-lg transition-colors",
        !editing && "cursor-text hover:bg-hover",
        activeEditing && "animate-rise",
      )}
    >
      <DocEditor
        key={`${heading}:${ed.stateRev}:${activeEditing ? "edit" : "read"}`}
        initialMarkdown={source}
        editable={activeEditing}
        config={config}
        focusAt={activeEditing ? focusAt : undefined}
        onChange={activeEditing ? (markdown) => onChange(heading, markdown) : undefined}
        className={cn(activeEditing && "tk-task-prose-editing", compact && "tk-task-prose-compact")}
      />
    </div>
  );
}

function TaskActionPanel({ task, projectId }: { task: TaskDetail; projectId: string }) {
  const statusAction = useTaskStatusAction(task.id, projectId);
  const closeTask = useCloseTask(task.id, projectId);
  const landTask = useLandTask(task.id, projectId);
  const gateAction = useGateAction();
  const evidenceAdd = useEvidenceAdd(task.id, projectId);
  const feedbackAdd = useFeedbackAdd(projectId);
  const confirm = useConfirm();

  const [reason, setReason] = useState("");
  const [actor, setActor] = useState("");
  const [gateText, setGateText] = useState("");
  const [gateID, setGateID] = useState(task.gates[0]?.id ?? "");
  const [evidenceKind, setEvidenceKind] = useState("automated_test");
  const [evidenceCovers, setEvidenceCovers] = useState(task.acceptance[0]?.id ?? "");
  const [evidenceSummary, setEvidenceSummary] = useState("");
  const [feedbackFriction, setFeedbackFriction] = useState("");
  const [feedbackIdea, setFeedbackIdea] = useState("");
  const [feedbackImpact, setFeedbackImpact] = useState("");

  // Shared busy flag: the whole panel disables while any one action is in
  // flight, so a slow POST can't be double-fired from another button.
  const busy =
    statusAction.isPending ||
    closeTask.isPending ||
    landTask.isPending ||
    gateAction.isPending ||
    evidenceAdd.isPending ||
    feedbackAdd.isPending;

  const mutateStatus = (status: string) =>
    statusAction.mutate({ status, reason: reason || undefined, actor: actor || undefined });

  // Land is irreversible git surgery — merges the task branch into main. Gate it
  // behind a type-the-id confirm before firing.
  const onLand = async () => {
    const ok = await confirm({
      title: `Land ${task.id} to main`,
      body: "This merges the task branch into the default branch.",
      confirmLabel: "Land",
      tone: "danger",
      typeToConfirm: task.id,
    });
    if (ok) landTask.mutate({});
  };

  return (
    <div className="space-y-3 rounded-xl border border-line bg-raised p-3">
      <div className="grid grid-cols-2 gap-1.5">
        {[
          ["ready", "Ready"],
          ["review", "Review"],
          ["rework", "Rework"],
          ["backlog", "Backlog"],
          ["cancelled", "Cancel"],
        ].map(([status, label]) => (
          <Button key={status} type="button" size="sm" variant={status === "cancelled" ? "danger" : "default"} disabled={busy} onClick={() => mutateStatus(status)}>
            {label}
          </Button>
        ))}
      </div>
      <ActionResultLine pending={statusAction.isPending} error={statusAction.error} result={statusAction.data} />

      <TextInput value={reason} onChange={(e) => setReason(e.target.value)} placeholder="reason" className="w-full" />
      <TextInput value={actor} onChange={(e) => setActor(e.target.value)} placeholder="actor" className="w-full" />

      <div className="grid grid-cols-2 gap-1.5">
        <Button type="button" size="sm" disabled={busy} onClick={() => closeTask.mutate({ reason: reason || undefined, actor: actor || undefined })}>
          Accept
        </Button>
        <Button type="button" size="sm" variant="danger" disabled={busy} onClick={onLand}>
          <GitMerge size={12} />
          Land
        </Button>
      </div>
      <ActionResultLine pending={closeTask.isPending} error={closeTask.error} result={closeTask.data} />
      <ActionResultLine pending={landTask.isPending} error={landTask.error} result={landTask.data} />

      {task.gates.length > 0 && (
        <div className="space-y-1.5 border-t border-line-soft pt-3">
          <Select value={gateID} onChange={(e) => setGateID(e.target.value)} className="w-full">
            {task.gates.map((gate) => (
              <option key={gate.id} value={gate.id}>
                {gate.id}
              </option>
            ))}
          </Select>
          <TextInput value={gateText} onChange={(e) => setGateText(e.target.value)} placeholder="gate evidence or reason" className="w-full" />
          <div className="grid grid-cols-3 gap-1.5">
            <Button type="button" size="sm" disabled={busy} onClick={() => gateAction.mutate({ gateId: gateID, action: "satisfy", body: { evidence: gateText, actor: actor || undefined }, taskId: task.id, projectId })}>
              Satisfy
            </Button>
            <Button type="button" size="sm" disabled={busy} onClick={() => gateAction.mutate({ gateId: gateID, action: "waive", body: { reason: gateText, actor: actor || undefined }, taskId: task.id, projectId })}>
              Waive
            </Button>
            <Button type="button" size="sm" variant="danger" disabled={busy} onClick={() => gateAction.mutate({ gateId: gateID, action: "obsolete", body: { reason: gateText, actor: actor || undefined }, taskId: task.id, projectId })}>
              Obsolete
            </Button>
          </div>
          <ActionResultLine pending={gateAction.isPending} error={gateAction.error} result={gateAction.data} />
        </div>
      )}

      <div className="space-y-1.5 border-t border-line-soft pt-3">
        <div className="flex gap-1.5">
          <Select value={evidenceKind} onChange={(e) => setEvidenceKind(e.target.value)} className="min-w-0 flex-1">
            <option value="automated_test">automated_test</option>
            <option value="manual_smoke">manual_smoke</option>
            <option value="human_review">human_review</option>
            <option value="verification_summary">verification_summary</option>
          </Select>
          {task.acceptance.length > 0 ? (
            <Select value={evidenceCovers} onChange={(e) => setEvidenceCovers(e.target.value)} className="min-w-0 flex-1">
              {task.acceptance.map((row) => (
                <option key={row.id} value={row.id}>
                  {row.id}
                </option>
              ))}
            </Select>
          ) : (
            <TextInput value={evidenceCovers} onChange={(e) => setEvidenceCovers(e.target.value)} placeholder="covers" className="min-w-0 flex-1" />
          )}
        </div>
        <TextInput value={evidenceSummary} onChange={(e) => setEvidenceSummary(e.target.value)} placeholder="evidence summary" className="w-full" />
        <Button type="button" size="sm" disabled={busy || !evidenceSummary.trim()} onClick={() => evidenceAdd.mutate({ kind: evidenceKind, covers: evidenceCovers, status: "accepted", summary: evidenceSummary })}>
          Add evidence
        </Button>
        <ActionResultLine pending={evidenceAdd.isPending} error={evidenceAdd.error} result={evidenceAdd.data} />
      </div>

      <div className="space-y-1.5 border-t border-line-soft pt-3">
        <TextInput value={feedbackFriction} onChange={(e) => setFeedbackFriction(e.target.value)} placeholder="feedback friction" className="w-full" />
        <TextInput value={feedbackIdea} onChange={(e) => setFeedbackIdea(e.target.value)} placeholder="product idea" className="w-full" />
        <TextInput value={feedbackImpact} onChange={(e) => setFeedbackImpact(e.target.value)} placeholder="impact" className="w-full" />
        <Button
          type="button"
          size="sm"
          disabled={busy || !feedbackFriction.trim()}
          onClick={() =>
            feedbackAdd.mutate({
              context: task.id,
              friction: feedbackFriction,
              productIdea: feedbackIdea,
              impact: feedbackImpact,
              related: task.id,
            })
          }
        >
          Add feedback
        </Button>
        <ActionResultLine pending={feedbackAdd.isPending} error={feedbackAdd.error} result={feedbackAdd.data} />
      </div>
    </div>
  );
}

function Section({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section className="mb-7">
      <SectionLabel className="mb-2.5">{label}</SectionLabel>
      {children}
    </section>
  );
}

function RailLabel({ children }: { children: ReactNode }) {
  return (
    <div className="mb-1.5 ml-0.5 font-mono text-[9px] uppercase tracking-[0.14em] text-fainter">
      {children}
    </div>
  );
}

function RunHistoryItem({ run, projectId }: { run: RunSummary; projectId: string }) {
  const tokens = compactNumber(run.tokens.input + run.tokens.output);
  return (
    <Link
      to="/p/$projectId/runs/$taskId"
      params={{ projectId, taskId: run.taskId }}
      className="rounded-xl border border-line bg-raised px-3.5 py-3 transition-colors hover:border-line-soft hover:bg-hover"
    >
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <RunnerBadge runner={run.runner} />
        <OutcomeChip outcome={run.outcome} />
        <Mono className="text-[10.5px] text-faint">{run.lane}</Mono>
        <span className="ml-auto flex items-center gap-1.5">
          <Dot tone={livenessTone[run.liveness]} size={6} />
          <Mono className="text-[10.5px] text-faint">{run.liveness}</Mono>
        </span>
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-muted">
        <span>{plural(run.attemptCount, "attempt")}</span>
        <span>{duration(run.elapsedSec)}</span>
        <span>{tokens} tokens</span>
        <span>last event {duration(run.sinceLastEventSec)} ago</span>
      </div>
    </Link>
  );
}

function plural(count: number, singular: string, pluralForm = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : pluralForm}`;
}

function FactRow({
  k,
  locked = false,
  last = false,
  children,
}: {
  k: string;
  locked?: boolean;
  last?: boolean;
  children: ReactNode;
}) {
  return (
    <div
      className={cn(
        "flex items-center justify-between gap-3 px-3.5 py-2.5",
        !last && "border-b border-line-soft",
      )}
    >
      <span className="flex items-center gap-1 font-mono text-[10.5px] text-faint">
        {k}
        {locked && <Lock size={8} strokeWidth={2.25} className="text-fainter" />}
      </span>
      {children}
    </div>
  );
}

const evidenceIcon: Record<EvidenceCard["kind"], ComponentType<{ size?: number; strokeWidth?: number; className?: string }>> = {
  file: FileText,
  log: ScrollText,
  image: ImageIcon,
  link: ExternalLink,
  diff: FileDiff,
};

function EvidenceItem({ evidence, projectId }: { evidence: EvidenceCard; projectId: string }) {
  const Icon = evidenceIcon[evidence.kind];
  // Open the artifact in the reader by its ref/path; the reader surfaces a real
  // not-found state if the path doesn't resolve — never a dead no-op button.
  return (
    <Link
      to="/p/$projectId/docs"
      params={{ projectId }}
      search={{ path: evidence.ref }}
      className="flex items-center gap-2.5 rounded-lg border border-line bg-raised px-3 py-2.5 text-left transition-colors hover:border-line-soft hover:bg-hover"
    >
      <Icon size={15} strokeWidth={1.75} className="flex-none text-muted" />
      <div className="min-w-0">
        <div className="truncate text-[12.5px] font-medium text-ink-soft">{evidence.label}</div>
        <Mono className="truncate text-[10px] text-faint">{evidence.ref}</Mono>
      </div>
    </Link>
  );
}

function ContractSkeleton() {
  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-line px-4 py-3.5 sm:px-6 lg:px-11">
        <Skeleton className="h-4 w-52" />
      </div>
      <div className="mx-auto grid w-full max-w-[1080px] grid-cols-1 gap-9 px-4 pt-7 sm:px-6 lg:grid-cols-[minmax(0,1fr)_280px] lg:px-11">
        <div className="min-w-0">
          <Skeleton className="mb-3 h-3 w-16" />
          <Skeleton className="mb-5 h-9 w-2/3" />
          <Skeleton className="mb-7 h-14 w-full" />
          <Skeleton className="mb-3 h-24 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
        <div>
          <Skeleton className="h-48 w-full" />
        </div>
      </div>
    </div>
  );
}
