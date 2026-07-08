import { useMemo, useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { CheckCircle2, CircleSlash, Flag, GitMerge, Play, RotateCcw, Square, Upload } from "lucide-react";
import { Button, Select, TextInput } from "@/components/ui/controls";
import { GateKindChip } from "@/components/ui/chips";
import { Mono } from "@/components/ui/primitives";
import { PageScroll, SectionLabel } from "@/components/ui/page";
import { QueryBoundary, SkeletonRows } from "@/components/ui/states";
import {
  useAttempts,
  useDaemon,
  useDaemonAction,
  useDecisions,
  useEvidence,
  useEvidenceAdd,
  useFeedback,
  useFeedbackAdd,
  useGateAction,
  useGates,
  useLandWave,
  useWaves,
} from "@/lib/queries";
import type { ActionResult, AttemptDetail, GateDetail, WaveSummary } from "@/types/domain";

const route = getRouteApi("/p/$projectId/ops");

export function ProjectOps() {
  const { projectId } = route.useParams();
  const waves = useWaves(projectId);
  const gates = useGates();
  const evidence = useEvidence();
  const decisions = useDecisions();
  const feedback = useFeedback();
  const attempts = useAttempts();
  const daemon = useDaemon();

  return (
    <PageScroll>
      <div className="mx-auto max-w-[1160px] animate-rise">
        <header className="mb-6 flex items-end justify-between gap-4">
          <div>
            <Mono className="mb-1 block text-[11px] text-faint">{projectId}</Mono>
            <h1 className="font-serif text-[30px] font-semibold tracking-[-0.02em] text-ink">Ops</h1>
          </div>
          <Link
            to="/p/$projectId/work"
            params={{ projectId }}
            className="rounded-lg border border-line px-3 py-1.5 text-[12.5px] font-medium text-ink-soft transition-colors hover:bg-hover"
          >
            Work
          </Link>
        </header>

        <div className="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
          <div className="min-w-0 space-y-7">
            <QueryBoundary q={waves} loading={<SkeletonRows rows={3} />}>
              {(items) => <WavePanel waves={items} projectId={projectId} />}
            </QueryBoundary>

            <QueryBoundary q={gates} loading={<SkeletonRows rows={3} />}>
              {(items) => <GatePanel gates={items} />}
            </QueryBoundary>

            <ReadTables
              evidence={evidence.data ?? []}
              decisions={decisions.data ?? []}
              feedback={feedback.data ?? []}
              attempts={attempts.data ?? []}
            />
          </div>

          <aside className="space-y-7">
            <QueryBoundary q={daemon} loading={<SkeletonRows rows={2} />}>
              {(status) => <DaemonPanel status={status} />}
            </QueryBoundary>
            <EvidenceForm />
            <FeedbackForm />
          </aside>
        </div>
      </div>
    </PageScroll>
  );
}

function WavePanel({ waves, projectId }: { waves: WaveSummary[]; projectId: string }) {
  const landWave = useLandWave();
  const [last, setLast] = useState<ActionResult | null>(null);
  return (
    <section>
      <SectionLabel className="mb-2.5">Waves</SectionLabel>
      <div className="grid grid-cols-1 gap-2">
        {waves.length === 0 && <EmptyLine text="No waves" />}
        {waves.map((wave) => (
          <div key={wave.id} className="rounded-lg border border-line bg-raised px-3.5 py-3">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <Mono className="text-[11px] text-faint">{wave.id}</Mono>
                  <span className="truncate text-[13px] font-semibold text-ink-soft">{wave.title}</span>
                  <Mono className="text-[10.5px] text-faint">{wave.status || "unlanded"}</Mono>
                  {wave.landedAt ? <Mono className="text-[10.5px] text-pass">landed {wave.landedAt}</Mono> : null}
                </div>
                <div className="mt-1 flex flex-wrap gap-1.5">
                  {wave.members.map((m) => (
                    <Link
                      key={m.id}
                      to="/p/$projectId/docs"
                      params={{ projectId }}
                      search={{ path: m.id }}
                      className="rounded border border-line bg-surface px-1.5 py-0.5 font-mono text-[10px] text-faint hover:bg-hover"
                    >
                      {m.id}:{m.status}/{m.proof}
                    </Link>
                  ))}
                </div>
              </div>
              <Button
                type="button"
                size="sm"
                onClick={() => landWave.mutate(wave.id, { onSuccess: setLast })}
                disabled={landWave.isPending}
              >
                <GitMerge size={12} />
                Land
              </Button>
            </div>
          </div>
        ))}
      </div>
      <ActionLine pending={landWave.isPending} result={last} />
    </section>
  );
}

function GatePanel({ gates }: { gates: GateDetail[] }) {
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [last, setLast] = useState<ActionResult | null>(null);
  const valueFor = (gate: string) => inputs[gate] ?? "";
  const setValue = (gate: string, value: string) => setInputs((prev) => ({ ...prev, [gate]: value }));

  return (
    <section>
      <SectionLabel className="mb-2.5">Gates</SectionLabel>
      <div className="grid grid-cols-1 gap-2">
        {gates.length === 0 && <EmptyLine text="No gates" />}
        {gates.map((gate) => (
          <div key={gate.id} className="rounded-lg border border-line bg-raised px-3.5 py-3">
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <GateKindChip kind={gate.kind} />
              <Mono className="text-[11px] text-faint">{gate.id}</Mono>
              <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-ink-soft">{gate.title}</span>
              <Mono className="text-[10.5px] text-faint">{gate.status}</Mono>
            </div>
            <div className="flex flex-wrap gap-2">
              <TextInput
                value={valueFor(gate.id)}
                onChange={(e) => setValue(gate.id, e.target.value)}
                placeholder="evidence or reason"
                className="min-w-[220px] flex-1"
              />
              <GateButton gate={gate} action="satisfy" text={valueFor(gate.id)} onResult={setLast} />
              <GateButton gate={gate} action="waive" text={valueFor(gate.id)} onResult={setLast} />
              <GateButton gate={gate} action="obsolete" text={valueFor(gate.id)} onResult={setLast} />
            </div>
          </div>
        ))}
      </div>
      <ActionLine pending={false} result={last} />
    </section>
  );
}

function GateButton({
  gate,
  action,
  text,
  onResult,
}: {
  gate: GateDetail;
  action: "satisfy" | "waive" | "obsolete";
  text: string;
  onResult: (result: ActionResult) => void;
}) {
  const mutation = useGateAction();
  const body = action === "satisfy" ? { evidence: text } : { reason: text };
  const Icon = action === "satisfy" ? CheckCircle2 : action === "waive" ? RotateCcw : CircleSlash;
  return (
    <Button
      type="button"
      size="sm"
      variant={action === "obsolete" ? "danger" : "default"}
      onClick={() => mutation.mutate({ gateId: gate.id, action, body, taskId: gate.blocks[0] }, { onSuccess: onResult })}
      disabled={mutation.isPending}
    >
      <Icon size={12} />
      {action}
    </Button>
  );
}

function DaemonPanel({ status }: { status: { connected: boolean; addr: string; activeRuns: number; queuedTasks: number; maxActiveRuns?: number } }) {
  const daemonAction = useDaemonAction();
  const [limit, setLimit] = useState(String(status.maxActiveRuns ?? 2));
  const [last, setLast] = useState<ActionResult | null>(null);
  return (
    <section>
      <SectionLabel className="mb-2.5">Daemon</SectionLabel>
      <div className="rounded-lg border border-line bg-raised px-3.5 py-3">
        <div className="mb-3 grid grid-cols-2 gap-2 font-mono text-[11px] text-faint">
          <span>{status.connected ? "connected" : "offline"}</span>
          <span className="text-right">{status.addr}</span>
          <span>active {status.activeRuns}</span>
          <span className="text-right">queued {status.queuedTasks}</span>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" onClick={() => daemonAction.mutate({ action: "start" }, { onSuccess: setLast })}>
            <Play size={12} /> Start
          </Button>
          <Button type="button" size="sm" onClick={() => daemonAction.mutate({ action: "stop" }, { onSuccess: setLast })}>
            <Square size={12} /> Stop
          </Button>
          <Button type="button" size="sm" onClick={() => daemonAction.mutate({ action: "resume" }, { onSuccess: setLast })}>
            <RotateCcw size={12} /> Resume
          </Button>
          <TextInput value={limit} onChange={(e) => setLimit(e.target.value)} className="w-20" />
          <Button type="button" size="sm" onClick={() => daemonAction.mutate({ action: "limits", body: { maxActiveRuns: Number(limit) } }, { onSuccess: setLast })}>
            Set limit
          </Button>
        </div>
      </div>
      <ActionLine pending={daemonAction.isPending} result={last} />
    </section>
  );
}

function EvidenceForm() {
  const add = useEvidenceAdd();
  const [taskId, setTaskId] = useState("");
  const [covers, setCovers] = useState("A1");
  const [summary, setSummary] = useState("");
  const [kind, setKind] = useState("automated_test");
  const [status, setStatus] = useState("accepted");
  const [acceptedBy, setAcceptedBy] = useState("");
  return (
    <section>
      <SectionLabel className="mb-2.5">Evidence add</SectionLabel>
      <div className="space-y-2 rounded-lg border border-line bg-raised p-3.5">
        <TextInput value={taskId} onChange={(e) => setTaskId(e.target.value)} placeholder="task id" className="w-full" />
        <div className="flex gap-2">
          <Select value={kind} onChange={(e) => setKind(e.target.value)} className="min-w-0 flex-1">
            <option value="automated_test">automated_test</option>
            <option value="manual_smoke">manual_smoke</option>
            <option value="human_review">human_review</option>
            <option value="verification_summary">verification_summary</option>
          </Select>
          <Select value={status} onChange={(e) => setStatus(e.target.value)} className="min-w-0 flex-1">
            <option value="accepted">accepted</option>
            <option value="pending_review">pending_review</option>
          </Select>
        </div>
        <TextInput value={covers} onChange={(e) => setCovers(e.target.value)} placeholder="covers" className="w-full" />
        <TextInput value={summary} onChange={(e) => setSummary(e.target.value)} placeholder="summary" className="w-full" />
        <TextInput value={acceptedBy} onChange={(e) => setAcceptedBy(e.target.value)} placeholder="accepted by" className="w-full" />
        <Button type="button" size="sm" onClick={() => add.mutate({ taskId, kind, covers, status, summary, acceptedBy })} disabled={add.isPending}>
          <Upload size={12} /> Add
        </Button>
      </div>
      <ActionLine pending={add.isPending} result={add.data ?? null} />
    </section>
  );
}

function FeedbackForm() {
  const add = useFeedbackAdd();
  const [context, setContext] = useState("");
  const [friction, setFriction] = useState("");
  const [idea, setIdea] = useState("");
  const [impact, setImpact] = useState("");
  const [related, setRelated] = useState("");
  return (
    <section>
      <SectionLabel className="mb-2.5">Feedback add</SectionLabel>
      <div className="space-y-2 rounded-lg border border-line bg-raised p-3.5">
        <TextInput value={context} onChange={(e) => setContext(e.target.value)} placeholder="context" className="w-full" />
        <TextInput value={friction} onChange={(e) => setFriction(e.target.value)} placeholder="friction" className="w-full" />
        <TextInput value={idea} onChange={(e) => setIdea(e.target.value)} placeholder="product idea" className="w-full" />
        <TextInput value={impact} onChange={(e) => setImpact(e.target.value)} placeholder="impact" className="w-full" />
        <TextInput value={related} onChange={(e) => setRelated(e.target.value)} placeholder="related" className="w-full" />
        <Button type="button" size="sm" onClick={() => add.mutate({ context, friction, productIdea: idea, impact, related })} disabled={add.isPending}>
          <Flag size={12} /> Add
        </Button>
      </div>
      <ActionLine pending={add.isPending} result={add.data ?? null} />
    </section>
  );
}

function ReadTables({
  evidence,
  decisions,
  feedback,
  attempts,
}: {
  evidence: Array<{ id: string; taskId: string; status: string; kind: string; summary?: string }>;
  decisions: Array<{ id: string; title: string; status: string; decision: string }>;
  feedback: Array<{ id: string; friction: string; productIdea: string; related: string[] }>;
  attempts: AttemptDetail[];
}) {
  const totalAttempts = useMemo(() => attempts.length, [attempts]);
  const latestAttempt = attempts[0];
  return (
    <section>
      <SectionLabel className="mb-2.5">Read views</SectionLabel>
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <MiniTable title="Evidence" rows={evidence.map((e) => [e.id, e.taskId, `${e.kind}/${e.status}`, e.summary ?? ""])} />
        <MiniTable title="Decisions" rows={decisions.map((d) => [d.id, d.status, d.title, d.decision])} />
        <MiniTable title="Feedback" rows={feedback.map((f) => [f.id, f.friction, f.productIdea, f.related.join(", ")])} />
        <MiniTable title={`Attempts ${totalAttempts}`} rows={attempts.map((a) => [a.id, a.taskId, a.outcome, `${a.tokens.input}/${a.tokens.output}`])} />
      </div>
      {latestAttempt ? <AttemptDetailPanel attempt={latestAttempt} /> : null}
    </section>
  );
}

function AttemptDetailPanel({ attempt }: { attempt: AttemptDetail }) {
  const path = attempt.workspacePath || attempt.rawLogPath || attempt.statusPath || "";
  return (
    <div className="mt-3 rounded-lg border border-line bg-raised p-3.5">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <SectionLabel>Latest attempt</SectionLabel>
        <Mono className="text-[10.5px] text-faint">{attempt.id}</Mono>
        <Mono className="text-[10.5px] text-faint">{attempt.runner || "runner unknown"}</Mono>
        <Mono className="text-[10.5px] text-faint">{attempt.outcome || "pending"}</Mono>
      </div>
      <div className="grid gap-2 text-[12.5px] text-ink-soft md:grid-cols-2">
        <span className="truncate">Task {attempt.taskId}</span>
        <span className="truncate">Lane {attempt.lane || "default"}</span>
        <span className="truncate">Branch {attempt.branchName || "none"}</span>
        <span className="truncate">Turns {attempt.turns.length} / events {attempt.events.length}</span>
        {path ? <Mono className="truncate text-[10.5px] text-faint md:col-span-2">{path}</Mono> : null}
      </div>
      {attempt.lastError ? <p className="mt-2 text-[12.5px] text-fail">{attempt.lastError}</p> : null}
      {attempt.finalSummary ? <p className="mt-2 line-clamp-3 text-[12.5px] text-ink-soft">{attempt.finalSummary}</p> : null}
      {!attempt.finalSummary && attempt.logsSummary ? <p className="mt-2 line-clamp-3 text-[12.5px] text-faint">{attempt.logsSummary}</p> : null}
    </div>
  );
}

function MiniTable({ title, rows }: { title: string; rows: string[][] }) {
  return (
    <div className="overflow-hidden rounded-lg border border-line bg-raised">
      <div className="border-b border-line px-3 py-2">
        <SectionLabel>{title}</SectionLabel>
      </div>
      {rows.length === 0 ? (
        <EmptyLine text="None" />
      ) : (
        rows.slice(0, 8).map((row, i) => (
          <div key={i} className="grid grid-cols-[120px_1fr] gap-3 border-b border-line-soft px-3 py-2 last:border-0">
            <Mono className="truncate text-[10.5px] text-faint">{row[0]}</Mono>
            <span className="truncate text-[12.5px] text-ink-soft">{row.slice(1).filter(Boolean).join(" · ")}</span>
          </div>
        ))
      )}
    </div>
  );
}

function ActionLine({ pending, result }: { pending: boolean; result: ActionResult | null }) {
  if (pending) return <div className="mt-2 text-[12px] text-faint">Working...</div>;
  if (!result) return null;
  const refused = result.refused || !result.ok;
  return (
    <div className={refused ? "mt-2 text-[12px] text-fail" : "mt-2 text-[12px] text-pass"}>
      {result.reason}
    </div>
  );
}

function EmptyLine({ text }: { text: string }) {
  return <div className="px-3 py-3 text-[12.5px] text-faint">{text}</div>;
}
