/*
  Project Settings / Details (route: /p/$projectId/settings).

  Per-project configuration: repository facts, live worktrees, general config,
  routing rules, workspace lifecycle, and landing / parallelism. Every editable
  row carries a provenance chip and — crucially — an edit writes a machine-local
  override rather than touching committed project config (see useConfigRows in
  ./project/parts). Derived facts render read-only, so a programmatic edit never
  *looks* like it rewrote a shared file.

  Settings values are screen-local (./project/mock); live project + daemon status
  come from the shared hooks.
*/

import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useDaemon, useProjects } from "@/lib/queries";
import { PageScroll, SectionLabel } from "@/components/ui/page";
import { EmptyState, QueryBoundary, Skeleton } from "@/components/ui/states";
import type { ProjectSummary } from "@/types/domain";
import {
  configurationRows,
  landingRows,
  overlapNote,
  portRange,
  projectMeta,
  repositoryRows,
  routingFallthrough,
  routingRules,
  settingsTabs,
  workspaceScripts,
  worktrees,
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
        {projectMeta.path} · {projectMeta.branch}
      </p>

      <SettingsTabs tabs={settingsTabs} active={tab} onChange={setTab} />

      <div key={tab} className="mt-6 animate-rise">
        {tab === "details" && (
          <>
            <SectionLabel className="mb-2.5">Repository</SectionLabel>
            <div className="mb-7">
              <SettingList rows={repo.rows} onChange={repo.setValue} onReset={repo.reset} />
            </div>

            <div className="mb-2.5 flex items-center justify-between">
              <SectionLabel>Worktrees</SectionLabel>
              {daemon && (
                <LiveMeta
                  connected={daemon.connected}
                  label={daemon.connected ? `${worktrees.length} active · ${daemon.addr}` : "daemon offline"}
                />
              )}
            </div>
            <div className="mb-7">
              <WorktreeList worktrees={daemon && !daemon.connected ? [] : worktrees} />
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
