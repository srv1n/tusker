import { useEffect, useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ActionResultLine, useConfirm } from "@/components/ui/action-feedback";
import { Chip, Mono } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import { sourceTone, type SettingSource } from "../app/mock";
import { useConfigResolve, useProjectSettings, useRemoveProject, useSetupDoctor } from "@/lib/queries";
import type { ProjectSummary, SetupFinding, SetupDoctorReport } from "@/types/domain";
import type { Tone } from "@/components/ui/tone";
import { ReadOnlyTag, SectionCard } from "./parts";

const configRows = [
  {
    key: "tier",
    label: "Progressive tier",
    description: "Controls the project's progressive delivery and validation level.",
    kind: "tier",
  },
  {
    key: "automation.enabled_runners",
    label: "Enabled runners",
    description: "Comma-separated runner names accepted by the daemon for automation.",
    kind: "runners",
  },
  {
    key: "automation.enabled",
    label: "Automation enabled",
    description: "Read-only here; use the Auto-spawn eligible tasks toggle above to change this value.",
    kind: "readonly",
  },
  {
    key: "automation.dispatch_scope",
    label: "Dispatch scope",
    description: "The daemon's effective automation dispatch scope.",
    kind: "readonly",
  },
] as const;

type ConfigRowDefinition = (typeof configRows)[number];
type SettingsMutation = ReturnType<typeof useProjectSettings>;

function sourceKey(source: string): SettingSource {
  const normalized = source.toLowerCase();
  if (normalized.includes("global")) return "global";
  if (normalized.includes("project")) return "project";
  if (normalized.includes("local")) return "local";
  if (normalized.includes("built-in") || normalized.includes("default")) return "default";
  return source in sourceTone ? (source as SettingSource) : "default";
}

function formatValue(value: unknown): string {
  if (Array.isArray(value)) return value.join(", ");
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "boolean") return value ? "enabled" : "disabled";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function formatEditorValue(value: unknown): string {
  if (Array.isArray(value)) return value.join(", ");
  return value === null || value === undefined ? "" : String(value);
}

function ProvenanceChip({ source }: { source: string }) {
  return (
    <Chip tone={sourceTone[sourceKey(source)]} variant="soft" mono>
      {source || "unknown"}
    </Chip>
  );
}

function ConfigRow({
  definition,
  projectId,
  settings,
  activeKey,
  onSave,
}: {
  definition: ConfigRowDefinition;
  projectId: string;
  settings: SettingsMutation;
  activeKey: string | null;
  onSave: (key: string, value: string | number) => Promise<void>;
}) {
  const resolved = useConfigResolve(definition.key, projectId);
  const effective = resolved.data ? formatEditorValue(resolved.data.value) : "";
  const [draft, setDraft] = useState<string | null>(null);
  const editorValue = draft ?? effective;
  const dirty = draft !== null && draft !== effective;
  const tierValue = Number(editorValue);
  const valid = definition.kind !== "tier"
    ? definition.kind !== "runners" || editorValue.trim().length > 0
    : Number.isInteger(tierValue) && tierValue >= 1 && tierValue <= 5;
  const notes = Array.from(
    new Set(
      (resolved.data?.sources ?? [])
        .map((source) => source.note?.trim())
        .filter((note): note is string => Boolean(note)),
    ),
  );

  useEffect(() => {
    if (!dirty && resolved.data) setDraft(null);
  }, [dirty, resolved.data]);

  if (resolved.error) {
    return (
      <div className="border-b border-line-soft px-4 py-3 text-[12px] text-fail last:border-b-0">
        <Mono className="mr-2 text-[11px]">{definition.key}</Mono>
        {resolved.error instanceof Error ? resolved.error.message : String(resolved.error)}
      </div>
    );
  }

  const resolution = resolved.data;
  const readonly = definition.kind === "readonly";
  const saving = activeKey === definition.key && settings.isPending;
  const canSave = dirty && valid && !settings.isPending && Boolean(resolution);

  return (
    <div className="border-b border-line-soft px-4 py-3 last:border-b-0">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[13px] text-ink-soft">{definition.label}</span>
            {resolution ? <ProvenanceChip source={resolution.source} /> : <span className="text-[11px] text-faint">Loading…</span>}
          </div>
          <p className="mt-0.5 max-w-[58ch] text-[11.5px] leading-snug text-faint">{definition.description}</p>
          {notes.map((note) => (
            <p key={note} className="mt-1 font-mono text-[10px] leading-snug text-faint">{note}</p>
          ))}
          {resolution?.path && <Mono className="mt-1 block truncate text-[10px] text-fainter">{resolution.path}</Mono>}
        </div>
        <div className="flex flex-none items-center gap-2">
          {readonly ? (
            <>
              <ReadOnlyTag />
              <Mono className="max-w-[260px] truncate text-[11.5px] text-ink-soft" title={formatValue(resolution?.value)}>
                {resolution ? formatValue(resolution.value) : "…"}
              </Mono>
            </>
          ) : definition.kind === "tier" ? (
            <select
              aria-label={definition.label}
              value={editorValue}
              onChange={(event) => setDraft(event.target.value)}
              className="rounded-md border border-line bg-surface px-2 py-1.5 font-mono text-[11.5px] text-ink focus-visible:border-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/20"
            >
              {[1, 2, 3, 4, 5].map((tier) => <option key={tier} value={tier}>{tier}</option>)}
            </select>
          ) : (
            <input
              aria-label={definition.label}
              value={editorValue}
              onChange={(event) => setDraft(event.target.value)}
              className="w-full min-w-0 rounded-md border border-line bg-surface px-2.5 py-1.5 font-mono text-[11.5px] text-ink sm:w-[230px]"
            />
          )}
          {!readonly && (
            <button
              type="button"
              disabled={!canSave}
              onClick={() => void onSave(definition.key, definition.kind === "tier" ? tierValue : editorValue.trim())}
              className="rounded-md border border-ink bg-ink px-2.5 py-1.5 text-[11.5px] font-semibold text-surface disabled:cursor-not-allowed disabled:opacity-40"
            >
              {saving ? "Saving…" : "Save"}
            </button>
          )}
        </div>
      </div>
      {definition.kind === "runners" && !valid && (
        <p className="mt-1 text-[11px] text-fail">Enter at least one runner.</p>
      )}
      {definition.kind === "tier" && !valid && (
        <p className="mt-1 text-[11px] text-fail">Tier must be a whole number from 1 to 5.</p>
      )}
      {activeKey === definition.key && (
        <ActionResultLine className="mt-2" pending={settings.isPending} error={settings.error} result={settings.data} />
      )}
    </div>
  );
}

