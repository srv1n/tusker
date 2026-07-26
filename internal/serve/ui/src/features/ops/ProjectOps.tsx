import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { CheckCircle2, CircleSlash, Flag, GitMerge, Play, RotateCcw, Square, Upload } from "lucide-react";
import { Button, Select, TextInput } from "@/components/ui/controls";
import { ActionResultLine, useConfirm } from "@/components/ui/action-feedback";
import { GateKindChip } from "@/components/ui/chips";
import { Mono } from "@/components/ui/primitives";
import { PageScroll, SectionLabel } from "@/components/ui/page";
import { QueryBoundary, SkeletonRows } from "@/components/ui/states";
import { diskPressurePresentation } from "./daemonStatus";
import { FactoryOperationsSurface } from "./FactoryOperations";
import {
  useDaemon,
  useDaemonAction,
  useDecisions,
  useEvidence,
  useEvidenceAdd,
  useFeedback,
  useFeedbackAdd,
  useFactoryOperations,
  useGateAction,
  useGates,
  useLandWave,
  useWaves,
} from "@/lib/queries";
import type { DaemonStatus, GateDetail, WaveBrief as WaveBriefContract, WaveSummary } from "@/types/domain";

const route = getRouteApi("/p/$projectId/ops");

export function ProjectOps() {
  const { projectId } = route.useParams();
  const waves = useWaves(projectId);
  const gates = useGates(undefined, projectId);
  const evidence = useEvidence(undefined, projectId);
  const decisions = useDecisions(undefined, projectId);
  const feedback = useFeedback(projectId);
  const daemon = useDaemon();
  const factoryOperations = useFactoryOperations(projectId);

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

        <QueryBoundary q={factoryOperations} loading={<SkeletonRows rows={6} />}>
          {(projection) => <FactoryOperationsSurface projection={projection} />}
        </QueryBoundary>

        <div className="my-7 border-t border-line" aria-hidden="true" />

        <div className="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
          <div className="min-w-0 space-y-7">
            <QueryBoundary q={waves} loading={<SkeletonRows rows={3} />}>
              {(items) => <WavePanel waves={items} projectId={projectId} />}
            </QueryBoundary>

            <QueryBoundary q={gates} loading={<SkeletonRows rows={3} />}>
              {(items) => <GatePanel gates={items} projectId={projectId} />}
            </QueryBoundary>

            <ReadTables
              evidence={evidence.data ?? []}
              decisions={decisions.data ?? []}
              feedback={feedback.data ?? []}
            />
          </div>

          <aside className="space-y-7">
            <QueryBoundary q={daemon} loading={<SkeletonRows rows={2} />}>
              {(status) => <DaemonPanel status={status} />}
            </QueryBoundary>
            <EvidenceForm projectId={projectId} />
            <FeedbackForm projectId={projectId} />
          </aside>
        </div>
      </div>
    </PageScroll>
  );
}

function WavePanel({ waves, projectId }: { waves: WaveSummary[]; projectId: string }) {
  const landWave = useLandWave(projectId);
  const confirm = useConfirm();

  // Landing a wave merges every member task into the default branch — gate it
  // behind a type-the-wave-id confirm.
  const onLand = async (wave: WaveSummary) => {
    const ok = await confirm({
      title: `Land wave ${wave.id}`,
      body: "This lands every task in the wave to the default branch. Irreversible.",
      confirmLabel: "Land wave",
      tone: "danger",
      typeToConfirm: wave.id,
    });
    if (ok) landWave.mutate(wave.id);
  };

  return (
    <section>
      <SectionLabel className="mb-2.5">Waves</SectionLabel>
      <div className="grid grid-cols-1 gap-2">
        {waves.length === 0 && <EmptyLine text="No waves" />}
        {waves.map((wave) => (
          <div id={`wave-${wave.id}`} key={wave.id} className="rounded-lg border border-line bg-raised px-3.5 py-3">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <Mono className="text-[11px] text-faint">{wave.id}</Mono>
                  <span className="min-w-0 truncate text-[13px] font-semibold text-ink-soft">{wave.title}</span>
                  <Mono className="text-[10.5px] text-faint">{wave.status || "unlanded"}</Mono>
                  <Mono className={wave.authorization.state === "armed" ? "text-[10.5px] text-pass" : "text-[10.5px] text-warn"}>
                    auth:{wave.authorization.state}
                  </Mono>
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
                {wave.authorization.action !== "none" ? <Mono className="mt-1 block text-[10px] text-warn">{wave.authorization.action}</Mono> : null}
                <WaveBriefView brief={wave.brief} />
              </div>
              <Button
                type="button"
                size="sm"
                onClick={() => onLand(wave)}
                disabled={landWave.isPending}
              >
                <GitMerge size={12} />
                Land
              </Button>
            </div>
          </div>
        ))}
      </div>
      <ActionResultLine className="mt-2" pending={landWave.isPending} error={landWave.error} result={landWave.data} />
    </section>
  );
}

