import { ArrowLeft, CircleAlert, ExternalLink, FileCheck2, FileWarning, Image, ShieldCheck } from "lucide-react";
import type { ReactNode } from "react";
import { Card, Chip, Mono } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import { humanizeToken, proofTone, statusToneOf } from "@/components/ui/tone";
import type { Tone } from "@/components/ui/tone";
import type { TaskDetail, WaveArtifactCard, WaveBrief, WaveSummary, WaveTaskDeliveryState } from "@/types/domain";

const supportedArtifactKinds = new Set(["diff", "file", "image", "jpeg", "jpg", "link", "log", "png", "screenshot", "webp"]);

export function artifactKindLabel(kind: string): string {
  const normalized = kind.trim().toLowerCase();
  // Do not infer before/after from filenames or from two unrelated artifacts.
  if (["image", "jpeg", "jpg", "png", "screenshot", "webp"].includes(normalized)) return "Image · single";
  if (supportedArtifactKinds.has(normalized)) return humanizeToken(normalized);
  return `Unsupported type · ${kind.trim() || "not supplied"}`;
}

export function artifactPresentation(artifact: Pick<WaveArtifactCard, "kind" | "artifactRef">): {
  label: string;
  tone: Tone;
  state: string;
} {
  const supported = supportedArtifactKinds.has(artifact.kind.trim().toLowerCase());
  if (!artifact.artifactRef?.trim()) return { label: artifactKindLabel(artifact.kind), tone: "warn", state: "Asset unavailable" };
  if (!supported) return { label: artifactKindLabel(artifact.kind), tone: "warn", state: "Unsupported asset" };
  return { label: artifactKindLabel(artifact.kind), tone: "pass", state: "Accepted evidence" };
}

export function resultTrustSummary(brief: Pick<WaveBrief, "outcome" | "seeIt">): {
  drained: boolean;
  acceptedEvidence: number;
  hasEvidenceGap: boolean;
} {
  return {
    drained: brief.outcome.fullyDrained,
    acceptedEvidence: brief.seeIt.length,
    hasEvidenceGap: brief.seeIt.length === 0,
  };
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="border-l border-line pl-3">
      <Mono className="text-[10px] uppercase tracking-[0.12em] text-faint">{label}</Mono>
      <div className="mt-1 text-[13px] text-ink-soft">{children}</div>
    </div>
  );
}

function OutcomeFact({ label, value }: { label: string; value: string }) {
  return <Fact label={label}>{value.trim() || "Not supplied"}</Fact>;
}

function AcceptanceFacts({ task }: { task: TaskDetail }) {
  if (task.acceptance.length === 0) return <p className="text-[12px] text-muted">No acceptance rows supplied.</p>;
  return (
    <div className="space-y-2">
      {task.acceptance.map((row) => (
        <div key={row.id} className="flex items-start gap-2 text-[12px] text-muted">
          <Chip tone={proofTone[row.proof]} mono>{row.proof}</Chip>
          <span>{row.id}: {row.text}</span>
        </div>
      ))}
    </div>
  );
}

function TaskEvidence({ task }: { task: TaskDetail }) {
  if (task.evidence.length === 0) return <span className="text-[12px] text-faint">No task evidence supplied.</span>;
  return (
    <div className="flex flex-wrap gap-x-3 gap-y-1.5">
      {task.evidence.map((evidence) => (
        <a key={evidence.id} href={evidence.ref} className="inline-flex items-center gap-1 text-[12px] text-info hover:text-ink">
          {evidence.kind === "image" ? <Image size={13} aria-hidden="true" /> : <FileCheck2 size={13} aria-hidden="true" />}
          <span>{evidence.label}</span>
          <ExternalLink size={11} aria-hidden="true" />
        </a>
      ))}
    </div>
  );
}

