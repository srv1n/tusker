import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { AlertTriangle, CircleOff, LockKeyhole, Sparkles } from "lucide-react";
import { Button, Select, SegmentedControl, TextInput, Toggle } from "@/components/ui/controls";
import { ActionResultLine } from "@/components/ui/action-feedback";
import { Card, Chip, Dot, Mono } from "@/components/ui/primitives";
import { PageHeader, PageScroll, SectionLabel } from "@/components/ui/page";
import { QueryBoundary, SkeletonRows } from "@/components/ui/states";
import { useDaemon, useFactoryOperations, useProjectAutomation, useProjectSettings, useProjects, useRuns, useWaves } from "@/lib/queries";
import { cn } from "@/lib/cn";
import { projectContainsCheckout, type DaemonStatus, type FactoryOperationsProjection, type ProjectSummary, type RunSummary, type WaveSummary } from "@/types/domain";
import { api } from "@/lib/api";
import { TiersSection } from "@/features/settings/app/TiersSection";
import { NAVIGATION_CHANGED_EVENT, readNavigationState, setProjectIcon, writeNavigationState, type ProjectIconName, type StorageLike } from "@/features/workbench/navigation/navigationState";
import { PROJECT_ICON_CHANGED_EVENT, projectIconChoices } from "@/features/workbench/navigation/projectIcons";
import { ProjectRegistrationRepair } from "./ProjectRegistrationRepair";

type SettingsTab = "basic" | "models" | "advanced";
type DiagnosticsTab = "health" | "runtime" | "runners" | "queue" | "workspaces" | "doctor" | "audit";

const unavailable = "Not available through the current serve API";

/**
 * The project settings surface deliberately exposes only the narrow write
 * contract the daemon supports. Everything else is a provenance-labelled fact,
 * rather than a convincing local toggle that would lie about persistence.
 */
export function Settings() {
  const { projectId } = useParams({ strict: false }) as { projectId: string };
  const [tab, setTab] = useState<SettingsTab>("basic");
  const projects = useProjects();
  const operations = useFactoryOperations(projectId);
  const automation = useProjectAutomation(projectId);
  const settings = useProjectSettings(projectId);

  return (
    <PageScroll>
      <div className="mx-auto max-w-[1120px] animate-rise">
        <PageHeader
          eyebrow={<Mono className="text-[11px] uppercase tracking-[0.14em] text-faint">tusker / {projectId}</Mono>}
          title="Settings"
          subtitle="Effective project policy, with the source of each value made explicit."
        />
        <div className="mb-7 border-b border-line pb-4">
          <SegmentedControl
            value={tab}
            onChange={setTab}
            options={[{ value: "basic", label: "Basic" }, { value: "models", label: "Models" }, { value: "advanced", label: "Advanced" }]}
          />
        </div>
        {tab === "models" ? <TiersSection projectId={projectId} scope="project" /> : (
        <QueryBoundary q={projects} loading={<SkeletonRows rows={5} />}>
          {(items) => {
            const group = items.find((item) => projectContainsCheckout(item, projectId));
            if (!group) return <MissingProject />;
            const selected = group.checkouts?.find((checkout) => checkout.id === projectId);
            const project = selected ? {
              ...group, id: selected.id, repoRoot: selected.repoRoot, vaultRoot: selected.vaultRoot, health: selected.health,
              automationEnabled: selected.automationEnabled, automationSource: selected.automationSource ?? group.automationSource,
            } : group;
            return tab === "basic" ? (
              <SettingsBasic project={project} projectIds={items.map((item) => item.id)} operations={operations.data} automation={automation} settings={settings} onOpenAdvanced={() => setTab("advanced")} />
            ) : (
              <SettingsAdvanced project={project} operations={operations.data} />
            );
          }}
        </QueryBoundary>
        )}
      </div>
    </PageScroll>
  );
}

