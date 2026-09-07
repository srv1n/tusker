/*
  WUX-T-0003 isolated preview — clearly labeled SAMPLE DATA.
  Renders the real ProjectNavigation against fixtures covering normal,
  empty, loading/error and narrow-screen cases. Not live integration.
*/

import { StrictMode, useState } from "react";
import { createRoot } from "react-dom/client";

import "@fontsource/archivo/400.css";
import "@fontsource/archivo/500.css";
import "@fontsource/archivo/600.css";
import "@fontsource/archivo/700.css";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@fontsource/jetbrains-mono/600.css";

import "@/styles/app.css";
import { ThemeProvider } from "@/lib/theme";
import type { ProjectSummary } from "@/types/domain";
import {
  ProjectNavigation,
  type WaveLink,
  type WaveReadState,
} from "@/features/workbench/navigation";

function project(id: string, name: string, extra?: Partial<ProjectSummary>): ProjectSummary {
  return {
    id,
    name,
    repoRoot: `/sample/repos/${id}`,
    vaultRoot: `/sample/repos/${id}/.tusker`,
    automationEnabled: false,
    health: "healthy",
    needsCount: 0,
    activeRuns: 0,
    worstLiveness: null,
    daemonConnected: false,
    ...extra,
  };
}

const PROJECTS: ProjectSummary[] = [
  project("p-tusker", "tusker", { needsCount: 2, activeRuns: 1, worstLiveness: "fresh" }),
  project("p-long", "a-much-longer-project-name-that-truncates-in-the-sidebar-row", { activeRuns: 2, worstLiveness: "fresh" }),
  project("p-empty", "empty-project-with-no-waves-at-all"),
  project("p-loading", "project-whose-waves-are-still-loading"),
  project("p-failed", "project-with-a-failed-wave-read", { health: "error", needsCount: 1 }),
  ...Array.from({ length: 13 }, (_, i) =>
    project(`p-extra-${i + 1}`, `sample-project-${String(i + 1).padStart(2, "0")}-with-a-fairly-long-name`),
  ),
];

function links(projectId: string, count: number, activeIndex = 0): WaveLink[] {
  return Array.from({ length: count }, (_, i) => ({
    id: `W-${projectId}-${i + 1}`,
    title: i === 0 ? `Build the everyday Tusker work experience` : `Wave ${i + 1} for ${projectId}`,
    href: `/p/${projectId}/waves/W-${i + 1}`,
    active: i === activeIndex,
  }));
}

const WAVE_LINKS: Record<string, WaveLink[]> = {
  "p-tusker": [
    ...links("p-tusker", 6),
    // Intentional duplicate ID: the component must dedupe by stable ID.
    { id: "W-p-tusker-1", title: "Duplicate of wave 1", href: "/p/p-tusker/waves/W-1", active: false },
  ],
  "p-long": links("p-long", 3),
  "p-empty": [],
  "p-failed": [],
};

const WAVE_READS: Record<string, WaveReadState> = {
  "p-tusker": { state: "ready" },
  "p-long": { state: "ready" },
  "p-empty": { state: "ready" },
  "p-loading": { state: "loading" },
  "p-failed": { state: "error", error: "wave index fetch failed (sample)" },
};

function Preview() {
  const [currentPath, setCurrentPath] = useState("/p/p-tusker/waves/W-1");
  const [expanded, setExpanded] = useState<string[]>(["p-tusker", "p-long", "p-loading", "p-failed", "p-empty"]);
  const [log, setLog] = useState<string[]>([]);

  return (
    <div className="flex min-h-screen bg-surface text-ink">
      <aside className="flex w-64 flex-none flex-col border-r border-line bg-panel p-2.5" aria-label="Preview sidebar">
        <p className="mb-2 rounded-md bg-warn-soft px-2.5 py-1.5 text-[11px] font-semibold text-warn">
          Sample data preview — not live integration.
        </p>
        <ProjectNavigation
          projects={PROJECTS}
          waveLinks={WAVE_LINKS}
          waveReads={WAVE_READS}
          currentPath={currentPath}
          onNavigate={(href) => {
            setCurrentPath(href);
            setLog((prev) => [`navigate ${href}`, ...prev].slice(0, 8));
          }}
          onAddProject={() => setLog((prev) => ["add-project", ...prev].slice(0, 8))}
          onExpandedProjectsChange={setExpanded}
        />
      </aside>
      <main className="min-w-0 flex-1 p-6">
        <h1 className="text-lg font-bold">Navigation preview harness</h1>
        <p className="mt-1 text-sm text-muted">
          {PROJECTS.length} sample projects · current path <code className="font-mono text-[12px]">{currentPath}</code> ·
          expanded <code className="font-mono text-[12px]">{expanded.length}</code> projects
        </p>
        <h2 className="mt-6 text-sm font-semibold">Interaction log</h2>
        <ol data-preview-log className="mt-2 space-y-1 font-mono text-[12px] text-muted">
          {log.length === 0 && <li>Interact with the navigator — every action lands here.</li>}
          {log.map((entry, i) => (
            <li key={`${i}-${entry}`}>{entry}</li>
          ))}
        </ol>
        <h2 className="mt-6 text-sm font-semibold">Covered cases</h2>
        <ul className="mt-2 list-disc space-y-1 pl-5 text-[13px] text-muted">
          <li>Eighteen projects with long truncating names and keyboard ↑ ↓ reorder.</li>
          <li>Seven wave shortcuts deduplicated to five plus Show more (tusker).</li>
          <li>Empty (p-empty), loading (p-loading) and failed (p-failed) reads stay distinct.</li>
          <li>Drag a header to reorder; focus restores and position is announced.</li>
        </ul>
      </main>
    </div>
  );
}

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <ThemeProvider>
        <Preview />
      </ThemeProvider>
    </StrictMode>,
  );
  root.setAttribute("data-wux-ready", "true");
}
