import { ExternalLink, ShieldCheck } from "lucide-react";
import type { ReactNode } from "react";
import { Chip, Mono } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import type {
  FactoryOperationsDecision,
  FactoryOperationsItem,
  FactoryOperationsProjection,
} from "@/types/domain";

const sections = [
  { key: "delivered", title: "Delivered", empty: "No delivered work is recorded yet." },
  { key: "workingNow", title: "Working now", empty: "No implementation or promotion is running." },
  { key: "reviewOrRework", title: "In review or rework", empty: "No objective review or machine rework is pending." },
  { key: "blocked", title: "Blocked", empty: "No machine-owned blocker is holding work." },
  { key: "needsYourDecision", title: "Needs your decision", empty: "No genuine human gate needs a decision." },
  { key: "nextFrontier", title: "Next frontier", empty: "No work is currently admitted to the next frontier." },
] as const;

export function FactoryOperationsSurface({ projection }: { projection: FactoryOperationsProjection }) {
  return (
    <section aria-labelledby="factory-operations-title" className="min-w-0 space-y-5">
      <header className="flex flex-col gap-2 border-b border-line pb-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="min-w-0">
          <div className="mb-1 flex items-center gap-2">
            <ShieldCheck size={14} className="text-pass" aria-hidden="true" />
            <Mono className="text-[10px] text-faint">{projection.schema}</Mono>
            <Chip tone="neutral" mono>read only</Chip>
          </div>
          <h2 id="factory-operations-title" className="font-serif text-[24px] font-semibold tracking-[-0.015em] text-ink">
            Factory operations
          </h2>
          <p className="mt-1 text-[12.5px] text-muted">
            Product outcomes and exact control-plane facts. Runner exhaust stays out of this view.
          </p>
        </div>
        <Mono className="text-[10px] text-faint">as of {projection.generatedAt}</Mono>
      </header>

      <OperationsPosture projection={projection} />

      <div className="grid min-w-0 grid-cols-1 gap-4 xl:grid-cols-2">
        {sections.map((section) => {
          const value = projection[section.key];
          return (
            <section
              key={section.key}
              aria-labelledby={`factory-${section.key}`}
              className="min-w-0 rounded-xl border border-line bg-raised p-3.5"
            >
              <div className="mb-2.5 flex items-baseline justify-between gap-3">
                <h3 id={`factory-${section.key}`} className="text-[13.5px] font-semibold text-ink">
                  {section.title}
                </h3>
                <Mono className="text-[10px] text-faint">{value.length}</Mono>
              </div>
              {section.key === "needsYourDecision" ? (
                <DecisionRows decisions={projection.needsYourDecision} empty={section.empty} />
              ) : (
                <OperationRows items={value as FactoryOperationsItem[]} empty={section.empty} />
              )}
            </section>
          );
        })}
      </div>
    </section>
  );
}