export function SettingsBasic({
  project,
  projectIds,
  operations,
  automation,
  settings,
  onOpenAdvanced,
}: {
  project: ProjectSummary;
  projectIds: string[];
  operations: FactoryOperationsProjection | undefined;
  automation: ReturnType<typeof useProjectAutomation>;
  settings: ReturnType<typeof useProjectSettings>;
  onOpenAdvanced: () => void;
}) {
  const [workspaceMode, setWorkspaceMode] = useState(project.workspaceMode ?? "shared");
  const [concurrency, setConcurrency] = useState(String(project.maxActiveRunsPerProject ?? ""));
  const automationPending = useRef(false);
  const saveExecution = () => {
    const parsed = Number(concurrency);
    settings.mutate({
      workspaceMode,
      ...(Number.isFinite(parsed) && parsed > 0 ? { maxActiveRunsPerProject: parsed } : {}),
    });
  };
  const setAutomation = (enabled: boolean) => {
    if (!beginAutomationToggle(automationPending)) return;
    automation.mutate(enabled, { onSettled: () => { automationPending.current = false; } });
  };

  return (
    <div className="space-y-9">
      {project.health === "error" ? (
        <Card className="flex flex-wrap items-center justify-between gap-3 border-warn/40 bg-warn-soft/40 p-4">
          <div>
            <h2 className="text-[14px] font-semibold text-ink">Project registration needs repair</h2>
            <p className="mt-1 text-[12px] leading-relaxed text-muted">Open Advanced settings to choose the canonical repository and vault. No task state is reset.</p>
          </div>
          <Button type="button" size="sm" variant="primary" onClick={onOpenAdvanced}>Open registration repair</Button>
        </Card>
      ) : null}
      <SettingGroup label="Appearance">
        <SettingRow
          name="Project icon"
          detail="Pick an icon, upload an image, or let Tusker use a repository icon when one is discoverable."
          source="Local"
          control={<ProjectIconPicker key={project.logicalId ?? project.id} projectId={project.logicalId ?? project.id} projectIds={projectIds} />}
        />
      </SettingGroup>
      <SettingGroup label="Automation">
		<SettingRow
		  name="Background work"
		  detail="Run queued work automatically while Tusker is open."
          source={project.automationSource ?? "Project"}
          control={<Toggle checked={project.automationEnabled} disabled={automation.isPending || automationPending.current} onChange={setAutomation} ariaLabel={`Background work (${project.automationEnabled ? "On" : "Off"})`} label={project.automationEnabled ? "On" : "Off"} />}
        />
        <ActionResultLine pending={automation.isPending} error={automation.error} result={automation.data} />
		<SettingRow
		  name="What runs"
		  detail="Only tasks and waves you explicitly run."
          source={operations?.project.dispatchScope.provenance ?? "Loading"}
          control={<ReadValue value={operations ? `${operations.project.dispatchScope.configured ?? "unset"} → ${operations.project.dispatchScope.effective}` : "Loading…"} />}
        />
        <SettingRow
          name="Completion handling"
          detail="Completion policy is a daemon-owned setting. This screen reports the effective policy without inventing a write path."
          source={operations?.project.completionMode.provenance ?? "Loading"}
          control={<ReadValue value={operations ? `${operations.project.completionMode.configured ?? "unset"} → ${operations.project.completionMode.effective}` : "Loading…"} />}
        />
      </SettingGroup>

      <SettingGroup label="Capacity & workspace">
        <SettingRow
          name="Workspace mode"
          detail={workspaceMode === "shared" ? "Work directly in the current checkout, including existing uncommitted changes. Parallel tasks must own different paths." : "Create an isolated Git worktree for each task so background work can run separately."}
          source={project.workspaceSource ?? "Project"}
          control={
            <Select aria-label="Workspace mode" value={workspaceMode} onChange={(event) => setWorkspaceMode(event.target.value)}>
              <option value="shared">Current checkout</option>
              <option value="worktree">Git worktree</option>
            </Select>
          }
        />
        <SettingRow
          name="Concurrent tasks"
          detail="Maximum active runs for this project; overlapping owned paths still wait, and the daemon enforces the global cap."
          source={project.concurrencySource ?? "Project"}
          control={<TextInput aria-label="Project concurrent tasks" inputMode="numeric" value={concurrency} onChange={(event) => setConcurrency(event.target.value)} className="w-28 font-mono" />}
        />
        <div className="flex flex-wrap items-center justify-end gap-3 bg-panel px-4 py-3">
          <ActionResultLine pending={settings.isPending} error={settings.error} result={settings.data} />
          <Button variant="primary" disabled={settings.isPending} onClick={saveExecution}>{settings.isPending ? "Saving…" : "Save execution settings"}</Button>
        </div>
      </SettingGroup>

      <SettingGroup label="Notifications">
        <UnavailableRow name="Notification policy" detail="In-app and operating-system notification preferences have no read/write Serve contract yet." />
      </SettingGroup>
    </div>
  );
}

