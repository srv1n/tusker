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
  ChevronDown,
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
  useDiscardTask,
  useDoc,
  useEvidenceAdd,
  useFeedbackAdd,
  useFrontmatterUpdate,
  useGateAction,
  useLandTask,
  useTask,
  useTaskStatusAction,
} from "@/lib/queries";
import { Button, Select, TextInput } from "@/components/ui/controls";
import { ActionResultLine, useConfirm } from "@/components/ui/action-feedback";
import { duration, relativeTime } from "@/lib/time";
import type { DiscardImpact, EvidenceCard, RunSummary, TaskDetail } from "@/types/domain";
import { DocEditor, type EditorRuntimeConfig } from "@/features/editor";
import { DocShell } from "./DocShell";
import { FrontmatterInlineControl, PropertyPanel } from "./PropertyPanel";
import { KindEyebrow, ResultChip } from "./bits";
import { ConflictBanner, MergeReadiness, SavedBanner, ValidationStrip } from "./banners";
import { useDocEditor, type DocEditor as DocEditorState } from "./editor";
import { resolveWikilink, wikilinkTargets } from "./mock";
import { HumanActionCard } from "@/features/human-action/HumanActionCard";
import type { MergeCheck } from "./types";
import {
  readMarkdownSection,
  replaceMarkdownSection,
  taskDetailToDocContent,
  taskDocPath,
} from "./taskMarkdown";

const barBtn =
  "rounded-lg px-3.5 py-1.5 text-[12.5px] font-semibold leading-none transition-colors";

export function TaskContract({ projectId, taskId, focusGateId }: { projectId: string; taskId: string; focusGateId?: string }) {
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
  return <ContractBody key={task.id} projectId={projectId} task={task} focusGateId={focusGateId} />;
}

