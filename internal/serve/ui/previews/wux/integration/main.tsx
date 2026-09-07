import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import "@/styles/app.css";
import { WaveOverview } from "@/features/workbench/overview";
import { TaskBoard } from "@/features/workbench/board";
import { mixedFixture } from "../overview/fixtures";

function App() {
  const fixture = useMemo(() => mixedFixture(), []);
  const [tab, setTab] = useState<"waves" | "board">("waves");
  const [query, setQuery] = useState("");
  const [completed, setCompleted] = useState(false);
  const [mode, setMode] = useState<"board" | "list">("board");
  useEffect(() => { document.querySelector("#root")?.setAttribute("data-wux-ready", "true"); }, []);
  return <main className="min-h-screen bg-surface px-4 py-6 text-ink sm:px-8">
    <div className="mx-auto max-w-[1180px]">
      <p className="font-mono text-[10px] uppercase tracking-[0.14em] text-faint">Sample data · integrated surface preview</p>
      <h1 className="mt-1 font-serif text-[34px] font-semibold">Work</h1>
      <nav className="mb-7 mt-5 flex gap-5 border-b border-line" aria-label="Work views">
        {(["waves", "board"] as const).map((value) => <button key={value} type="button" aria-current={tab === value ? "page" : undefined} onClick={() => setTab(value)} className={tab === value ? "border-b-2 border-ink pb-2 text-[13px] font-semibold" : "pb-2 text-[13px] text-muted"}>{value === "waves" ? "Waves" : "Board"}</button>)}
      </nav>
      {tab === "waves" ? <WaveOverview {...fixture} query={query} showCompleted={completed} onQueryChange={setQuery} onShowCompletedChange={setCompleted} onOpenWave={() => {}} onOpenUnassigned={() => setTab("board")} /> : <TaskBoard tasks={fixture.tasks} runs={fixture.runs} mode={mode} onModeChange={setMode} onSelectTask={() => {}} selectedTags={[]} onSelectedTagsChange={() => {}} tagsAvailable={false} />}
    </div>
  </main>;
}

const root = document.getElementById("root");
if (!root) throw new Error("#root not found");
createRoot(root).render(<App />);