export function beginAutomationToggle(pending: { current: boolean }) {
  if (pending.current) return false;
  pending.current = true;
  return true;
}

function localNavigationStorage(): StorageLike | null {
  try {
    return typeof window !== "undefined" && window.localStorage ? window.localStorage : null;
  } catch {
    return null;
  }
}

const PROJECT_ICON_UPLOAD_MAX_BYTES = 96 * 1024;

function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).split(",")[1] ?? "");
    reader.onerror = () => reject(reader.error ?? new Error("Could not read the image file"));
    reader.readAsDataURL(blob);
  });
}

/**
 * Center-crops an uploaded image to a square and re-encodes it small enough for
 * the action-body limit. SVGs pass through untouched; other formats get a
 * size ladder that trades resolution for a payload that always fits.
 */
async function rasterizeProjectIcon(file: File): Promise<{ data: string; mime: string }> {
  if (file.type === "image/svg+xml") {
    if (file.size > PROJECT_ICON_UPLOAD_MAX_BYTES) throw new Error("That SVG is too large for an icon upload.");
    return { data: await blobToBase64(file), mime: "image/svg+xml" };
  }
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    throw new Error("That image format can't be processed in this browser.");
  }
  const crop = Math.min(bitmap.width, bitmap.height);
  const sx = (bitmap.width - crop) / 2;
  const sy = (bitmap.height - crop) / 2;
  for (const [size, type] of [[256, "image/png"], [128, "image/png"], [256, "image/webp"], [128, "image/webp"]] as const) {
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = size;
    const context = canvas.getContext("2d");
    if (!context) continue;
    context.drawImage(bitmap, sx, sy, crop, crop, 0, 0, size, size);
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, 0.92));
    if (blob && blob.size <= PROJECT_ICON_UPLOAD_MAX_BYTES) {
      const encoded = { data: await blobToBase64(blob), mime: blob.type || type };
      bitmap.close();
      return encoded;
    }
  }
  bitmap.close();
  throw new Error("Couldn't compress that image under the icon size limit.");
}

