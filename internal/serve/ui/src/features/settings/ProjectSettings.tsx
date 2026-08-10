/*
  Project Settings / Details (route: /p/$projectId/settings).

  Per-project configuration: repository facts, live execution workspaces, general config,
  routing rules, workspace lifecycle, and landing / parallelism. Every editable
  row carries a provenance chip and — crucially — an edit writes a machine-local
  override rather than touching committed project config (see useConfigRows in
  ./project/parts). Derived facts render read-only, so a programmatic edit never
  *looks* like it rewrote a shared file.

  Presentational fixture rows remain screen-local; execution policy and runtime
  state come exclusively from shared API hooks.
*/

import { useEffect, useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useDaemon, useProjectAutomation, useProjectSettings, useProjects, useRuns } from "@/lib/queries";
import { PageScroll, SectionLabel } from "@/components/ui/page";
import { EmptyState, QueryBoundary, Skeleton } from "@/components/ui/states";
import { ActionResultLine } from "@/components/ui/action-feedback";
import type { ProjectSummary } from "@/types/domain";
import {
  configurationRows,
  landingRows,
  overlapNote,
  portRange,
  repositoryRows,
  routingFallthrough,
  routingRules,
  settingsTabs,
  workspaceScripts,
  type SettingsTab,
} from "./project/mock";
import {
  HintNote,
  IntroText,
  LiveMeta,
  ReadOnlyNote,
  RoutingList,
  ScriptField,
  SettingList,
  SettingsTabs,
  useConfigRows,
  WorktreeList,
} from "./project/parts";

const route = getRouteApi("/p/$projectId/settings");

export function ProjectSettings() {
  const { projectId } = route.useParams();
  const projectsQ = useProjects();

  return (
    <PageScroll>
      <div className="mx-auto max-w-[860px]">
        <QueryBoundary q={projectsQ} loading={<SettingsSkeleton />}>
          {(projects) => {
            const project = projects.find((p) => p.id === projectId);
            if (!project) {
              return (
                <EmptyState
                  title="Project not found"
                  hint="This project isn’t registered with the daemon."
                  action={
                    <Link
                      to="/"
                      className="rounded-lg border border-line px-3 py-1.5 text-[12.5px] font-medium text-ink-soft transition-colors hover:bg-hover"
                    >
                      Back to inbox
                    </Link>
                  }
                />
              );
            }
            return <SettingsBody project={project} projectId={projectId} />;
          }}
        </QueryBoundary>
      </div>
    </PageScroll>
  );
}