function TaskResult({ task, outcome, onOpenTask }: { task: TaskDetail; outcome?: WaveTaskDeliveryState; onOpenTask: (id: string) => void }) {
  return (
    <Card className="p-4 sm:p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <button type="button" onClick={() => onOpenTask(task.id)} className="text-left text-[15px] font-medium text-ink hover:text-info">
            {task.title}
          </button>
          <p className="mt-1 text-[12.5px] leading-relaxed text-muted">{task.intent.trim() || "Intent not supplied."}</p>
        </div>
        <Chip tone={statusToneOf(task.status)}>{humanizeToken(task.status)}</Chip>
      </div>

      <div className="mt-5 grid gap-4 border-t border-line pt-4 sm:grid-cols-2 lg:grid-cols-4">
        <OutcomeFact label="Implementation" value={outcome?.implementation ?? "Not supplied"} />
        <OutcomeFact label="Proof" value={outcome?.proof ?? "Not supplied"} />
        <OutcomeFact label="Independent review" value={outcome?.review ?? "Not supplied"} />
        <OutcomeFact label="Landing" value={outcome?.landing ?? "Not supplied"} />
      </div>

      <div className="mt-5 grid gap-5 border-t border-line pt-4 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
        <div>
          <SectionLabel className="mb-2">Acceptance</SectionLabel>
          <AcceptanceFacts task={task} />
        </div>
        <div>
          <SectionLabel className="mb-2">Available task evidence</SectionLabel>
          <TaskEvidence task={task} />
        </div>
      </div>

      {outcome?.firstActionableFailure ? (
        <div className="mt-4 flex items-start gap-2 rounded-lg border border-warn/30 bg-warn-soft px-3 py-2 text-[12px] text-warn">
          <CircleAlert size={15} className="mt-0.5 shrink-0" aria-hidden="true" />
          <span>{outcome.firstActionableFailure}</span>
        </div>
      ) : null}
    </Card>
  );
}

function AcceptedArtifact({ artifact }: { artifact: WaveArtifactCard }) {
  const presentation = artifactPresentation(artifact);
  return (
    <Card className="p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <Chip tone={presentation.tone}>{presentation.state}</Chip>
            <Mono className="text-[10px] uppercase tracking-[0.1em] text-faint">{presentation.label}</Mono>
          </div>
          <p className="mt-2 text-[13px] leading-relaxed text-ink-soft">{artifact.summary}</p>
          <p className="mt-2 text-[11.5px] text-muted">
            {artifact.acceptanceIds.length ? `Covers ${artifact.acceptanceIds.join(", ")}` : "Acceptance coverage not supplied"}
          </p>
        </div>
        {artifact.evidenceHref ? (
          <a href={artifact.evidenceHref} className="inline-flex shrink-0 items-center gap-1 text-[12px] font-medium text-info hover:text-ink">
            Evidence <ExternalLink size={13} aria-hidden="true" />
          </a>
        ) : <span className="text-[11px] text-faint">Evidence link unavailable</span>}
      </div>
    </Card>
  );
}

