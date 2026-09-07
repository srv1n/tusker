import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import "@/styles/app.css";
import { WaveOverview } from "@/features/workbench/overview/WaveOverview";
import { mixedFixture } from "./fixtures";

type Scenario = "mixed" | "empty" | "loading" | "error";

const SCENARIOS: Array<{ id: Scenario; label: string }> = [
  { id: "mixed", label: "Mixed states" },
  { id: "empty", label: "Empty" },
  { id: "loading", label: "Loading" },
  { id: "error", label: "Error" },
];

function App() {
  const fixture = useMemo(() => mixedFixture(), []);
  const [scenario, setScenario] = useState<Scenario>("mixed");
  const [query, setQuery] = useState("");
  const [showCompleted, setShowCompleted] = useState(false);
  const [lastAction, setLastAction] = useState("Sample preview: no navigation yet.");

  useEffect(() => {
    document.querySelector("#root")?.setAttribute("data-wux-ready", "true");
  }, [scenario]);

  return (
    <div style={{ padding: "2rem 1.5rem", background: "var(--color-surface)", minHeight: "100vh" }}>
      <p style={{ fontSize: 11, color: "var(--color-faint)", marginBottom: 4 }}>
        Sample data preview — not live integration.
      </p>
      <h1 style={{ fontSize: 24, fontWeight: 650, color: "var(--color-ink)", marginBottom: 4 }}>
        Work
      </h1>
      <div style={{ display: "flex", gap: 8, margin: "12px 0 20px", flexWrap: "wrap" }} role="group" aria-label="Preview scenario">
        {SCENARIOS.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => setScenario(item.id)}
            aria-pressed={scenario === item.id}
            style={{
              border: "1px solid var(--color-line)",
              borderRadius: 8,
              padding: "6px 12px",
              fontSize: 12,
              fontWeight: scenario === item.id ? 700 : 400,
              background: scenario === item.id ? "var(--color-panel)" : "transparent",
              color: "var(--color-ink)",
            }}
          >
            {item.label}
          </button>
        ))}
      </div>
      <WaveOverview
        waves={scenario === "empty" ? [] : fixture.waves}
        tasks={scenario === "empty" ? [] : fixture.tasks}
        runs={fixture.runs}
        startability={fixture.startability}
        descriptions={fixture.descriptions}
        query={query}
        showCompleted={showCompleted}
        onQueryChange={setQuery}
        onShowCompletedChange={setShowCompleted}
        onOpenWave={(id) => setLastAction(`Sample preview: open wave ${id}. Selection retained.`)}
        onOpenUnassigned={() => setLastAction("Sample preview: open board with unassigned filter.")}
        loading={scenario === "loading"}
        error={scenario === "error" ? "Sample: the wave read failed." : undefined}
      />
      <p role="status" style={{ marginTop: 16, fontSize: 12, color: "var(--color-muted)" }}>
        {lastAction} {query ? `Search: “${query}”.` : ""} Completed filter: {showCompleted ? "shown" : "hidden"}.
      </p>
    </div>
  );
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("#root not found");
createRoot(rootEl).render(<App />);