export function ConfigSection({ projectId }: { projectId: string }) {
  const settings = useProjectSettings(projectId);
  const [activeKey, setActiveKey] = useState<string | null>(null);
  const save = async (key: string, value: string | number) => {
    setActiveKey(key);
    try {
      await settings.mutateAsync({ key, value });
    } catch {
      // ActionResultLine renders the typed refusal or transport error.
    }
  };

  return (
    <section className="mb-7">
      <SectionLabel className="mb-2.5">Configuration</SectionLabel>
      <SectionCard>
        {configRows.map((definition) => (
          <ConfigRow
            key={definition.key}
            definition={definition}
            projectId={projectId}
            settings={settings}
            activeKey={activeKey}
            onSave={save}
          />
        ))}
      </SectionCard>
    </section>
  );
}

const findingTone: Record<string, Tone> = { ok: "pass", warning: "warn", error: "fail" };

function Finding({ finding }: { finding: SetupFinding }) {
  const status = findingTone[finding.status] ?? "neutral";
  return (
    <div className="border-b border-line-soft px-4 py-3 last:border-b-0">
      <div className="flex flex-wrap items-center gap-2">
        <Chip tone={status} variant="soft" mono>{finding.status}</Chip>
        <Mono className="text-[11px] text-ink-soft">{finding.code}</Mono>
        {finding.repairable && <Chip tone="info" variant="soft" mono>repairable</Chip>}
        {finding.changed && <Chip tone="pass" variant="soft" mono>changed</Chip>}
      </div>
      <p className="mt-1 text-[12px] leading-relaxed text-muted">{finding.message}</p>
      {finding.path && <Mono className="mt-1 block truncate text-[10.5px] text-faint">{finding.path}</Mono>}
      {finding.action && <p className="mt-1 text-[11px] text-faint">{finding.action}</p>}
    </div>
  );
}

function DoctorReport({ report }: { report: SetupDoctorReport }) {
  return (
    <div className="mt-3">
      <div className={`rounded-lg border px-3 py-2 text-[12px] ${report.ok ? "border-pass/25 bg-pass-soft text-pass" : "border-warn/25 bg-warn-soft text-warn"}`}>
        {report.ok ? "Setup is healthy." : "Setup needs attention."}
        <Mono className="ml-2 text-[10px] opacity-80">{report.dry_run ? "dry run" : "repairs applied"}</Mono>
      </div>
      <SectionCard className="mt-3">
        {report.findings.length ? report.findings.map((finding, index) => <Finding key={`${finding.code}-${index}`} finding={finding} />) : (
          <div className="px-4 py-4 text-[12px] text-faint">No findings.</div>
        )}
      </SectionCard>
    </div>
  );
}