export function WaveResults({ wave, tasks, onOpenFlow, onOpenTask }: {
  wave: WaveSummary;
  tasks: TaskDetail[];
  onOpenFlow: () => void;
  onOpenTask: (id: string) => void;
}) {
  const trust = resultTrustSummary(wave.brief);
  const outcomes = new Map(wave.brief.outcome.tasks.map((task) => [task.taskId, task]));
  const counts = Object.entries(wave.brief.outcome.counts);

  return (
    <main data-wux-ready="true" className="min-h-full bg-surface px-4 py-5 text-ink sm:px-6 lg:px-8 lg:py-8">
      <div className="mx-auto w-full max-w-[1120px]">
        <header className="flex flex-wrap items-start justify-between gap-4 border-b border-line pb-6">
          <div className="min-w-0 max-w-[760px]">
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <SectionLabel>Completed wave · results</SectionLabel>
              <Chip tone="pass" mono>Outcome reader</Chip>
            </div>
            <h1 className="font-serif text-[28px] font-semibold leading-tight tracking-[-0.02em] text-ink sm:text-[34px]">{wave.title}</h1>
            <p className="mt-3 max-w-[720px] text-[14px] leading-relaxed text-muted">{wave.brief.outcome.summary || "Outcome summary not supplied."}</p>
          </div>
          <button type="button" onClick={onOpenFlow} className="inline-flex items-center gap-2 rounded-lg border border-line bg-raised px-3 py-2 text-[12px] font-medium text-ink-soft hover:border-ink hover:text-ink">
            <ArrowLeft size={15} aria-hidden="true" /> Open flow
          </button>
        </header>

        <section className="mt-6 grid gap-3 sm:grid-cols-3" aria-label="Outcome summary">
          <Fact label="Record state"><Chip tone="pass">Completed record</Chip></Fact>
          <Fact label="Process state"><Chip tone={trust.drained ? "muted" : "warn"}>{trust.drained ? "Work drained" : "Drain not supplied"}</Chip></Fact>
          <Fact label="Accepted evidence"><Chip tone={trust.acceptedEvidence ? "pass" : "warn"}>{trust.acceptedEvidence} artifact{trust.acceptedEvidence === 1 ? "" : "s"}</Chip></Fact>
        </section>

        <section className="mt-8" aria-labelledby="delivered-heading">
          <div className="mb-3 flex flex-wrap items-end justify-between gap-2">
            <div><SectionLabel>01 · Delivered outcome</SectionLabel><h2 id="delivered-heading" className="mt-1 font-serif text-[22px] font-semibold">What this wave delivered</h2></div>
            {counts.length ? <div className="flex flex-wrap gap-2">{counts.map(([key, value]) => <Chip key={key} mono>{humanizeToken(key)} {value}</Chip>)}</div> : null}
          </div>
          <Card className="p-4 sm:p-5">
            <p className="text-[14px] leading-relaxed text-ink-soft">{wave.brief.outcome.summary || "No delivered outcome was supplied."}</p>
            <p className="mt-3 text-[12px] text-muted">The task cards below preserve each task’s supplied intent and delivery facts; no result is inferred from process state.</p>
          </Card>
        </section>

        <section className="mt-8" aria-labelledby="acceptance-heading">
          <div className="mb-3"><SectionLabel>02 · Acceptance and review</SectionLabel><h2 id="acceptance-heading" className="mt-1 font-serif text-[22px] font-semibold">Independent review facts</h2></div>
          <div className="space-y-3">
            {tasks.length ? tasks.map((task) => <TaskResult key={task.id} task={task} outcome={outcomes.get(task.id)} onOpenTask={onOpenTask} />) : <Card className="p-5 text-[13px] text-muted">No task details were supplied.</Card>}
          </div>
        </section>

        <section className="mt-8" aria-labelledby="evidence-heading">
          <div className="mb-3 flex flex-wrap items-end justify-between gap-2">
            <div><SectionLabel>03 · Evidence</SectionLabel><h2 id="evidence-heading" className="mt-1 font-serif text-[22px] font-semibold">Accepted evidence</h2></div>
            <span className="text-[12px] text-muted">Canonical acceptance only</span>
          </div>
          {trust.hasEvidenceGap ? (
            <Card className="border-warn/40 bg-warn-soft p-5">
              <div className="flex items-start gap-3">
                <FileWarning size={18} className="mt-0.5 shrink-0 text-warn" aria-hidden="true" />
                <div>
                  <h3 className="text-[14px] font-semibold text-ink">Evidence gap: no accepted evidence is attached</h3>
                  <p className="mt-1 text-[12.5px] leading-relaxed text-muted">This record is drained, but drained work, successful processes and uploaded artifacts do not prove acceptance. Review the task evidence and acceptance rows before calling the outcome verified.</p>
                </div>
              </div>
            </Card>
          ) : (
            <div className="space-y-3">{wave.brief.seeIt.map((artifact) => <AcceptedArtifact key={`${artifact.taskId}-${artifact.evidenceRef}`} artifact={artifact} />)}</div>
          )}
        </section>

        <footer className="mt-8 flex flex-wrap items-center gap-2 border-t border-line pt-4 text-[11.5px] text-muted">
          <ShieldCheck size={14} aria-hidden="true" />
          <span>Evidence eligibility follows the canonical accepted record. Flow remains available from the action above.</span>
        </footer>
      </div>
    </main>
  );
}
