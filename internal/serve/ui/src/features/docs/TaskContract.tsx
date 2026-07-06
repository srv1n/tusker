import { useState, type ComponentType, type ReactNode } from "react";
import {
  ExternalLink,
  FileDiff,
  FileText,
  Image as ImageIcon,
  Link2,
  Lock,
  Pencil,
  ScrollText,
  X,
} from "lucide-react";
import { Link } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { Dot, Mono } from "@/components/ui/primitives";
import {
  GateKindChip,
  PriorityChip,
  ProofChip,
  ReadinessChip,
  RiskChip,
  StatusChip,
} from "@/components/ui/chips";
import { SectionLabel } from "@/components/ui/page";
import { Skeleton, ErrorState } from "@/components/ui/states";
import { statusTone } from "@/components/ui/tone";
import { useTask } from "@/lib/queries";
import { relativeTime } from "@/lib/time";
import type { EvidenceCard, TaskDetail } from "@/types/domain";
import type { ValidationIssue } from "./types";
import { DocShell } from "./DocShell";
import { Markdown } from "./Markdown";
import { PropertyPanel } from "./PropertyPanel";
import { KindEyebrow, ResultChip } from "./bits";
import { MergeReadiness } from "./banners";
import { localTaskDetails, mergeChecksFor, mockValidate, taskDocPath } from "./mock";