export function WaveBriefView({ brief }: { brief: WaveBriefContract }) {
  return (
    <div className="mt-3 space-y-3 border-t border-line pt-3" data-testid="wave-brief">
      <section>
        <div className="flex items-center justify-between gap-2">
          <SectionLabel>Outcome</SectionLabel>
          <a href={brief.waveHref} className="font-mono text-[10px] text-faint hover:text-ink">{brief.waveId}</a>
        </div>
        <p className="mt-1 text-[12px] text-ink-soft">{brief.outcome.summary}</p>
        <div className="mt-1 flex flex-wrap gap-1">
          {brief.outcome.tasks.map((task) => (
            <a key={task.taskId} href={task.taskHref} className="rounded border border-line px-1.5 py-0.5 font-mono text-[10px] text-faint hover:bg-hover">
              {task.taskId} · impl:{task.implementation} proof:{task.proof} review:{task.review} land:{task.landing} docs:{task.documentation}
            </a>
          ))}
        </div>
      </section>
      <section>
        <SectionLabel>See it</SectionLabel>
        {brief.seeIt.length === 0 ? <EmptyLine text="None" /> : (
          <div className="mt-1 grid gap-1.5 sm:grid-cols-2">
            {brief.seeIt.map((artifact, index) => (
              <a key={`${artifact.evidenceRef}-${index}`} href={artifact.evidenceHref} className="rounded-md border border-line bg-surface px-2.5 py-2 hover:bg-hover">
                <div className="flex justify-between gap-2"><Mono className="text-[10px] text-accent">{artifact.kind}</Mono><Mono className="text-[9px] text-faint">{artifact.acceptanceIds.join(",")}</Mono></div>
                <p className="mt-1 text-[11.5px] text-ink-soft">{artifact.summary}</p>
                {artifact.artifactRef ? <Mono className="mt-1 block truncate text-[9.5px] text-faint">{artifact.artifactRef}</Mono> : null}
              </a>
            ))}
          </div>
        )}
      </section>
      <BriefRows title="Landed" empty="None" rows={brief.landed.map((x) => ({ id: x.taskId, text: `${x.title}${x.commit ? ` · ${x.commit}` : ""}`, href: x.taskHref }))} />
      <BriefRows title="Rework/parked" empty="None" rows={brief.reworkParked.map((x) => ({ id: x.taskId, text: x.firstActionableFailure, href: x.taskHref }))} />
      <BriefRows title="Human action" empty="None" rows={brief.humanAction.map((x) => ({ id: x.gateId, text: `${x.action} · resume ${x.resumeId}`, href: x.gateHref }))} />
      <BriefRows title="Documentation" empty="None" rows={brief.documentation.map((x) => ({ id: x.taskId, text: `${x.node} · ${x.state}`, href: x.nodeHref }))} />
    </div>
  );
}

function BriefRows({ title, empty, rows }: { title: string; empty: string; rows: Array<{ id: string; text: string; href: string }> }) {
  return <section><SectionLabel>{title}</SectionLabel>{rows.length === 0 ? <EmptyLine text={empty} /> : <div className="mt-1 space-y-1">{rows.map((row, index) => <a key={`${row.id}-${index}`} href={row.href} className="block text-[11px] text-ink-soft hover:text-ink"><Mono className="mr-1 text-[10px] text-faint">{row.id}</Mono>{row.text}</a>)}</div>}</section>;
}

function GatePanel({ gates, projectId }: { gates: GateDetail[]; projectId: string }) {
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const valueFor = (gate: string) => inputs[gate] ?? "";
  const setValue = (gate: string, value: string) => setInputs((prev) => ({ ...prev, [gate]: value }));

  // One mutation shared across every gate in the panel, so its `isPending`
  // disables the whole group while any single gate action is in flight — no
  // double-fire of satisfy/waive/obsolete.
  const gateAction = useGateAction();
  const confirm = useConfirm();

  const runGate = async (gate: GateDetail, action: "satisfy" | "waive" | "obsolete") => {
    const text = valueFor(gate.id);
    // Waive and obsolete DISCARD a gate — confirm before firing. Satisfy does not.
    if (action === "waive" || action === "obsolete") {
      const ok = await confirm({
        title: `${action === "waive" ? "Waive" : "Obsolete"} ${gate.id}`,
        body:
          action === "waive"
            ? "Waiving discards this gate without satisfying it."
            : "Marking this gate obsolete discards it.",
        confirmLabel: action === "waive" ? "Waive gate" : "Obsolete gate",
        tone: "danger",
      });
      if (!ok) return;
    }
    const body = action === "satisfy" ? { evidence: text } : { reason: text };
    gateAction.mutate({ gateId: gate.id, action, body, taskId: gate.blocks[0], projectId });
  };

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
              <GateButton action="satisfy" disabled={gateAction.isPending} onClick={() => runGate(gate, "satisfy")} />
              <GateButton action="waive" disabled={gateAction.isPending} onClick={() => runGate(gate, "waive")} />
              <GateButton action="obsolete" disabled={gateAction.isPending} onClick={() => runGate(gate, "obsolete")} />
            </div>
          </div>
        ))}
      </div>
      <ActionResultLine className="mt-2" pending={gateAction.isPending} error={gateAction.error} result={gateAction.data} />
    </section>
  );
}