function ProjectIconPicker({ projectId, projectIds }: { projectId: string; projectIds: string[] }) {
  const [selected, setSelected] = useState<ProjectIconName>(() => readNavigationState(localNavigationStorage(), projectIds).projectIconById[projectId] ?? "auto");
  const [iconSource, setIconSource] = useState<"uploaded" | "discovered" | null>(null);
  const [iconVersion, setIconVersion] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let live = true;
    api.projectIconSource(projectId).then((source) => { if (live) setIconSource(source); }).catch(() => {});
    return () => { live = false; };
  }, [projectId, iconVersion]);

  const choose = (icon: ProjectIconName) => {
    const storage = localNavigationStorage();
    const state = readNavigationState(storage, projectIds);
    writeNavigationState(storage, setProjectIcon(state, projectIds, projectId, icon));
    setSelected(icon);
    if (typeof window !== "undefined") window.dispatchEvent(new Event(NAVIGATION_CHANGED_EVENT));
  };
  const upload = async (file: File) => {
    setBusy(true);
    setError("");
    try {
      const icon = await rasterizeProjectIcon(file);
      await api.setProjectIconImage(projectId, icon);
      choose("auto");
      setIconVersion((version) => version + 1);
      window.dispatchEvent(new Event(PROJECT_ICON_CHANGED_EVENT));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Icon upload failed.");
    } finally {
      setBusy(false);
      if (fileInput.current) fileInput.current.value = "";
    }
  };
  const pick = () => {
    const pickImage = window.tuskerShell?.pickImage;
    if (pickImage) {
      void pickImage()
        .then((picked) => {
          if (!picked) return;
          if ("error" in picked) { setError(picked.error); return; }
          const bytes = Uint8Array.from(atob(picked.data), (char) => char.charCodeAt(0));
          void upload(new File([bytes], picked.name, { type: picked.mime }));
        })
        .catch(() => setError("The image picker did not respond."));
      return;
    }
    fileInput.current?.click();
  };
  const clearUploaded = async () => {
    setBusy(true);
    setError("");
    try {
      await api.clearProjectIconImage(projectId);
      setIconVersion((version) => version + 1);
      window.dispatchEvent(new Event(PROJECT_ICON_CHANGED_EVENT));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Icon removal failed.");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-col gap-2">
      <div className="flex max-w-[280px] flex-wrap items-center gap-1" role="group" aria-label="Project icon">
        <button type="button" aria-label="Automatic project icon" title="Automatic" aria-pressed={selected === "auto"} onClick={() => choose("auto")} className={cn("flex h-8 w-8 items-center justify-center rounded-md border transition-colors", selected === "auto" ? "border-ink bg-ink text-surface" : "border-line text-muted hover:bg-hover hover:text-ink")}><Sparkles size={15} aria-hidden="true" /></button>
        {projectIconChoices.map(({ name, label, Icon, className }) => <button key={name} type="button" aria-label={`${label} project icon`} title={label} aria-pressed={selected === name} onClick={() => choose(name)} className={cn("flex h-8 w-8 items-center justify-center rounded-md border transition-colors", selected === name ? "border-ink bg-ink" : "border-line hover:bg-hover")}><Icon size={15} aria-hidden="true" className={selected === name ? "text-surface" : className} /></button>)}
      </div>
      <div className="flex items-center gap-2">
        <input ref={fileInput} type="file" accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml,image/x-icon" className="sr-only" onChange={(event) => { const file = event.target.files?.[0]; if (file) void upload(file); }} />
        {iconSource === "uploaded" ? (
          <>
            <img src={`/api/projects/${encodeURIComponent(projectId)}/icon?v=${iconVersion}`} alt="Uploaded project icon" className="h-8 w-8 rounded-md border border-line object-cover" />
            <Button type="button" size="sm" disabled={busy} onClick={pick}>Replace</Button>
            <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => void clearUploaded()}>Remove</Button>
          </>
        ) : (
          <Button type="button" size="sm" disabled={busy} onClick={pick}>{busy ? "Processing…" : "Upload image"}</Button>
        )}
        {iconSource === "discovered" ? <span className="text-[11px] text-faint">Using an icon found in the repository</span> : null}
      </div>
      {error ? <p role="alert" className="text-[11px] text-fail">{error}</p> : null}
    </div>
  );
}

