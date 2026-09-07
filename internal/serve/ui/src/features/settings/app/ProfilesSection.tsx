/*
  Runner profiles tab — a profile is the named bundle the daemon uses to launch
  an agent (harness · model · effort · permission preset · subagent policy).
  Built-ins are editable-by-copy: Duplicate leaves the original intact.
*/

import { useState } from "react";
import { Copy, Pencil, Play, Plus } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import type { RunnerConformanceReport } from "@/types/domain";
import { DashedButton, HarnessChip } from "./parts";
import { runnerProfiles, type RunnerProfile } from "./mock";

function ProfileFact({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-faint">{k}</span>
      <span className="text-ink-soft">{v}</span>
    </div>
  );
}

function ProfileCard({ p }: { p: RunnerProfile }) {
  return (
    <div className="rounded-[10px] border border-line px-4 py-[15px]">
      <div className="mb-3 flex items-center gap-[9px]">
        <span className="font-serif text-[16px] font-semibold text-ink">{p.name}</span>
        <HarnessChip harness={p.harness} />
      </div>

      <div className="flex flex-col gap-1.5 font-mono text-[11px]">
        <ProfileFact k="model" v={p.model} />
        <ProfileFact k="effort" v={p.effort} />
        <ProfileFact k="permission" v={p.preset} />
        <ProfileFact k="subagents" v={p.subagents} />
      </div>

      <div className="mt-3 flex items-center justify-between border-t border-line-soft pt-[11px]">
        <span className="font-mono text-[10px] text-fainter">
          {p.builtin ? "built-in · copy to edit" : "custom"}
        </span>
        <span className="flex items-center gap-3">
          {/* TODO(api): clone / edit a profile through the settings API */}
          <button
            type="button"
            disabled
            title="Runner profile persistence is not available yet"
            className="inline-flex items-center gap-1 text-[11.5px] text-muted transition-colors hover:text-ink"
          >
            <Copy size={12} strokeWidth={2} /> Duplicate
          </button>
          <button
            type="button"
            disabled
            title="Runner profile persistence is not available yet"
            className="inline-flex items-center gap-1 text-[11.5px] text-muted transition-colors hover:text-ink"
          >
            <Pencil size={12} strokeWidth={2} /> Edit
          </button>
        </span>
      </div>
    </div>
  );
}

export function ProfilesSection() {
  const [harness, setHarness] = useState("codex_exec");
  const [preset, setPreset] = useState("read-only");
  const [exercise, setExercise] = useState("print");
  const [report, setReport] = useState<RunnerConformanceReport>();
  const [error, setError] = useState("");
  const [running, setRunning] = useState(false);

  async function testHarness(live: boolean) {
    setRunning(true);
    setError("");
    try {
      setReport(await api.runnerConformance(harness, preset, live, live ? exercise : ""));
    } catch (cause) {
      setReport(undefined);
      setError(cause instanceof ApiError ? cause.message : String(cause));
    } finally {
      setRunning(false);
    }
  }

  return (
    <div className="animate-rise">
      <p className="mb-4 max-w-[64ch] text-[13px] leading-relaxed text-muted">
        A profile is the bundle the daemon uses to launch an agent. Built-ins are editable by copy —
        duplicating one leaves the original intact.
      </p>

      <section className="mb-4 rounded-[10px] border border-line bg-surface px-4 py-[15px]" aria-labelledby="runner-conformance-title">
        <h2 id="runner-conformance-title" className="font-serif text-[16px] font-semibold text-ink">Test an installed harness</h2>
        <p className="mb-3 mt-1 text-[12px] leading-relaxed text-muted">
          Local checks verify the executable, version, authentication, and exact permission flags. A live canary also starts one disposable model turn.
        </p>
        <div className="flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1 font-mono text-[10px] text-faint">
            HARNESS
            <select value={harness} onChange={(event) => setHarness(event.target.value)} className="rounded-md border border-line bg-canvas px-2 py-1.5 text-[12px] text-ink">
              <option value="codex_exec">Codex CLI</option>
              <option value="muse">Muse profile</option>
              <option value="claude-code">Claude CLI</option>
            </select>
          </label>
          <label className="flex flex-col gap-1 font-mono text-[10px] text-faint">
            PERMISSION PRESET
            <select value={preset} onChange={(event) => setPreset(event.target.value)} className="rounded-md border border-line bg-canvas px-2 py-1.5 text-[12px] text-ink">
              <option value="read-only">Read only</option>
              <option value="workspace-write-offline">Workspace write, offline</option>
              <option value="workspace-write-network">Workspace write, network</option>
            </select>
          </label>
          <label className="flex flex-col gap-1 font-mono text-[10px] text-faint">
            LIVE EXERCISE
            <select value={exercise} onChange={(event) => setExercise(event.target.value)} className="rounded-md border border-line bg-canvas px-2 py-1.5 text-[12px] text-ink">
              <option value="print">Print token</option>
              <option value="timer">One-second timer</option>
            </select>
          </label>
          <button type="button" disabled={running} onClick={() => void testHarness(false)} className="rounded-md border border-line px-3 py-1.5 text-[12px] font-medium text-ink hover:bg-hover disabled:opacity-50">Run local checks</button>
          <button type="button" disabled={running} onClick={() => void testHarness(true)} className="inline-flex items-center gap-1 rounded-md bg-ink px-3 py-1.5 text-[12px] font-medium text-surface disabled:opacity-50"><Play size={12} /> Run live canary</button>
        </div>
        {error && <p role="alert" className="mt-3 text-[12px] text-danger">{error}</p>}
        {report && (
          <div className="mt-3 border-t border-line-soft pt-3" role="status">
            <p className="mb-2 font-mono text-[11px] text-ink-soft">
              {report.harness_id} · {report.version || "unknown version"} · {report.ready ? "ready" : report.live ? "not ready" : "local checks complete"}
            </p>
            <ul className="grid gap-1 font-mono text-[10px]">
              {report.cases.map((item) => <li key={item.id} className="flex gap-2"><span className={item.result === "pass" ? "text-success" : item.result === "not_run" ? "text-faint" : "text-danger"}>{item.result}</span><span className="text-ink-soft">{item.id}{item.evidence ? ` — ${item.evidence}` : ""}</span></li>)}
            </ul>
          </div>
        )}
      </section>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {runnerProfiles.map((p) => (
          <ProfileCard key={p.name} p={p} />
        ))}
      </div>

      {/* TODO(api): create a new profile */}
      <DashedButton disabled title="Runner profile persistence is not available yet" className="mt-3.5 inline-flex items-center gap-1">
        <Plus size={13} strokeWidth={2} /> New profile
      </DashedButton>
    </div>
  );
}