function OperationsPosture({ projection }: { projection: FactoryOperationsProjection }) {
  const { project, authority, capacity } = projection;
  return (
    <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4" aria-label="Factory posture">
      <PostureCard label="Automation">
        <div className="flex flex-wrap items-center gap-1.5">
          <Chip tone={project.registered ? "pass" : "warn"}>
            {project.registered ? "registered" : "unregistered"}
          </Chip>
          <Chip tone={project.enabled ? "pass" : "neutral"}>
            {project.enabled ? "pickup enabled" : "pickup disabled"}
          </Chip>
          <Chip tone={project.automationEnabled ? "pass" : "neutral"}>
            automation {project.automationEnabled ? "enabled" : "disabled"}
          </Chip>
        </div>
        <FactLine label="health" value={project.health} />
        <FactLine label="auto source" value={project.automationProvenance} />
        <FactLine label="scope" value={`${project.dispatchScope.configured ?? "unset"} → ${project.dispatchScope.effective}`} />
        <FactLine label="source" value={project.dispatchScope.provenance} />
      </PostureCard>

      <PostureCard label="Integration & promotion">
        <FactLine label="completion" value={`${project.completionMode.configured ?? "unset"} → ${project.completionMode.effective} · ${project.completionMode.provenance}`} />
        <FactLine label="promotion" value={`${project.promotionMode.mode} · observe:${project.promotionMode.observe} stage:${project.promotionMode.stage} promote:${project.promotionMode.promote} release:${project.promotionMode.release}`} />
        <FactLine label="source" value={project.promotionMode.provenance || "default"} />
        <FactLine label="default" value={`${authority.defaultRef}${authority.defaultSha ? ` @ ${shortRevision(authority.defaultSha)}` : ""}`} />
      </PostureCard>

      <PostureCard label="Capacity">
        <FactLine label="project" value={`${capacity.project.active}/${capacity.project.limit} active · ${capacity.project.available} free`} />
        <FactLine label="global" value={`${capacity.global.active}/${capacity.global.limit} active · ${capacity.global.available} free`} />
        <FactLine label="resources" value={`${capacity.resourceHolds.length} held`} />
      </PostureCard>

      <PostureCard label="Armed fingerprints">
        {authority.waves.length === 0 ? (
          <p className="text-[11.5px] text-faint">No waves.</p>
        ) : (
          <div className="space-y-2">
            {authority.waves.map((wave) => (
              <div key={wave.waveId} className="min-w-0">
                <div className="flex min-w-0 items-center gap-1.5">
                  <a href={wave.href} className="font-mono text-[10px] text-accent hover:text-ink">{wave.waveId}</a>
                  <Chip tone={wave.fingerprintHealth === "current" ? "pass" : "warn"} mono>
                    {wave.state}/{wave.fingerprintHealth}
                  </Chip>
                </div>
                <Mono className="mt-0.5 block break-words text-[9.5px] text-faint">{wave.integrationRef}</Mono>
                <Mono className="mt-0.5 block break-words text-[9.5px] text-faint">
                  current {wave.currentFingerprint ?? "—"} · armed {wave.authorizedFingerprint ?? "—"}
                </Mono>
                <Mono className="mt-0.5 block break-words text-[9.5px] text-muted">{wave.safeAction}</Mono>
              </div>
            ))}
          </div>
        )}
      </PostureCard>

      {capacity.resourceHolds.length > 0 ? (
        <div className="min-w-0 rounded-xl border border-warn/25 bg-warn-soft p-3 sm:col-span-2 xl:col-span-4">
          <SectionLabel>Capacity / resource holds</SectionLabel>
          <div className="mt-2 flex min-w-0 flex-wrap gap-2">
            {capacity.resourceHolds.map((hold) => (
              <span key={`${hold.name}-${hold.taskId ?? hold.projectId}`} className="max-w-full rounded-md border border-warn/20 bg-surface px-2 py-1">
                <Mono className="break-words text-[10px] text-warn">{hold.name}</Mono>
                <span className="ml-1.5 text-[10.5px] text-muted">{hold.purpose}{hold.taskId ? ` · ${hold.taskId}` : ""}</span>
              </span>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function PostureCard({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section className="min-w-0 rounded-xl border border-line bg-panel p-3">
      <SectionLabel className="mb-2">{label}</SectionLabel>
      <div className="min-w-0 space-y-1.5">{children}</div>
    </section>
  );
}

function FactLine({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid min-w-0 grid-cols-[68px_minmax(0,1fr)] gap-2 text-[10.5px]">
      <span className="text-faint">{label}</span>
      <Mono className="min-w-0 break-words text-muted">{value}</Mono>
    </div>
  );
}

function OperationRows({ items, empty }: { items: FactoryOperationsItem[]; empty: string }) {
  if (items.length === 0) return <EmptyProjection text={empty} />;
  return (
    <div className="min-w-0 space-y-2">
      {items.map((item) => (
        <article key={`${item.kind}-${item.id}`} className="min-w-0 rounded-lg border border-line bg-surface p-3">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <a href={item.href} className="inline-flex min-w-0 items-center gap-1 font-mono text-[10.5px] text-accent hover:text-ink">
              <span className="break-words">{item.id}</span><ExternalLink size={10} aria-hidden="true" />
            </a>
            <Chip tone={stateTone(item.state)} mono>{humanize(item.state)}</Chip>
            {item.waveId ? <Mono className="text-[9.5px] text-faint">{item.waveId}</Mono> : null}
          </div>
          <h4 className="mt-1.5 break-words text-[12.5px] font-medium text-ink-soft">{item.title}</h4>
          <p className="mt-1 break-words text-[12px] leading-relaxed text-ink-soft">{item.productOutcome}</p>
          {item.cause ? <p className="mt-1.5 break-words text-[11px] leading-relaxed text-warn">Cause: {item.cause}</p> : null}
          <p className="mt-1.5 break-words text-[11px] leading-relaxed text-muted">
            <span className="font-medium text-ink-soft">Automatic next:</span> {item.automaticNextAction}
          </p>
          <Mono className="mt-1.5 block break-words rounded bg-panel px-2 py-1 text-[9.5px] text-faint">{item.safeAction}</Mono>
          <OperationEvidence item={item} />
        </article>
      ))}
    </div>
  );
}

function OperationEvidence({ item }: { item: FactoryOperationsItem }) {
  const revisions = [
    ["state", item.revisions.stateRevision],
    ["work", item.revisions.workRevision?.toString()],
    ["implementation", item.revisions.implementationSha],
    ["review result", item.revisions.resultRevision],
    ["integration", revisionPair(item.revisions.integrationRef, item.revisions.integrationSha)],
    ["default", revisionPair(item.revisions.defaultRef, item.revisions.defaultSha)],
  ].filter((entry): entry is [string, string] => Boolean(entry[1]));
  if (item.acceptedArtifacts.length === 0 && revisions.length === 0 && item.affectedTaskIds.length === 0) return null;
  return (
    <div className="mt-2 border-t border-line pt-2 text-[10px]">
      {item.affectedTaskIds.length > 0 ? (
        <div className="flex min-w-0 flex-wrap gap-1">
          <span className="text-faint">Affected</span>
          {item.affectedTaskIds.map((id) => <Mono key={id} className="break-words text-muted">{id}</Mono>)}
        </div>
      ) : null}
      {revisions.length > 0 ? (
        <div className="mt-1 grid min-w-0 grid-cols-1 gap-x-3 gap-y-0.5 sm:grid-cols-2">
          {revisions.map(([label, value]) => <FactLine key={label} label={label} value={value} />)}
        </div>
      ) : null}
      {item.acceptedArtifacts.length > 0 ? (
        <div className="mt-1.5 flex min-w-0 flex-wrap gap-1">
          {item.acceptedArtifacts.map((artifact, index) => (
            <a
              key={`${artifact.evidenceRef}-${artifact.artifactRef ?? index}`}
              href={artifact.evidenceHref}
              className="max-w-full break-words rounded border border-line px-1.5 py-0.5 text-[9.5px] text-accent hover:bg-hover"
            >
              {artifact.kind} · {artifact.summary}
            </a>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function DecisionRows({ decisions, empty }: { decisions: FactoryOperationsDecision[]; empty: string }) {
  if (decisions.length === 0) return <EmptyProjection text={empty} />;
  return (
    <div className="min-w-0 space-y-2">
      {decisions.map((decision) => (
        <article key={decision.gateId} className="min-w-0 rounded-lg border border-warn/30 bg-warn-soft p-3">
          <div className="flex flex-wrap items-center gap-1.5">
            <a href={decision.href} className="font-mono text-[10.5px] text-accent hover:text-ink">{decision.gateId}</a>
            <Chip tone="warn" mono>{decision.owner}</Chip>
          </div>
          <p className="mt-1.5 break-words text-[12px] font-medium text-ink">{decision.action}</p>
          <p className="mt-1 break-words text-[11px] leading-relaxed text-muted">Why human: {decision.whyHuman}</p>
          <p className="mt-1 break-words text-[11px] leading-relaxed text-muted">Verify: {decision.verification}</p>
          <p className="mt-1 break-words text-[11px] leading-relaxed text-muted">Automatic next: {decision.automaticNextAction}</p>
          <Mono className="mt-1.5 block break-words rounded bg-surface px-2 py-1 text-[9.5px] text-warn">{decision.safeAction}</Mono>
          <div className="mt-1.5 flex min-w-0 flex-wrap gap-1">
            {decision.affectedTaskIds.map((id) => <Mono key={id} className="break-words text-[9.5px] text-faint">{id}</Mono>)}
          </div>
        </article>
      ))}
    </div>
  );
}

function EmptyProjection({ text }: { text: string }) {
  return <p className="rounded-lg border border-dashed border-line px-3 py-5 text-center text-[11.5px] text-faint">{text}</p>;
}

function revisionPair(ref: string | undefined, sha: string | undefined): string | undefined {
  if (!ref && !sha) return undefined;
  return `${ref ?? "revision"}${sha ? ` @ ${shortRevision(sha)}` : ""}`;
}

function shortRevision(value: string): string {
  return value.length > 12 ? value.slice(0, 12) : value;
}

function humanize(value: string): string {
  return value.replaceAll("_", " ").replaceAll("-", " ");
}

function stateTone(state: string): "pass" | "warn" | "fail" | "accent" | "neutral" {
  if (["delivered", "integrated", "promoted"].includes(state)) return "pass";
  if (["running", "ready"].includes(state)) return "accent";
  if (state.includes("blocked") || state.includes("parked") || state.includes("stale")) return "fail";
  if (state.includes("review") || state.includes("rework") || state.includes("waiting")) return "warn";
  return "neutral";
}