function SettingsAdvanced({ project, operations }: { project: ProjectSummary; operations: FactoryOperationsProjection | undefined }) {
  const needsRegistrationRepair = project.health === "error";
  return (
    <div className="space-y-8">
      <aside className="rounded-xl border border-dashed border-line bg-panel px-4 py-3 text-[13px] leading-relaxed text-muted">
        Advanced values are read-only except for the bounded registration repair below. It changes only the Serve project pointer; it does not reset or retire task state.
      </aside>
      <SettingGroup label="Project and repository">
        <ReadOnlyRow name="Project ID" value={project.id} source="Registry" />
        <ReadOnlyRow name="Repository" value={project.repoRoot || "Not served"} source="Registry" />
        <ReadOnlyRow name="Vault" value={project.vaultRoot || "Not served"} source="Registry" />
        <ReadOnlyRow name="Project health" value={project.health} source="Runtime" />
      </SettingGroup>
      {(project.checkouts?.length ?? 0) > 1 ? (
        <SettingGroup label="Registered checkouts">
          {project.checkouts?.map((checkout) => (
            <ReadOnlyRow
              key={checkout.id}
              name={checkout.branch ? checkout.detached ? `Detached · ${checkout.head ?? "unknown"}` : checkout.branch : checkout.label}
              value={`${checkout.label} · ${checkout.available ? checkout.activity : "Unavailable"}`}
              source={checkout.repoRoot}
            />
          ))}
          <div className="space-y-1 px-4 py-3">
            <p className="text-[11px] leading-4 text-faint">Grouping is navigation-only. Cleanup preview: <span className="font-mono">tusker projects prune</span>.</p>
            {project.registryPreview?.missingRegistrations.length ? <p className="text-[11px] leading-4 text-warn">Missing registrations: {project.registryPreview.missingRegistrations.join(", ")}</p> : null}
          </div>
        </SettingGroup>
      ) : null}
      <ProjectRegistrationRepair project={project} needsAttention={needsRegistrationRepair} />
      <SettingGroup label="Authority and promotion">
        <ReadOnlyRow name="Dispatch scope" value={operations ? operations.project.dispatchScope.effective : "Loading…"} source={operations?.project.dispatchScope.provenance ?? "Runtime"} />
        <ReadOnlyRow name="Completion mode" value={operations ? operations.project.completionMode.effective : "Loading…"} source={operations?.project.completionMode.provenance ?? "Runtime"} />
        <ReadOnlyRow name="Promotion mode" value={operations ? operations.project.promotionMode.mode : "Loading…"} source={operations?.project.promotionMode.provenance ?? "Runtime"} />
        <UnavailableRow name="Routing, profiles, schedules, and notification policies" detail="These policy families are not yet readable and writable as independent Serve settings." />
      </SettingGroup>
      <SettingGroup label="Provenance">
        <ReadOnlyRow name="Automation" value={project.automationEnabled ? "Enabled" : "Disabled"} source={project.automationSource ?? "Project"} />
        <ReadOnlyRow name="Workspace" value={project.workspaceMode ?? "Default"} source={project.workspaceSource ?? "Project"} />
        <ReadOnlyRow name="Concurrency" value={String(project.maxActiveRunsPerProject ?? "Default")} source={project.concurrencySource ?? "Project"} />
      </SettingGroup>
    </div>
  );
}

/** Diagnostics is a passive inspection surface. It may report a failure, but never offers a fake repair. */
export function Diagnostics() {
  const { projectId } = useParams({ strict: false }) as { projectId: string };
  const [tab, setTab] = useState<DiagnosticsTab>("health");
  const daemon = useDaemon();
  const operations = useFactoryOperations(projectId);
  const runs = useRuns(projectId);
  const waves = useWaves(projectId);

  return (
    <PageScroll>
      <div className="mx-auto max-w-[1180px] animate-rise">
        <PageHeader
          eyebrow={<Mono className="text-[11px] uppercase tracking-[0.14em] text-faint">tusker / {projectId}</Mono>}
          title="Diagnostics"
          actions={<Link to="/p/$projectId/diagnostics/executions" params={{ projectId }} className="rounded-lg border border-line px-3 py-2 text-[12px] font-medium text-ink-soft hover:bg-hover">Execution observability</Link>}
          subtitle="Runtime facts and bounded operational evidence. Repair actions appear only when the service exposes a canonical mutation."
        />
        <div className="mb-7 flex flex-wrap gap-1 border-b border-line pb-4">
          <SegmentedControl
            size="sm"
            value={tab}
            onChange={setTab}
            options={[
              { value: "health", label: "Health" }, { value: "runtime", label: "Runtime" }, { value: "runners", label: "Runners" },
              { value: "queue", label: "Queue" }, { value: "workspaces", label: "Workspaces" }, { value: "doctor", label: "Doctor" }, { value: "audit", label: "Audit" },
            ]}
          />
        </div>
        <DiagnosticsBody tab={tab} daemon={daemon.data} operations={operations.data} runs={runs.data} waves={waves.data} />
      </div>
    </PageScroll>
  );
}