function GateButton({
  action,
  disabled,
  onClick,
}: {
  action: "satisfy" | "waive" | "obsolete";
  disabled: boolean;
  onClick: () => void;
}) {
  const Icon = action === "satisfy" ? CheckCircle2 : action === "waive" ? RotateCcw : CircleSlash;
  return (
    <Button
      type="button"
      size="sm"
      variant={action === "obsolete" ? "danger" : "default"}
      onClick={onClick}
      disabled={disabled}
    >
      <Icon size={12} />
      {action}
    </Button>
  );
}

function DaemonPanel({ status }: { status: DaemonStatus }) {
  const daemonAction = useDaemonAction();
  const confirm = useConfirm();
  const [limit, setLimit] = useState(String(status.maxActiveRuns ?? 2));
  const diskPressure = diskPressurePresentation(status.diskPressure);

  // One mutation for the whole control group → its `isPending` disables every
  // button while any one is in flight.
  const busy = daemonAction.isPending;

  const onStop = async () => {
    const ok = await confirm({
      title: "Stop the daemon",
      body: "Stopping the daemon kills active runs in flight.",
      confirmLabel: "Stop daemon",
      tone: "danger",
    });
    if (ok) daemonAction.mutate({ action: "stop" });
  };

  return (
    <section>
      <SectionLabel className="mb-2.5">Daemon</SectionLabel>
      <div className="rounded-lg border border-line bg-raised px-3.5 py-3">
        <div className="mb-3 grid grid-cols-2 gap-2 font-mono text-[11px] text-faint">
          <span>{status.connected ? "connected" : "offline"}</span>
          <span className="truncate text-right">{status.addr}</span>
          <span>active {status.activeRuns}</span>
          <span className="text-right">queued {status.queuedTasks}</span>
        </div>
        {diskPressure ? (
          <div
            data-daemon-disk-pressure={status.diskPressure?.state}
            className={`mb-3 text-[11px] ${
              diskPressure.tone === "fail"
                ? "text-fail"
                : diskPressure.tone === "warn"
                  ? "text-warn"
                  : diskPressure.tone === "pass"
                    ? "text-pass"
                    : "text-faint"
            }`}
          >
            <span className="font-medium">{diskPressure.label}</span>
            {diskPressure.reason ? <span className="ml-1 text-faint">{diskPressure.reason}</span> : null}
          </div>
        ) : null}
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" disabled={busy} onClick={() => daemonAction.mutate({ action: "start" })}>
            <Play size={12} /> Start
          </Button>
          <Button type="button" size="sm" disabled={busy} onClick={onStop}>
            <Square size={12} /> Stop
          </Button>
          <Button type="button" size="sm" disabled={busy} onClick={() => daemonAction.mutate({ action: "resume" })}>
            <RotateCcw size={12} /> Resume
          </Button>
          <TextInput value={limit} onChange={(e) => setLimit(e.target.value)} className="w-20" />
          <Button type="button" size="sm" disabled={busy} onClick={() => daemonAction.mutate({ action: "limits", body: { maxActiveRuns: Number(limit) } })}>
            Set limit
          </Button>
        </div>
      </div>
      <ActionResultLine className="mt-2" pending={daemonAction.isPending} error={daemonAction.error} result={daemonAction.data} />
    </section>
  );
}

function EvidenceForm({ projectId }: { projectId: string }) {
  const add = useEvidenceAdd(undefined, projectId);
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
      <ActionResultLine className="mt-2" pending={add.isPending} error={add.error} result={add.data} />
    </section>
  );
}

function FeedbackForm({ projectId }: { projectId: string }) {
  const add = useFeedbackAdd(projectId);
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
      <ActionResultLine className="mt-2" pending={add.isPending} error={add.error} result={add.data} />
    </section>
  );
}

function ReadTables({
  evidence,
  decisions,
  feedback,
}: {
  evidence: Array<{ id: string; taskId: string; status: string; kind: string; summary?: string }>;
  decisions: Array<{ id: string; title: string; status: string; decision: string }>;
  feedback: Array<{ id: string; friction: string; productIdea: string; related: string[] }>;
}) {
  return (
    <section>
      <SectionLabel className="mb-2.5">Read views</SectionLabel>
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <MiniTable title="Evidence" rows={evidence.map((e) => [e.id, e.taskId, `${e.kind}/${e.status}`, e.summary ?? ""])} />
        <MiniTable title="Decisions" rows={decisions.map((d) => [d.id, d.status, d.title, d.decision])} />
        <MiniTable title="Feedback" rows={feedback.map((f) => [f.id, f.friction, f.productIdea, f.related.join(", ")])} />
      </div>
    </section>
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

function EmptyLine({ text }: { text: string }) {
  return <div className="px-3 py-3 text-[12.5px] text-faint">{text}</div>;
}