function SettingsBody({ project, projectId }: { project: ProjectSummary; projectId: string }) {
  const [tab, setTab] = useState<SettingsTab>("details");
  const daemonQ = useDaemon();
  const daemon = daemonQ.data;
  const automation = useProjectAutomation(projectId);
  const settings = useProjectSettings(projectId);
  const [workspaceMode, setWorkspaceMode] = useState(project.workspaceMode ?? "shared");
  const [concurrency, setConcurrency] = useState(String(project.maxActiveRunsPerProject ?? 1));
  useEffect(() => {
    setWorkspaceMode(project.workspaceMode ?? "shared");
    setConcurrency(String(project.maxActiveRunsPerProject ?? 1));
  }, [project.workspaceMode, project.maxActiveRunsPerProject]);
  const concurrencyNumber = Number(concurrency);
  const concurrencyValid = Number.isInteger(concurrencyNumber) && concurrencyNumber >= 1 && concurrencyNumber <= 256;
  const dirty = workspaceMode !== (project.workspaceMode ?? "shared") || concurrency !== String(project.maxActiveRunsPerProject ?? 1);
  const saveExecution = async () => {
    if (!concurrencyValid || !["shared", "worktree", "clone", "copy"].includes(workspaceMode)) return;
    try {
      await settings.mutateAsync({ workspaceMode, maxActiveRunsPerProject: concurrencyNumber });
    } catch {
      // TanStack exposes the typed transport error through settings.error; keep
      // the draft intact so the operator can correct/retry it.
    }
  };
  const runsQ = useRuns(projectId);
  const workspaces = (runsQ.data ?? []).filter((r) => !r.terminal && ["claimed", "starting", "running"].includes(r.leaseStateRaw ?? "")).map((r) => ({
    task: r.taskId, path: r.workspacePath || project.repoRoot, lease: r.liveness, mode: r.workspaceMode || "shared",
  }));

  // Three independent override write-paths (edits → local, reset → inherited).
  const repo = useConfigRows(repositoryRows);
  const config = useConfigRows(configurationRows);
  const landing = useConfigRows(landingRows);

  return (
    <div className="animate-rise">
      {/* Header */}
      <Link
        to="/p/$projectId"
        params={{ projectId }}
        className="mb-4 inline-flex items-center font-mono text-[11.5px] text-faint transition-colors hover:text-ink"
      >
        ← {project.name}
      </Link>
      <h1 className="font-serif text-[30px] font-semibold leading-tight tracking-[-0.02em] text-ink">
        Details &amp; settings
      </h1>
      <p className="mb-6 mt-1 font-mono text-[11.5px] text-faint">
        {project.repoRoot}
      </p>

      <SettingsTabs tabs={settingsTabs} active={tab} onChange={setTab} />

      <div key={tab} className="mt-6 animate-rise">
        {tab === "details" && (
          <>
            <SectionLabel className="mb-2.5">Daemon automation</SectionLabel>
            <div className="mb-7 flex items-start justify-between gap-5 rounded-lg border border-line bg-panel px-4 py-3.5" data-project-automation>
              <div>
                <div className="text-[13px] font-semibold text-ink">Auto-spawn eligible tasks</div>
                <p className="mt-1 max-w-[590px] text-[12px] leading-relaxed text-muted">
                  When enabled, the daemon polls this project and dispatches eligible ready or rework tasks. Registration alone never enables it.
                </p>
                {(automation.error || automation.data?.ok === false) && (
                  <p className="mt-2 text-[11.5px] text-fail">
                    {automation.error instanceof Error ? automation.error.message : automation.data?.reason}
                  </p>
                )}
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={project.automationEnabled}
                disabled={automation.isPending}
                onClick={() => automation.mutate(!project.automationEnabled)}
                className={`relative mt-0.5 h-6 w-11 flex-none rounded-full transition-colors disabled:opacity-50 ${
                  project.automationEnabled ? "bg-pass" : "bg-line-strong"
                }`}
              >
                <span
                  className={`absolute top-1 h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${
                    project.automationEnabled ? "translate-x-5" : "translate-x-1"
                  }`}
                />
                <span className="sr-only">{project.automationEnabled ? "Disable" : "Enable"} daemon automation</span>
              </button>
            </div>

            <SectionLabel className="mb-2.5">Execution policy</SectionLabel>
            <div className="mb-7 grid grid-cols-2 gap-3 rounded-lg border border-line bg-panel p-4">
              <label className="text-[12px] text-muted">Workspace mode
                <select aria-label="Workspace mode" value={workspaceMode} onChange={(e) => setWorkspaceMode(e.target.value)} className="mt-1 block w-full rounded border border-line bg-canvas p-2 text-ink">
                  <option value="shared">shared repository</option><option value="worktree">worktree</option><option value="clone">clone</option><option value="copy">copy</option>
                </select><span className="font-mono text-[10px]">{project.workspaceSource}</span>
              </label>
              <label className="text-[12px] text-muted">Project concurrency
                <input aria-label="Project concurrency" type="number" min={1} max={256} step={1} value={concurrency} onChange={(e) => setConcurrency(e.target.value)} className="mt-1 block w-full rounded border border-line bg-canvas p-2 text-ink" />
                <span className="font-mono text-[10px]">{project.concurrencySource}</span>
              </label>
            </div>
            <div className="mb-7 flex flex-wrap items-center gap-3">
              <button type="button" disabled={!dirty || !concurrencyValid || settings.isPending} onClick={() => void saveExecution()} className="rounded border border-ink bg-ink px-3 py-2 text-[12px] font-semibold text-surface disabled:cursor-not-allowed disabled:opacity-45">
                {settings.isPending ? "Saving…" : "Save execution settings"}
              </button>
              {dirty && !concurrencyValid && <span className="text-[11px] text-fail">Concurrency must be a whole number from 1 to 256.</span>}
              <ActionResultLine pending={settings.isPending} error={settings.error} result={settings.data} />
            </div>

            <SectionLabel className="mb-2.5">Repository</SectionLabel>
            <div className="mb-7">
              <SettingList rows={repo.rows} onChange={repo.setValue} onReset={repo.reset} />
            </div>

            <div className="mb-2.5 flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
              <SectionLabel>Execution workspaces</SectionLabel>
              {daemon && (
                <LiveMeta
                  connected={daemon.connected}
                  label={daemon.connected ? `${workspaces.length} active · ${daemon.addr}` : "daemon offline"}
                />
              )}
            </div>
            <div className="mb-7">
              <WorktreeList worktrees={daemon && !daemon.connected ? [] : workspaces} />
            </div>

            <SectionLabel className="mb-2.5">Configuration</SectionLabel>
            <SettingList rows={config.rows} onChange={config.setValue} onReset={config.reset} />
          </>
        )}

        {tab === "routing" && (
          <>
            <IntroText>
              For each kind of task, use this runner profile. Rules are evaluated top-down;{" "}
              <b className="font-semibold text-ink">first match wins</b>. Every run names which rule
              picked its profile.
            </IntroText>
            <RoutingList initial={routingRules} fallthrough={routingFallthrough} />
          </>
        )}

        {tab === "workspace" && (
          <>
            <IntroText>Each parallel worktree is created and torn down with these hooks.</IntroText>
            {workspaceScripts.map((s) => (
              <ScriptField key={s.key} script={s} />
            ))}
            <ReadOnlyNote>Ports — {portRange}, so dev servers never collide.</ReadOnlyNote>
          </>
        )}

        {tab === "landing" && (
          <>
            <IntroText>How finished work merges back, and how much runs in parallel.</IntroText>
            <SettingList rows={landing.rows} onChange={landing.setValue} onReset={landing.reset} />
            <HintNote>{overlapNote}</HintNote>
          </>
        )}
      </div>
    </div>
  );
}

function SettingsSkeleton() {
  return (
    <div className="animate-pulse-soft">
      <Skeleton className="mb-4 h-3 w-24" />
      <Skeleton className="h-8 w-64" />
      <Skeleton className="mb-6 mt-2 h-3 w-48" />
      <Skeleton className="mb-6 h-9 w-72" />
      <Skeleton className="mb-2.5 h-3 w-20" />
      <Skeleton className="mb-7 h-40 w-full" />
      <Skeleton className="mb-2.5 h-3 w-24" />
      <Skeleton className="h-32 w-full" />
    </div>
  );
}