function DiagnosticsBody({
  tab,
  daemon,
  operations,
  runs,
  waves,
}: {
  tab: DiagnosticsTab;
  daemon: DaemonStatus | undefined;
  operations: FactoryOperationsProjection | undefined;
  runs: RunSummary[] | undefined;
  waves: WaveSummary[] | undefined;
}) {
  if (!daemon || !operations || !runs || !waves) return <SkeletonRows rows={6} />;
  switch (tab) {
    case "health": return <HealthTab daemon={daemon} operations={operations} />;
    case "runtime": return <RuntimeTab daemon={daemon} />;
    case "runners": return <RunnersTab runs={runs} />;
    case "queue": return <QueueTab operations={operations} />;
    case "workspaces": return <WorkspacesTab runs={runs} />;
    case "doctor": return <DoctorTab />;
    case "audit": return <AuditTab operations={operations} waves={waves} />;
  }
}

function HealthTab({ daemon, operations }: { daemon: DaemonStatus; operations: FactoryOperationsProjection }) {
  const healthy = daemon.daemonAlive !== false && !daemon.crashLoop?.open && !daemon.invariantCircuit?.open;
  return <div className="space-y-6">
    <Card className={cn("border-l-2 p-5", healthy ? "border-l-pass" : "border-l-fail")}>
      <div className="flex items-start gap-3"><Dot tone={healthy ? "pass" : "fail"} pulse={healthy} size={8} /><div><h2 className="font-serif text-[23px] font-semibold text-ink">{healthy ? "Runtime is reporting healthy" : "Runtime needs attention"}</h2><p className="mt-1 text-[13px] leading-relaxed text-muted">{healthy ? "The daemon reports a live control plane. Runner profile health is not part of this endpoint." : daemon.daemonDownReason ?? daemon.crashLoop?.summary ?? daemon.invariantCircuit?.summary ?? "A runtime circuit is open."}</p></div></div>
    </Card>
    <FactGrid facts={[
      ["Daemon", daemon.daemonAlive === false ? "not running" : "running"], ["Active runs", `${daemon.activeRuns}/${daemon.maxActiveRuns ?? "—"}`],
      ["Queued tasks", String(daemon.queuedTasks)], ["Factory health", operations.project.health],
      ["Project capacity", `${operations.capacity.project.active}/${operations.capacity.project.limit}`], ["Global capacity", `${operations.capacity.global.active}/${operations.capacity.global.limit}`],
    ]} />
    {(daemon.crashLoop?.open || daemon.invariantCircuit?.open) ? <IssueCard title="Runtime circuit open" detail={daemon.crashLoop?.reason ?? daemon.invariantCircuit?.reason ?? "See daemon diagnostics for the recorded violation."} /> : null}
  </div>;
}

function RuntimeTab({ daemon }: { daemon: DaemonStatus }) {
  return <div className="space-y-6"><FactGrid facts={[
    ["Serve address", daemon.addr], ["PID", String(daemon.daemonPid ?? "—")], ["Started", daemon.daemonStartedAt ?? "—"], ["Last poll", daemon.daemonLastPollAt ?? "—"],
    ["Connection", daemon.connected ? "connected" : "offline"], ["Disk pressure", daemon.diskPressure?.state ?? "not reported"],
  ]} /><UnavailablePanel title="Runtime controls" detail="Start, stop, and resume exist as low-level daemon mutations, but the runtime panel does not expose a restart or scheduling-pause control because those exact operations are not provided by the Serve contract." /></div>;
}

function RunnersTab({ runs }: { runs: RunSummary[] }) {
  const roster = useMemo(() => {
    const grouped = new Map<string, RunSummary[]>();
    runs.forEach((run) => grouped.set(run.runner, [...(grouped.get(run.runner) ?? []), run]));
    return [...grouped.entries()];
  }, [runs]);
  return <div className="space-y-5"><section><SectionLabel className="mb-2">Observed runners from runtime runs</SectionLabel><div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{roster.length ? roster.map(([runner, runnerRuns]) => <Card key={runner} className="p-4"><div className="flex items-center justify-between"><Mono className="text-[12px] text-ink">{runner}</Mono><Chip tone="neutral" mono>{runnerRuns.length} runs</Chip></div><p className="mt-3 text-[12.5px] text-muted">{runnerRuns.filter((run) => run.processRunning).length} process(es) running · {runnerRuns.filter((run) => run.liveness === "dead").length} dead/stale records</p><Mono className="mt-3 block text-[10px] text-faint">{[...new Set(runnerRuns.map((run) => run.model).filter(Boolean))].join(" · ") || "model not reported"}</Mono></Card>) : <EmptyReadout text="No runtime rows for this project." />}</div></section><UnavailablePanel title="Runner health and repair" detail="Serve does not expose profile probes, executable discovery, alternative copies, doctor findings, or a safe repair mutation. A missing runner must remain a reported runtime fact, not a simulated repair workflow." /></div>;
}

