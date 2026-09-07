/*
  WUX-T-0005 — isolated WaveFlow preview (sample data only).

  Mounts the real WaveFlow component against labeled fixtures. Records
  selection/viewport events, reports model+layout timings for the 30- and
  100-node graphs, and marks data-wux-ready once rendered. Nothing here is
  production data and nothing here claims live integration.
*/

import { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "@/styles/app.css";
import { WaveFlow } from "@/features/workbench/flow/WaveFlow";
import {
  ALL_FIXTURES,
  hundredFixture,
  liveUpdateBase,
  liveUpdateNext,
  thirtyFixture,
  type FlowFixture,
} from "@/features/workbench/flow/fixtures";
import { buildFlowGraph, layoutFlowGraph, topologyKey, type FlowViewport } from "@/features/workbench/flow/flowGraph";

interface LogEntry {
  at: string;
  text: string;
}

function now(): string {
  return new Date().toISOString().slice("11", 23);
}

function averageMs(fn: () => void, rounds = 5): number {
  const samples: number[] = [];
  for (let i = 0; i < rounds; i += 1) {
    const start = performance.now();
    fn();
    samples.push(performance.now() - start);
  }
  samples.sort((a, b) => a - b);
  return samples[Math.floor(samples.length / 2)] ?? 0;
}

function modelAndLayoutMs(fixture: FlowFixture): { nodes: number; edges: number; ms: number; topology: string } {
  let graph = buildFlowGraph(fixture);
  const ms = averageMs(() => {
    graph = buildFlowGraph(fixture);
    layoutFlowGraph(graph, fixture.memberIds);
  });
  return { nodes: graph.nodes.length, edges: graph.edges.length, ms, topology: topologyKey(graph) };
}

function hostInfo(): string {
  const parts = [
    `platform=${navigator.platform ?? "unknown"}`,
    `cores=${navigator.hardwareConcurrency ?? "unknown"}`,
    `ua=${navigator.userAgent}`,
  ];
  return parts.join(" · ");
}

function initialScenarioKey(scenarios: FlowFixture[]): string {
  // ?scenario=<key> deep-links one fixture so headless evidence capture can
  // render every acceptance row without clicks.
  const key = new URLSearchParams(window.location.search).get("scenario");
  if (key && scenarios.some((item) => item.key === key)) return key;
  return "chain";
}

function Preview() {
  const scenarios = useMemo(() => {
    const live = liveUpdateBase();
    return [...ALL_FIXTURES.filter((item) => item.key !== "hundred"), live];
  }, []);
  const [scenarioKey, setScenarioKey] = useState(() => initialScenarioKey(scenarios));
  const [fixture, setFixture] = useState<FlowFixture>(
    () => scenarios.find((item) => item.key === initialScenarioKey(scenarios)) ?? liveUpdateBase(),
  );
  const [selectedTaskId, setSelectedTaskId] = useState<string | undefined>(undefined);
  const [viewport, setViewport] = useState<FlowViewport>({ x: 0, y: 0, scale: 1 });
  const [log, setLog] = useState<LogEntry[]>([{ at: now(), text: "preview mounted with sample data" }]);
  const [liveAdvanced, setLiveAdvanced] = useState(false);
  const paintStart = useRef(0);

  const append = (text: string): void => {
    setLog((prev) => [...prev.slice(-11), { at: now(), text }]);
  };

  useEffect(() => {
    document.documentElement.setAttribute("data-wux-ready", "true");
  }, []);

  // Full-paint timing: from scenario commit to the next painted frame.
  useEffect(() => {
    if (paintStart.current === 0) return;
    const start = paintStart.current;
    const frame = requestAnimationFrame(() => {
      append(`painted "${fixture.label}" in ${(performance.now() - start).toFixed(1)} ms (commit to frame)`);
    });
    return () => cancelAnimationFrame(frame);
  }, [fixture]);

  const perf = useMemo(() => {
    const thirty = thirtyFixture();
    const hundred = hundredFixture();
    return { thirty: modelAndLayoutMs(thirty), hundred: modelAndLayoutMs(hundred) };
  }, []);

  const choose = (key: string): void => {
    const next = scenarios.find((item) => item.key === key);
    if (!next) return;
    paintStart.current = performance.now();
    setScenarioKey(key);
    setFixture(next);
    setSelectedTaskId(undefined);
    setViewport({ x: 0, y: 0, scale: 1 });
    setLiveAdvanced(false);
    append(`scenario "${next.label}" selected; viewport reset to origin`);
  };

  const advanceLive = (): void => {
    const next = liveAdvanced ? liveUpdateBase() : liveUpdateNext();
    paintStart.current = performance.now();
    setFixture({ ...next });
    setLiveAdvanced(!liveAdvanced);
    append(
      liveAdvanced
        ? "live update rewound to base states; viewport untouched"
        : "live update applied (same topology, advanced states); viewport untouched",
    );
  };

  const openTask = (): void => {
    if (!selectedTaskId) return;
    append(`task panel would open for ${selectedTaskId}; graph position kept`);
  };

  return (
    <div className="min-h-screen bg-surface text-ink">
      <div className="border-b border-line bg-warn-soft px-5 py-2">
        <p className="text-[12.5px] text-ink">
          Sample data preview — every task, state and model below is a fixture. Not production data; not live integration.
        </p>
      </div>
      <div className="mx-auto w-full max-w-[1240px] px-5 pb-16 pt-6">
        <header className="mb-5">
          <p className="font-mono text-[11px] uppercase tracking-[0.14em] text-faint">WUX-T-0005 · WaveFlow</p>
          <h1 className="mt-1 text-[24px] font-semibold tracking-tight">Readable interactive wave graph</h1>
          <p className="mt-1 max-w-[760px] text-[14px] text-muted">{fixture.description}</p>
        </header>

        <div className="mb-4 flex flex-wrap gap-2" role="group" aria-label="Preview scenarios">
          {scenarios.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => choose(item.key)}
              aria-pressed={scenarioKey === item.key}
              className={`rounded-md border px-2.5 py-1.5 text-[12.5px] font-medium ${
                scenarioKey === item.key
                  ? "border-accent bg-accent-soft text-ink"
                  : "border-line bg-panel text-ink-soft hover:bg-hover"
              }`}
            >
              {item.label}
            </button>
          ))}
          {scenarioKey === "live" && (
            <button
              type="button"
              onClick={advanceLive}
              className="rounded-md border border-line bg-panel px-2.5 py-1.5 text-[12.5px] font-medium text-ink-soft hover:bg-hover"
            >
              {liveAdvanced ? "Rewind update" : "Apply live update"}
            </button>
          )}
        </div>

        <WaveFlow
          memberIds={fixture.memberIds}
          tasks={fixture.tasks}
          runs={fixture.runs}
          dependencyFacts={fixture.dependencyFacts}
          selectedTaskId={selectedTaskId}
          viewport={viewport}
          onSelectTask={(id) => {
            setSelectedTaskId(id);
            append(`selected ${id}; viewport unchanged`);
          }}
          onViewportChange={(next) => {
            setViewport(next);
          }}
        />

        <div className="mt-4 grid gap-4 lg:grid-cols-2">
          <section className="rounded-lg border border-line bg-panel p-4" aria-label="Inspector state">
            <h2 className="text-[14px] font-semibold">Inspector state</h2>
            <p className="mt-1 text-[13px] text-muted">
              Selected: <span className="font-mono text-ink">{selectedTaskId ?? "none"}</span>
            </p>
            <p className="text-[13px] text-muted">
              Viewport: <span className="font-mono text-ink">x={viewport.x} y={viewport.y} scale={viewport.scale}</span>
            </p>
            <button
              type="button"
              onClick={openTask}
              disabled={!selectedTaskId}
              className="mt-2 rounded-md border border-line bg-surface px-2.5 py-1.5 text-[12.5px] font-medium text-ink-soft hover:bg-hover disabled:opacity-50"
            >
              Open task (keeps graph position)
            </button>
          </section>
          <section className="rounded-lg border border-line bg-panel p-4" aria-label="Event log">
            <h2 className="text-[14px] font-semibold">Event log</h2>
            <ol className="mt-2 max-h-40 space-y-1 overflow-y-auto font-mono text-[11.5px] text-muted">
              {log.map((entry, index) => (
                <li key={index}>
                  {entry.at} · {entry.text}
                </li>
              ))}
            </ol>
          </section>
        </div>

        <section className="mt-4 rounded-lg border border-line bg-panel p-4" aria-label="Render measurements">
          <h2 className="text-[14px] font-semibold">Render measurements</h2>
          <p className="mt-1 font-mono text-[11.5px] leading-5 text-muted">{hostInfo()}</p>
          <ul className="mt-2 space-y-1 font-mono text-[11.5px] text-muted">
            <li>
              30 nodes / {perf.thirty.edges} edges (long titles): model + layout {perf.thirty.ms.toFixed(2)} ms median-of-5 ·
              topology {perf.thirty.topology.slice(0, 48)}…
            </li>
            <li>
              100 nodes / {perf.hundred.edges} edges: model + layout {perf.hundred.ms.toFixed(2)} ms median-of-5 ·
              topology {perf.hundred.topology.slice(0, 48)}…
            </li>
          </ul>
          <p className="mt-2 text-[12px] text-muted">
            Method: median of 5 synchronous buildFlowGraph + layoutFlowGraph passes in this browser; paint timing per
            scenario appears in the event log (commit to next frame). Full-page screenshot timings are recorded in the
            evidence report.
          </p>
        </section>
      </div>
    </div>
  );
}

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(<Preview />);
  document.documentElement.setAttribute("data-wux-ready", "true");
}