export function TaskContract({ projectId, taskId }: { projectId: string; taskId: string }) {
  const q = useTask(taskId);
  const task = q.data ?? localTaskDetails[taskId];

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
  const checks = task.status === "review" ? mergeChecksFor(task.id) : [];
  const frontmatter = [
    { key: "id", value: task.id, locked: true },
    { key: "status", value: task.status, locked: true },
    { key: "readiness", value: task.readiness, locked: true },
    { key: "priority", value: task.priority, locked: true },
    { key: "risk", value: task.risk, locked: true },
    { key: "epic", value: task.epicId, locked: true },
  ];

  return (
    <DocShell
      projectId={projectId}
      path={task.id}
      actions={
        <Link
          to="/p/$projectId/docs"
          params={{ projectId }}
          search={{ path: taskDocPath(task.id) }}
          className="flex items-center gap-1.5 rounded-lg border border-line bg-raised px-3.5 py-1.5 text-[12.5px] font-semibold text-ink-soft transition-colors hover:border-line-soft hover:bg-hover"
        >
          <ExternalLink size={13} strokeWidth={2} />
          Open markdown
        </Link>
      }
    >
      <div className="mx-auto grid w-full max-w-[1080px] grid-cols-1 gap-9 px-11 pb-24 pt-7 lg:grid-cols-[minmax(0,1fr)_280px]">
        <div className="min-w-0">
          {checks.length > 0 && (
            <MergeReadiness
              checks={checks}
              onAccept={() => {
                /* TODO(api): POST /api/tasks/:id/close */
              }}
            />
          )}

          <KindEyebrow kind="task" className="mb-1.5" />
          <Mono className="mb-1 block text-[11.5px] text-faint">{task.id}</Mono>
          <h1 className="mb-4 font-serif text-[32px] font-semibold leading-[1.08] tracking-[-0.02em] text-ink">
            {task.title}
          </h1>

          <PropertyPanel frontmatter={frontmatter} />

          <Section label="Intent">
            <EditableProse text={task.intent} projectId={projectId} />
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
                  <EvidenceItem key={e.id} evidence={e} />
                ))}
              </div>
            </Section>
          )}

          {task.knowledgeDelta && (
            <Section label="Knowledge delta">
              <div className="flex gap-3 rounded-xl border border-accent/25 bg-accent-soft/40 px-4 py-3">
                <Link2 size={15} strokeWidth={2} className="mt-1 flex-none text-accent" />
                <div className="min-w-0 [&_p]:my-0 [&_p]:text-[13.5px] [&_p]:leading-[1.55] [&_p]:text-muted">
                  <Markdown markdown={task.knowledgeDelta} projectId={projectId} />
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
            <button
              className="w-full rounded-lg bg-ink py-2.5 text-[13px] font-semibold text-surface transition-opacity hover:opacity-90"
              // TODO(api): POST /api/tasks/:id/transition (promote / rework / accept)
            >
              {stateAction(task.status)}
            </button>
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

function stateAction(status: TaskDetail["status"]): string {
  switch (status) {
    case "review":
      return "Send to rework";
    case "ready":
      return "Promote to in progress";
    case "in_progress":
      return "Request review";
    case "blocked":
      return "Clear blocker";
    default:
      return "Promote to ready";
  }
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

function EvidenceItem({ evidence }: { evidence: EvidenceCard }) {
  const Icon = evidenceIcon[evidence.kind];
  return (
    <button
      className="flex items-center gap-2.5 rounded-lg border border-line bg-raised px-3 py-2.5 text-left transition-colors hover:border-line-soft hover:bg-hover"
      // TODO(api): open evidence artifact (GET /api/tasks/:id/evidence/:ref)
    >
      <Icon size={15} strokeWidth={1.75} className="flex-none text-muted" />
      <div className="min-w-0">
        <div className="truncate text-[12.5px] font-medium text-ink-soft">{evidence.label}</div>
        <Mono className="truncate text-[10px] text-faint">{evidence.ref}</Mono>
      </div>
    </button>
  );
}

/**
 * In-place body editor (packet §4.5: body sections editable, frontmatter locked).
 * A pencil reveals a markdown textarea; save runs the same mock validation.
 * TODO(api): TipTap markdown surface + CAS save (shared with the doc reader).
 */
function EditableProse({ text, projectId }: { text: string; projectId: string }) {
  const [editing, setEditing] = useState(false);
  const [saved, setSaved] = useState(text);
  const [draft, setDraft] = useState(text);
  const [issues, setIssues] = useState<ValidationIssue[]>([]);

  const errors = issues.filter((i) => i.severity === "error");

  const save = () => {
    const found = mockValidate(draft);
    setIssues(found);
    if (found.some((i) => i.severity === "error")) return; // reject
    setSaved(draft);
    setEditing(false);
  };

  if (!editing) {
    return (
      <div className="group relative">
        <div className="[&>*:first-child]:mt-0 [&_p:last-child]:mb-0">
          <Markdown markdown={saved} projectId={projectId} />
        </div>
        <button
          onClick={() => {
            setDraft(saved);
            setIssues([]);
            setEditing(true);
          }}
          className="absolute -top-1 right-0 flex items-center gap-1 rounded-md border border-line bg-raised px-2 py-1 text-[11px] font-medium text-muted opacity-0 transition-opacity hover:bg-hover group-hover:opacity-100"
        >
          <Pencil size={11} strokeWidth={2} />
          Edit
        </button>
      </div>
    );
  }

  return (
    <div className="overflow-hidden rounded-lg border border-l-2 border-line border-l-accent/60 bg-panel">
      <textarea
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        spellCheck={false}
        className="tk-scroll block min-h-[8rem] w-full resize-y bg-transparent px-4 py-3 font-mono text-[13px] leading-[1.65] text-ink-soft focus:outline-none"
      />
      {errors.length > 0 && (
        <div className="border-t border-line-soft bg-fail-soft px-4 py-2">
          {errors.map((e, i) => (
            <div key={i} className="flex gap-2 text-[12px]">
              <Mono className="font-semibold text-fail">error</Mono>
              <span className="text-ink-soft">{e.message}</span>
            </div>
          ))}
        </div>
      )}
      <div className="flex items-center gap-2 border-t border-line-soft px-4 py-2">
        <button
          className="rounded-md bg-pass px-3 py-1.5 text-[12px] font-semibold text-surface transition-opacity hover:opacity-90"
          onClick={save}
        >
          Save
        </button>
        <button
          className="rounded-md border border-line px-3 py-1.5 text-[12px] font-medium text-muted transition-colors hover:bg-hover"
          onClick={() => setEditing(false)}
        >
          Cancel
        </button>
        <Mono className="ml-auto text-[10px] text-warn">editing · markdown</Mono>
      </div>
    </div>
  );
}

function ContractSkeleton() {
  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-line px-11 py-3.5">
        <Skeleton className="h-4 w-52" />
      </div>
      <div className="mx-auto grid w-full max-w-[1080px] grid-cols-1 gap-9 px-11 pt-7 lg:grid-cols-[minmax(0,1fr)_280px]">
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