function QueueTab({ operations }: { operations: FactoryOperationsProjection }) {
  return <div className="space-y-6"><QueueSection title="Working now" items={operations.workingNow} /><QueueSection title="Review or rework" items={operations.reviewOrRework} /><QueueSection title="Blocked" items={operations.blocked} /><QueueSection title="Next frontier" items={operations.nextFrontier} /></div>;
}

function WorkspacesTab({ runs }: { runs: RunSummary[] }) {
  const active = runs.filter((run) => !run.terminal && (run.processRunning || ["claimed", "starting", "running"].includes(run.leaseStateRaw ?? "")));
  return <div className="space-y-4"><SectionLabel>Live and recently reported workspaces</SectionLabel>{active.length === 0 ? <EmptyReadout text="No active workspace leases." /> : <div className="overflow-x-auto rounded-xl border border-line"><table className="w-full min-w-[680px] text-left text-[12px]"><thead className="border-b border-line bg-panel font-mono text-[10px] uppercase tracking-[0.1em] text-faint"><tr><th className="px-3 py-2">Task</th><th className="px-3 py-2">Workspace</th><th className="px-3 py-2">Mode</th><th className="px-3 py-2">Lease</th><th className="px-3 py-2">Liveness</th></tr></thead><tbody>{active.map((run) => <tr key={run.taskId} className="border-b border-line-soft last:border-0"><td className="px-3 py-3"><Mono className="text-[11px] text-ink-soft">{run.taskId}</Mono><div className="mt-1 text-muted">{run.taskTitle}</div></td><td className="max-w-[300px] truncate px-3 py-3 font-mono text-[10.5px] text-faint">{run.workspacePath ?? "not reported"}</td><td className="px-3 py-3 text-muted">{run.workspaceMode ?? "not reported"}</td><td className="px-3 py-3"><Mono className="text-[10.5px] text-muted">{run.leaseState}</Mono></td><td className="px-3 py-3"><Chip tone={run.liveness === "fresh" ? "pass" : run.liveness === "dead" ? "fail" : "warn"} mono>{run.liveness}</Chip></td></tr>)}</tbody></table></div>}</div>;
}

function DoctorTab() { return <UnavailablePanel title="Doctor and safe repairs" detail="The service currently provides no doctor report, executable discovery, cleanup preview, or repair endpoint. This tab intentionally has no ‘Run’, ‘Apply’, or ‘Use discovered copy’ button." />; }

function AuditTab({ operations, waves }: { operations: FactoryOperationsProjection; waves: WaveSummary[] }) {
  return <div className="space-y-6"><FactGrid facts={[["Generated", operations.generatedAt], ["Armed waves", String(operations.authority.waves.length)], ["Visible waves", String(waves.length)], ["Resource holds", String(operations.capacity.resourceHolds.length)], ["Needs decision", String(operations.needsYourDecision.length)], ["Delivered records", String(operations.delivered.length)]]} /><section><SectionLabel className="mb-2">Wave authorization</SectionLabel><div className="space-y-2">{waves.length ? waves.map((wave) => <Card key={wave.id} className="flex flex-wrap items-center justify-between gap-3 p-3"><div><Mono className="text-[10.5px] text-faint">{wave.id}</Mono><span className="ml-2 text-[13px] font-medium text-ink-soft">{wave.title}</span></div><div className="flex items-center gap-2"><Chip tone={wave.authorization.state === "armed" ? "pass" : "warn"} mono>{wave.authorization.state}</Chip><Mono className="text-[10px] text-faint">{wave.authorization.action}</Mono></div></Card>) : <EmptyReadout text="No wave records." />}</div></section></div>;
}