function ContractBody({ projectId, task, focusGateId }: { projectId: string; task: TaskDetail; focusGateId?: string }) {
  const docPath = taskDocPath(task.id);
  const docQuery = useDoc(docPath, projectId);
  const doc = docQuery.data ?? taskDetailToDocContent(task);
  const ed = useDocEditor(doc);
  const navigate = useNavigate();
  const editorHostRef = useRef<HTMLDivElement>(null);
  const draftRef = useRef(ed.draft);
  const [activeProse, setActiveProse] = useState<string | null>(null);
  const [focusAt, setFocusAt] = useState<{ x: number; y: number } | null>(null);
  const humanActions = [...(task.humanActions?.length ? task.humanActions : task.humanAction ? [task.humanAction] : [])]
    .sort((a, b) => Number(b.gateId === focusGateId) - Number(a.gateId === focusGateId));

  const editing = ed.phase === "editing";
  const confirm = useConfirm();
  const closeReview = useCloseTask(task.id, projectId);
  const frontmatterUpdate = useFrontmatterUpdate();
  const pendingFrontmatterKey = frontmatterUpdate.isPending ? frontmatterUpdate.variables?.key : null;
  const rawStatus =
    task.rawStatus ?? (task.status === "in_progress" || task.status === "blocked" ? "ready" : task.status);
  const rawReadiness =
    task.rawReadiness ??
    (task.readiness === "blocked_dependency"
      ? "blocked_by_dependency"
      : task.readiness === "blocked_gate"
        ? "blocked_by_gate"
        : task.readiness);

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
    { key: "title", value: task.title, locked: false },
    { key: "id", value: task.id, locked: true, lockReason: "Task id is the record key. Rename by creating or superseding the task." },
    { key: "status", value: rawStatus, locked: true, lockReason: "Status is managed by lifecycle actions; use Discard task for cancelled work." },
    { key: "readiness", value: rawReadiness, locked: true, lockReason: "Readiness is derived from status, gates, dependencies, and runtime ownership." },
    { key: "priority", value: task.priority, locked: false },
    { key: "risk", value: task.risk, locked: false },
    { key: "epic", value: task.epicId, locked: true, lockReason: "Epic membership is managed by task planning controls." },
    { key: "updated_at", value: task.updatedAt.slice(0, 10), locked: true, lockReason: "updated_at is stamped by successful structured actions." },
  ];
  const frontmatterByKey = Object.fromEntries(frontmatter.map((field) => [field.key, field]));
  const commitFrontmatter = (key: string, value: string) =>
    frontmatterUpdate.mutate({ target: { kind: "task", id: task.id }, key, value });

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

          <PropertyPanel
            frontmatter={frontmatter}
            onCommit={commitFrontmatter}
            pendingKey={pendingFrontmatterKey}
          />

          {humanActions.map((action) => (
            <HumanActionCard
              key={action.gateId}
              action={action}
              taskId={task.id}
              taskTitle={task.title}
              projectId={projectId}
            />
          ))}

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
            <FactRow k="status">
              <EditableFact
                field={frontmatterByKey.status}
                onCommit={commitFrontmatter}
                pending={pendingFrontmatterKey === "status"}
              >
                <StatusChip status={task.rawStatus ?? task.status} />
              </EditableFact>
            </FactRow>
            <FactRow k="readiness">
              <EditableFact
                field={frontmatterByKey.readiness}
                onCommit={commitFrontmatter}
                pending={pendingFrontmatterKey === "readiness"}
              >
                <ReadinessChip readiness={frontmatterByKey.readiness.value} />
              </EditableFact>
            </FactRow>
            <FactRow k="priority">
              <EditableFact
                field={frontmatterByKey.priority}
                onCommit={commitFrontmatter}
                pending={pendingFrontmatterKey === "priority"}
              >
                <PriorityChip priority={frontmatterByKey.priority.value as typeof task.priority} />
              </EditableFact>
            </FactRow>
            <FactRow k="risk">
              <EditableFact
                field={frontmatterByKey.risk}
                onCommit={commitFrontmatter}
                pending={pendingFrontmatterKey === "risk"}
              >
                <RiskChip risk={frontmatterByKey.risk.value as typeof task.risk} />
              </EditableFact>
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
            {humanActions.length === 0 && <TaskActionPanel task={task} projectId={projectId} />}
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
  const discardTask = useDiscardTask(task.id, projectId);
  const landTask = useLandTask(task.id, projectId);
  const gateAction = useGateAction();
  const evidenceAdd = useEvidenceAdd(task.id, projectId);
  const feedbackAdd = useFeedbackAdd(projectId);
  const confirm = useConfirm();

  const [moreOpen, setMoreOpen] = useState(false);
  const [selectedStatus, setSelectedStatus] = useState("");
  const [activeAction, setActiveAction] = useState("");
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
  const [discardImpact, setDiscardImpact] = useState<DiscardImpact | null>(null);
  const [discardResolution, setDiscardResolution] = useState<"" | "detach" | "discard">("");

  // Shared busy flag: the whole panel disables while any one action is in
  // flight, so a slow POST can't be double-fired from another button.
  const busy =
    statusAction.isPending ||
    closeTask.isPending ||
    discardTask.isPending ||
    landTask.isPending ||
    gateAction.isPending ||
    evidenceAdd.isPending ||
    feedbackAdd.isPending;

  const currentStatus =
    task.rawStatus ??
    (task.status === "in_progress" || task.status === "blocked" ? "ready" : task.status);
  const statusOptions = [
    ["ready", "Ready"],
    ["review", "Review"],
    ["rework", "Rework"],
    ["backlog", "Backlog"],
  ].filter(([status]) => status !== currentStatus);
  const selectedStatusLabel = statusOptions.find(([status]) => status === selectedStatus)?.[1];

  const mutateStatus = () => {
    if (!selectedStatus) return;
    statusAction.mutate(
      { status: selectedStatus, reason: reason || undefined, actor: actor || undefined },
      { onSuccess: () => setSelectedStatus("") },
    );
  };

  const previewDiscard = async () => {
    const result = await discardTask.mutateAsync({ dryRun: true });
    if (result.ok && result.discard) {
      setDiscardImpact(result.discard);
      if (!result.discard.requiresResolution) setDiscardResolution("");
    }
  };

  const onDiscard = async () => {
    if (!discardImpact || !reason.trim()) return;
    if (discardImpact.requiresResolution && !discardResolution) return;
    const direct = discardImpact.directDependents.length;
    const cascade = discardImpact.cascadeDependents.length;
    const dependencySummary = !direct
      ? "No active downstream tasks depend on it."
      : discardResolution === "detach"
        ? `${direct} direct dependent${direct === 1 ? "" : "s"} will have this prerequisite explicitly detached.`
        : `${cascade} downstream task${cascade === 1 ? "" : "s"} will also be discarded.`;
    const gateSummary = discardImpact.openGates.length
      ? ` ${discardImpact.openGates.length} open gate${discardImpact.openGates.length === 1 ? "" : "s"} will be made obsolete.`
      : "";
    const ok = await confirm({
      title: `Discard ${task.id}`,
      body: `${dependencySummary}${gateSummary} Task, attempt, evidence, and event history will be preserved.`,
      confirmLabel: "Discard task",
      tone: "danger",
      typeToConfirm: task.id,
    });
    if (!ok) return;
    discardTask.mutate(
      {
        reason: reason.trim(),
        actor: actor || undefined,
        dependents: discardResolution || undefined,
      },
      {
        onSuccess: (result) => {
          if (result.ok) {
            setDiscardImpact(null);
            setDiscardResolution("");
            setActiveAction("");
          }
        },
      },
    );
  };

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
    <div className="overflow-hidden rounded-xl border border-line bg-raised">
      <div className="space-y-2.5 p-3">
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="mb-1 font-mono text-[9px] uppercase tracking-[0.14em] text-fainter">
              Current state
            </div>
            <StatusChip status={task.rawStatus ?? task.status} />
          </div>
          <Select
            aria-label="Move task to another state"
            value={selectedStatus}
            disabled={busy}
            onChange={(event) => setSelectedStatus(event.target.value)}
            className="w-[138px]"
          >
            <option value="">Move to...</option>
            {statusOptions.map(([status, label]) => (
              <option key={status} value={status}>
                {label}
              </option>
            ))}
          </Select>
        </div>

        {selectedStatus && (
          <div className="space-y-2 border-t border-line-soft pt-2.5 animate-rise">
            <TextInput
              aria-label="Reason for status change"
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder="Reason (optional)"
              className="w-full"
            />
            <TextInput
              aria-label="Actor for status change"
              value={actor}
              onChange={(event) => setActor(event.target.value)}
              placeholder="Actor (optional)"
              className="w-full"
            />
            <Button
              type="button"
              size="sm"
              variant={selectedStatus === "cancelled" ? "danger" : "primary"}
              className="w-full"
              disabled={busy}
              onClick={mutateStatus}
            >
              Move to {selectedStatusLabel}
            </Button>
          </div>
        )}
        <ActionResultLine pending={statusAction.isPending} error={statusAction.error} result={statusAction.data} />
      </div>

      <button
        type="button"
        aria-expanded={moreOpen}
        onClick={() => {
          setMoreOpen((open) => !open);
          if (moreOpen) setActiveAction("");
        }}
        className="flex w-full items-center justify-between border-t border-line-soft px-3 py-2.5 text-left text-[12.5px] font-medium text-ink-soft transition-colors hover:bg-hover"
      >
        More actions
        <ChevronDown
          size={14}
          strokeWidth={2}
          className={cn("text-faint transition-transform", moreOpen && "rotate-180")}
        />
      </button>

      {moreOpen && (
        <div className="space-y-2.5 border-t border-line-soft p-3 animate-rise">
          <Select
            aria-label="Choose another task action"
            value={activeAction}
            onChange={(event) => setActiveAction(event.target.value)}
            disabled={busy}
            className="w-full"
          >
            <option value="">Choose an action...</option>
            <option value="close">Accept &amp; close</option>
            <option value="land">Land task branch</option>
            <option value="discard">Discard task</option>
            {task.gates.length > 0 && <option value="gate">Manage gate</option>}
            <option value="evidence">Add evidence</option>
            <option value="feedback">Send feedback</option>
          </Select>

          {activeAction === "close" && (
            <div className="space-y-2 animate-rise">
              <TextInput aria-label="Close reason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Reason (optional)" className="w-full" />
              <TextInput aria-label="Close actor" value={actor} onChange={(e) => setActor(e.target.value)} placeholder="Actor (optional)" className="w-full" />
              <Button type="button" size="sm" variant="primary" className="w-full" disabled={busy} onClick={() => closeTask.mutate({ reason: reason || undefined, actor: actor || undefined })}>
                Accept &amp; close
              </Button>
              <ActionResultLine pending={closeTask.isPending} error={closeTask.error} result={closeTask.data} />
            </div>
          )}

          {activeAction === "land" && (
            <div className="space-y-2 animate-rise">
              <p className="text-[12px] leading-relaxed text-muted">
                Merge this task branch into the default branch. You will confirm the task ID next.
              </p>
              <Button type="button" size="sm" variant="danger" className="w-full" disabled={busy} onClick={onLand}>
                <GitMerge size={12} />
                Land task branch
              </Button>
              <ActionResultLine pending={landTask.isPending} error={landTask.error} result={landTask.data} />
            </div>
          )}

          {activeAction === "discard" && (
            <div className="space-y-2 animate-rise">
              <p className="text-[12px] leading-relaxed text-muted">
                Remove this task from active work without deleting its audit history. Tusker will inspect downstream dependencies before changing anything.
              </p>
              <TextInput
                aria-label="Discard reason"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                placeholder="Why is this work being discarded?"
                className="w-full"
              />
              {!discardImpact ? (
                <Button type="button" size="sm" variant="danger" className="w-full" disabled={busy || !reason.trim()} onClick={previewDiscard}>
                  Review discard impact
                </Button>
              ) : (
                <div className="space-y-2 rounded-lg border border-line-soft bg-hover/40 p-2.5">
                  <div className="font-mono text-[10.5px] text-faint">
                    {discardImpact.directDependents.length} direct dependent{discardImpact.directDependents.length === 1 ? "" : "s"} · {discardImpact.openGates.length} open gate{discardImpact.openGates.length === 1 ? "" : "s"}
                  </div>
                  {discardImpact.directDependents.length > 0 && (
                    <div className="text-[11.5px] leading-relaxed text-muted">
                      {discardImpact.directDependents.map((dependent) => dependent.id).join(", ")}
                    </div>
                  )}
                  {discardImpact.requiresResolution && (
                    <Select
                      aria-label="Resolve downstream dependencies"
                      value={discardResolution}
                      onChange={(event) => setDiscardResolution(event.target.value as "" | "detach" | "discard")}
                      className="w-full"
                    >
                      <option value="">Resolve downstream tasks...</option>
                      <option value="detach">Detach this prerequisite</option>
                      <option value="discard">Discard downstream closure</option>
                    </Select>
                  )}
                  <Button
                    type="button"
                    size="sm"
                    variant="danger"
                    className="w-full"
                    disabled={busy || !reason.trim() || (discardImpact.requiresResolution && !discardResolution)}
                    onClick={onDiscard}
                  >
                    Discard task
                  </Button>
                </div>
              )}
              <ActionResultLine pending={discardTask.isPending} error={discardTask.error} result={discardTask.data} />
            </div>
          )}

          {activeAction === "gate" && (
            <div className="space-y-2 animate-rise">
              <Select aria-label="Gate" value={gateID} onChange={(e) => setGateID(e.target.value)} className="w-full">
                {task.gates.map((gate) => (
                  <option key={gate.id} value={gate.id}>{gate.id}</option>
                ))}
              </Select>
              <TextInput aria-label="Gate evidence or reason" value={gateText} onChange={(e) => setGateText(e.target.value)} placeholder="Evidence or reason" className="w-full" />
              <TextInput aria-label="Gate actor" value={actor} onChange={(e) => setActor(e.target.value)} placeholder="Actor (optional)" className="w-full" />
              <div className="grid grid-cols-3 gap-1.5">
                <Button type="button" size="sm" disabled={busy} onClick={() => gateAction.mutate({ gateId: gateID, action: "satisfy", body: { evidence: gateText, actor: actor || undefined }, taskId: task.id, projectId })}>Satisfy</Button>
                <Button type="button" size="sm" disabled={busy} onClick={() => gateAction.mutate({ gateId: gateID, action: "waive", body: { reason: gateText, actor: actor || undefined }, taskId: task.id, projectId })}>Waive</Button>
                <Button type="button" size="sm" variant="danger" disabled={busy} onClick={() => gateAction.mutate({ gateId: gateID, action: "obsolete", body: { reason: gateText, actor: actor || undefined }, taskId: task.id, projectId })}>Obsolete</Button>
              </div>
              <ActionResultLine pending={gateAction.isPending} error={gateAction.error} result={gateAction.data} />
            </div>
          )}

          {activeAction === "evidence" && (
            <div className="space-y-2 animate-rise">
              <div className="flex gap-1.5">
                <Select aria-label="Evidence kind" value={evidenceKind} onChange={(e) => setEvidenceKind(e.target.value)} className="min-w-0 flex-1">
                  <option value="automated_test">automated_test</option>
                  <option value="manual_smoke">manual_smoke</option>
                  <option value="human_review">human_review</option>
                  <option value="verification_summary">verification_summary</option>
                </Select>
                {task.acceptance.length > 0 ? (
                  <Select aria-label="Acceptance covered" value={evidenceCovers} onChange={(e) => setEvidenceCovers(e.target.value)} className="min-w-0 flex-1">
                    {task.acceptance.map((row) => <option key={row.id} value={row.id}>{row.id}</option>)}
                  </Select>
                ) : (
                  <TextInput aria-label="Acceptance covered" value={evidenceCovers} onChange={(e) => setEvidenceCovers(e.target.value)} placeholder="Covers" className="min-w-0 flex-1" />
                )}
              </div>
              <TextInput aria-label="Evidence summary" value={evidenceSummary} onChange={(e) => setEvidenceSummary(e.target.value)} placeholder="Evidence summary" className="w-full" />
              <Button type="button" size="sm" className="w-full" disabled={busy || !evidenceSummary.trim()} onClick={() => evidenceAdd.mutate({ kind: evidenceKind, covers: evidenceCovers, status: "accepted", summary: evidenceSummary })}>Add evidence</Button>
              <ActionResultLine pending={evidenceAdd.isPending} error={evidenceAdd.error} result={evidenceAdd.data} />
            </div>
          )}

          {activeAction === "feedback" && (
            <div className="space-y-2 animate-rise">
              <TextInput aria-label="Feedback friction" value={feedbackFriction} onChange={(e) => setFeedbackFriction(e.target.value)} placeholder="What got in the way?" className="w-full" />
              <TextInput aria-label="Product idea" value={feedbackIdea} onChange={(e) => setFeedbackIdea(e.target.value)} placeholder="Product idea (optional)" className="w-full" />
              <TextInput aria-label="Feedback impact" value={feedbackImpact} onChange={(e) => setFeedbackImpact(e.target.value)} placeholder="Impact (optional)" className="w-full" />
              <Button type="button" size="sm" className="w-full" disabled={busy || !feedbackFriction.trim()} onClick={() => feedbackAdd.mutate({ context: task.id, friction: feedbackFriction, productIdea: feedbackIdea, impact: feedbackImpact, related: task.id })}>Send feedback</Button>
              <ActionResultLine pending={feedbackAdd.isPending} error={feedbackAdd.error} result={feedbackAdd.data} />
            </div>
          )}
        </div>
      )}
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

function EditableFact({
  field,
  onCommit,
  pending,
  children,
}: {
  field: { key: string; value: string; locked: boolean; lockReason?: string };
  onCommit: (key: string, value: string) => void;
  pending: boolean;
  children: ReactNode;
}) {
  return (
    <FrontmatterInlineControl
      field={field}
      onCommit={onCommit}
      pending={pending}
      showChevron={false}
      className="border-0 bg-transparent p-0 hover:bg-transparent"
    >
      {children}
    </FrontmatterInlineControl>
  );
}

function RunHistoryItem({ run, projectId }: { run: RunSummary; projectId: string }) {
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
