/*
  WUX-T-0006 — isolated TaskInspector preview (sample data only).

  Binary: bun run dev -- --host 127.0.0.1 --port 5184 --strictPort
  URL:    http://127.0.0.1:5184/previews/wux/inspector/index.html
*/

import { StrictMode, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@fontsource/archivo/400.css";
import "@fontsource/archivo/500.css";
import "@fontsource/archivo/600.css";
import "@fontsource/archivo/700.css";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@/styles/app.css";
import { ThemeProvider } from "@/lib/theme";
import { ConfirmProvider } from "@/components/ui/action-feedback";
import { TaskInspector, type InspectorExecutionIdentity } from "@/features/workbench/inspector/TaskInspector";
import { acceptedTask, acceptedRun, failedRun, failedTask, fixtureRuns, fixtureTasks, longContractTask, readyRun, readyTask, unavailableTask } from "./fixtures";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
});

type Scenario = "normal" | "loading" | "error" | "late-response" | "unobserved";

const SCENARIOS: Array<{ id: Scenario; label: string; hint: string }> = [
  { id: "normal", label: "Normal selection", hint: "Intent, stage, decision and evidence render before metadata." },
  { id: "loading", label: "Loading", hint: "Skeleton state while the task payload is in flight." },
  { id: "error", label: "Failed load", hint: "Unavailable task details stay distinct from loading." },
  { id: "late-response", label: "Late response A→B", hint: "Task A payload with B selected renders loading, never A's data." },
  { id: "unobserved", label: "Unobserved identity", hint: "Missing provider/transport reads as Unavailable, never a guess." },
];

const OBSERVED_IDENTITY: InspectorExecutionIdentity = {
  provider: "preview-provider",
  model: "preview-model",
  transport: "ACP",
  stage: "execute",
  observed: true,
};

function Preview() {
  const [taskId, setTaskId] = useState(readyTask.id);
  const [scenario, setScenario] = useState<Scenario>("normal");
  const [open, setOpen] = useState(true);
  const [openedTask, setOpenedTask] = useState<string | null>(null);
  const originRef = useRef<HTMLButtonElement>(null);

  const task =
    scenario === "late-response"
      ? (fixtureTasks.find((t) => t.id === readyTask.id) ?? null)
      : (fixtureTasks.find((t) => t.id === taskId) ?? null);
  const selectedTaskId = scenario === "late-response" ? failedTask.id : taskId;
  const run = scenario === "late-response" ? null : (fixtureRuns[taskId] ?? null);

  useEffect(() => {
    document.documentElement.setAttribute("data-wux-ready", "true");
  }, []);

  // Host-side focus contract demo: closing restores focus to the origin.
  const handleClose = () => {
    setOpen(false);
    requestAnimationFrame(() => originRef.current?.focus());
  };

  return (
    <div className="min-h-dvh bg-surface text-ink">
      <header className="border-b border-line px-5 py-4">
        <p className="font-mono text-[10.5px] font-semibold uppercase tracking-[0.14em] text-faint">
          WUX-T-0006 · sample-data preview
        </p>
        <h1 className="mt-1 text-[20px] font-semibold tracking-[-0.02em]">TaskInspector preview</h1>
        <p className="mt-1 max-w-[720px] text-[13px] leading-5 text-muted">
          Every fixture below is labeled sample data. Keyboard: Tab to the Close button, Enter to
          close, Escape anywhere closes the panel.
        </p>
      </header>

      <div className="flex flex-wrap gap-6 px-5 py-4">
        <fieldset>
          <legend className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Task</legend>
          <div className="mt-2 flex flex-wrap gap-2">
            {fixtureTasks.map((t) => (
              <button
                key={t.id}
                type="button"
                onClick={() => { setTaskId(t.id); setOpen(true); }}
                aria-pressed={taskId === t.id}
                className={`rounded-lg border px-3 py-1.5 text-[12.5px] font-medium ${taskId === t.id ? "border-ink bg-ink text-surface" : "border-line bg-raised text-ink"}`}
              >
                {t.id}
              </button>
            ))}
          </div>
        </fieldset>

        <fieldset>
          <legend className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Scenario</legend>
          <div className="mt-2 flex flex-wrap gap-2">
            {SCENARIOS.map((s) => (
              <button
                key={s.id}
                type="button"
                onClick={() => { setScenario(s.id); setOpen(true); }}
                aria-pressed={scenario === s.id}
                title={s.hint}
                className={`rounded-lg border px-3 py-1.5 text-[12.5px] font-medium ${scenario === s.id ? "border-ink bg-ink text-surface" : "border-line bg-raised text-ink"}`}
              >
                {s.label}
              </button>
            ))}
          </div>
          <p className="mt-2 max-w-[420px] text-[12px] leading-5 text-muted">
            {SCENARIOS.find((s) => s.id === scenario)?.hint}
          </p>
        </fieldset>
      </div>

      <div className="px-5 pb-8">
        <button
          ref={originRef}
          type="button"
          data-testid="preview-origin"
          onClick={() => setOpen(true)}
          className="rounded-lg border border-line bg-raised px-4 py-2 text-[13px] font-medium text-ink"
        >
          Inspect selected task (origin focus)
        </button>
        {openedTask && (
          <p className="mt-2 text-[12.5px] text-muted">
            Full-task navigation requested for <span className="font-mono">{openedTask}</span> (integration wires the route).
          </p>
        )}
        <ul className="mt-3 max-w-[720px] list-disc space-y-1 pl-5 text-[12.5px] leading-5 text-muted">
          <li>Ready: {readyTask.id} (decision) · Failed: {failedTask.id} · Accepted: {acceptedTask.id} · Long: {longContractTask.id} · Unavailable: {unavailableTask.id}</li>
          <li>Runs: {readyRun.taskId} running · {failedRun.taskId} failed · {acceptedRun.taskId} accepted.</li>
        </ul>
      </div>

      {open && (
        <TaskInspector
          task={scenario === "loading" || scenario === "error" ? null : task}
          run={run}
          selectedTaskId={selectedTaskId}
          loading={scenario === "loading" || scenario === "late-response"}
          error={scenario === "error" ? "preview network failure (sample)" : undefined}
          executionIdentity={scenario === "unobserved" ? { observed: false } : OBSERVED_IDENTITY}
          onClose={handleClose}
          onOpenTask={(id) => setOpenedTask(id)}
        />
      )}
    </div>
  );
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("#root not found");
createRoot(rootEl).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <ConfirmProvider>
          <Preview />
        </ConfirmProvider>
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
);