function SettingGroup({ label, children }: { label: string; children: ReactNode }) { return <section><SectionLabel className="mb-[10px] text-ink">{label}</SectionLabel><Card className="divide-y divide-line-soft overflow-hidden">{children}</Card></section>; }
function SettingRow({ name, detail, source, control }: { name: string; detail: string; source: string; control: ReactNode }) { return <div className="grid gap-3 px-4 py-4 md:grid-cols-[minmax(250px,1fr)_minmax(200px,280px)_110px] md:items-center"><div><h3 className="text-[14px] font-medium text-ink">{name}</h3><p className="mt-1 text-[12px] leading-relaxed text-muted">{detail}</p></div><div>{control}</div><Mono className="text-[9.5px] uppercase tracking-[0.08em] text-faint md:text-right">{source}</Mono></div>; }
function UnavailableRow({ name, detail }: { name: string; detail: string }) { return <SettingRow name={name} detail={detail} source="Unavailable" control={<span className="inline-flex items-center gap-1.5 text-[12px] text-faint"><LockKeyhole size={12} /> {unavailable}</span>} />; }
function ReadOnlyRow({ name, value, source }: { name: string; value: string; source: string }) { return <SettingRow name={name} detail="Served as an effective value; this surface does not expose an edit control." source={source} control={<ReadValue value={value} />} />; }
function ReadValue({ value }: { value: string }) { return <Mono className="block break-words text-[11px] text-ink-soft">{value}</Mono>; }
function MissingProject() { return <Card className="p-8 text-center"><AlertTriangle className="mx-auto text-warn" size={22} /><h2 className="mt-3 font-serif text-[21px] font-semibold text-ink">Project not found</h2><p className="mt-2 text-[13px] text-muted">This project is not in the current Serve registry.</p></Card>; }
function FactGrid({ facts }: { facts: Array<[string, string]> }) { return <div className="grid gap-px overflow-hidden rounded-xl border border-line bg-line sm:grid-cols-2 xl:grid-cols-3">{facts.map(([label, value]) => <div key={label} className="bg-raised px-4 py-3"><SectionLabel>{label}</SectionLabel><Mono className="mt-1 block break-words text-[12px] text-ink-soft">{value}</Mono></div>)}</div>; }
function IssueCard({ title, detail }: { title: string; detail: string }) { return <Card className="border-fail/35 bg-fail-soft p-4"><div className="flex gap-3"><AlertTriangle size={17} className="mt-0.5 flex-none text-fail" /><div><h3 className="text-[14px] font-semibold text-fail">{title}</h3><p className="mt-1 text-[12.5px] leading-relaxed text-ink-soft">{detail}</p></div></div></Card>; }
function UnavailablePanel({ title, detail }: { title: string; detail: string }) { return <Card className="border-dashed bg-panel p-5"><div className="flex gap-3"><CircleOff size={17} className="mt-0.5 flex-none text-faint" /><div><SectionLabel>{unavailable}</SectionLabel><h2 className="mt-1 text-[15px] font-semibold text-ink-soft">{title}</h2><p className="mt-1 text-[13px] leading-relaxed text-muted">{detail}</p></div></div></Card>; }
function EmptyReadout({ text }: { text: string }) { return <Card className="border-dashed p-4 text-[13px] text-muted">{text}</Card>; }
function QueueSection({ title, items }: { title: string; items: FactoryOperationsProjection["workingNow"] }) { return <section><div className="mb-2 flex items-center justify-between"><SectionLabel>{title}</SectionLabel><Mono className="text-[10px] text-faint">{items.length}</Mono></div>{items.length ? <div className="space-y-2">{items.map((item) => <Card key={`${item.kind}-${item.id}`} className="p-3"><div className="flex flex-wrap items-center gap-2"><Mono className="text-[10.5px] text-faint">{item.id}</Mono><Chip tone="neutral" mono>{item.state}</Chip></div><h3 className="mt-1.5 text-[13px] font-medium text-ink-soft">{item.title}</h3><p className="mt-1 text-[12px] text-muted">{item.automaticNextAction}</p>{item.cause ? <p className="mt-1 text-[11px] text-warn">{item.cause}</p> : null}</Card>)}</div> : <EmptyReadout text={`No ${title.toLowerCase()} records.`} />}</section>; }