export function SetupDoctorPanel({ projectId }: { projectId: string }) {
  const doctor = useSetupDoctor(projectId);
  const [lastApply, setLastApply] = useState(false);
  const report = doctor.data?.report;
  const repairable = report?.findings.some((finding) => finding.repairable) ?? false;
  const run = (apply: boolean) => {
    setLastApply(apply);
    doctor.mutate({ apply });
  };

  return (
    <section className="mb-7">
      <SectionLabel className="mb-2.5">Setup doctor</SectionLabel>
      <div className="rounded-lg border border-line bg-panel p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <div className="text-[13px] font-semibold text-ink">Check project setup</div>
            <p className="mt-1 max-w-[62ch] text-[12px] leading-relaxed text-muted">Run an operator-invoked audit of the registered project. Repairs are deterministic and only run when explicitly applied.</p>
          </div>
          <button
            type="button"
            disabled={doctor.isPending}
            onClick={() => run(false)}
            className="rounded border border-ink bg-ink px-3 py-2 text-[12px] font-semibold text-surface disabled:cursor-not-allowed disabled:opacity-45"
          >
            {doctor.isPending && !lastApply ? "Running…" : "Run doctor"}
          </button>
        </div>
        <ActionResultLine className="mt-3" pending={doctor.isPending} error={doctor.error} result={doctor.data} />
        {report && <DoctorReport report={report} />}
        {report && repairable && (
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <button
              type="button"
              disabled={doctor.isPending}
              onClick={() => run(true)}
              className="rounded border border-warn bg-warn-soft px-3 py-2 text-[12px] font-semibold text-warn disabled:cursor-not-allowed disabled:opacity-45"
            >
              {doctor.isPending && lastApply ? "Applying…" : "Apply deterministic repairs"}
            </button>
          </div>
        )}
      </div>
    </section>
  );
}

function healthTone(health?: string | null): Tone {
  const normalized = health?.toLowerCase() ?? "";
  if (["healthy", "ok", "ready", "connected"].includes(normalized)) return "pass";
  if (["error", "failed", "offline"].includes(normalized)) return "fail";
  return "warn";
}

export function RepositoryFacts({ project }: { project: ProjectSummary }) {
  return (
    <section className="mb-7">
      <SectionLabel className="mb-2.5">Repository</SectionLabel>
      <SectionCard>
        <Fact label="Repository root" value={project.repoRoot} mono />
        <Fact label="Vault root" value={project.vaultRoot} mono />
        <Fact label="Health" value={<Chip tone={healthTone(project.health)} variant="soft" mono>{project.health || "unknown"}</Chip>} />
        {project.lastError && <Fact label="Last error" value={project.lastError} error />}
      </SectionCard>
    </section>
  );
}

function Fact({ label, value, mono, error }: { label: string; value: ReactNode; mono?: boolean; error?: boolean }) {
  return (
    <div className="flex flex-col gap-1 px-4 py-3 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
      <span className="text-[12.5px] text-muted">{label}</span>
      <span className={`${mono ? "font-mono text-[11px]" : "text-[12px]"} max-w-full break-all text-right ${error ? "text-fail" : "text-ink-soft"}`}>{value}</span>
    </div>
  );
}

export function DangerZone({ project }: { project: ProjectSummary }) {
  const confirm = useConfirm();
  const navigate = useNavigate();
  const remove = useRemoveProject(project.id);
  const removeProject = async () => {
    const confirmed = await confirm({
      title: "Remove project from daemon?",
      body: `This forgets ${project.name}'s registration only. The repository and .tusker vault remain untouched.`,
      confirmLabel: "Remove from daemon",
      tone: "danger",
      typeToConfirm: project.id,
    });
    if (!confirmed) return;
    try {
      await remove.mutateAsync();
      void navigate({ to: "/" });
    } catch {
      // ActionResultLine renders the typed refusal or transport error.
    }
  };

  return (
    <section className="mb-7">
      <SectionLabel className="mb-2.5">Danger zone</SectionLabel>
      <div className="rounded-lg border border-fail/30 bg-fail-soft/30 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <div className="text-[13px] font-semibold text-ink">Remove from daemon</div>
            <p className="mt-1 max-w-[60ch] text-[12px] leading-relaxed text-muted">Only the daemon registration is removed. The repository and its <Mono className="text-[11px]">.tusker</Mono> vault are never touched.</p>
          </div>
          <button
            type="button"
            disabled={remove.isPending}
            onClick={() => void removeProject()}
            className="rounded border border-fail bg-fail px-3 py-2 text-[12px] font-semibold text-surface disabled:cursor-not-allowed disabled:opacity-45"
          >
            {remove.isPending ? "Removing…" : "Remove from daemon"}
          </button>
        </div>
        <ActionResultLine className="mt-3" pending={remove.isPending} error={remove.error} result={remove.data} />
      </div>
    </section>
  );
}
