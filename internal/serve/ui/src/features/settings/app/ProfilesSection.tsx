/*
  Runner profiles tab — a profile is the named bundle the daemon uses to launch
  an agent (harness · model · effort · permission preset · subagent policy).
  Built-ins are editable-by-copy: Duplicate leaves the original intact.
*/

import { useEffect, useState } from "react";
import { Play } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import type { ModelLevelsReport, RunnerConformanceReport } from "@/types/domain";
import { HarnessChip } from "./parts";

function ProfileFact({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex min-w-0 items-start justify-between gap-3">
      <span className="text-faint">{k}</span>
      <span className="min-w-0 break-all text-right text-ink-soft">{v}</span>
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
  const [levels, setLevels] = useState<ModelLevelsReport>();
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [scope, setScope] = useState("project");
  const [profile, setProfile] = useState({ name: "", harness: "codex_exec", model: "", effort: "medium", preset: "workspace-write-offline" });

  useEffect(() => {
    void api.modelLevels().then((next) => {
      setLevels(next);
      setDrafts(Object.fromEntries(next.levels.flatMap((row) => [[`${row.level}:execute`, row.execute.profiles.join(", ")], [`${row.level}:review`, row.review.profiles.join(", ")]])));
    }).catch((cause) => setError(cause instanceof ApiError ? cause.message : String(cause)));
  }, []);

  async function saveLevel(level: string, lane: string) {
    if (!levels) return;
    setRunning(true); setError("");
    try {
      const names = (drafts[`${level}:${lane}`] || "").split(",").map((value) => value.trim()).filter(Boolean);
      setLevels(await api.modelLevelsSet(level, lane, names, levels.revision, scope));
    } catch (cause) { setError(cause instanceof ApiError ? cause.message : String(cause)); }
    finally { setRunning(false); }
  }

  async function resetLevel(level: string, lane: string) {
    if (!levels) return;
    setRunning(true); setError("");
    try { const next = await api.modelLevelsReset(level, lane, levels.revision, scope); setLevels(next); setDrafts((old) => ({...old, [`${level}:${lane}`]: next.levels.find((row) => row.level === level)?.[lane as "execute" | "review"].profiles.join(", ") || ""})); }
    catch (cause) { setError(cause instanceof ApiError ? cause.message : String(cause)); }
    finally { setRunning(false); }
  }

  async function saveProfile() {
    if (!levels) return;
    setRunning(true); setError("");
    try {
      setLevels(await api.modelProfileSet(profile, levels.revision, scope));
      setProfile((old) => ({ ...old, name: "", model: "" }));
    } catch (cause) { setError(cause instanceof ApiError ? cause.message : String(cause)); }
    finally { setRunning(false); }
  }

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
        Configure ordered implementation and reviewer profiles once. The first profile is primary; later entries are explicit fallbacks.
      </p>

      <section className="mb-4 min-w-0 overflow-hidden rounded-[10px] border border-line bg-surface px-4 py-[15px]" aria-labelledby="model-level-title">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2"><h2 id="model-level-title" className="font-serif text-[16px] font-semibold text-ink">Models</h2><label className="font-mono text-[10px] text-faint">EDIT <select value={scope} onChange={(event) => setScope(event.target.value)} className="ml-2 rounded-md border border-line bg-canvas px-2 py-1 text-ink"><option value="project">This project</option><option value="global">Global defaults</option></select></label></div>
        {!levels && !error && <p className="text-[12px] text-muted">Loading model configuration…</p>}
        {levels?.levels.map((row) => <div key={row.level} className="grid gap-2 border-t border-line-soft py-3 md:grid-cols-[7rem_1fr_1fr]">
          <div><strong className="capitalize text-[13px] text-ink">{row.level}</strong><p className="font-mono text-[9px] text-faint">{row.execute.source}</p></div>
          {(["execute", "review"] as const).map((lane) => <label key={lane} className="min-w-0 text-[10px] uppercase text-faint">{lane}
            <div className="mt-1 grid min-w-0 grid-cols-[minmax(0,1fr)_auto_auto] gap-1"><input aria-label={`${row.level} ${lane} profiles`} value={drafts[`${row.level}:${lane}`] || ""} onChange={(event) => setDrafts((old) => ({...old, [`${row.level}:${lane}`]: event.target.value}))} placeholder="primary, fallback" className="min-w-0 rounded-md border border-line bg-canvas px-2 py-1.5 font-mono text-[11px] normal-case text-ink"/><button disabled={running} onClick={() => void saveLevel(row.level, lane)} type="button" className="rounded-md border border-line px-2 text-[10px] text-ink">Save</button>{scope === "project" && <button disabled={running || !row[lane].overridden} onClick={() => void resetLevel(row.level, lane)} type="button" className="px-1 text-[10px] normal-case text-muted disabled:opacity-40">Reset</button>}</div>
          </label>)}
        </div>)}
        <details className="border-t border-line-soft pt-3">
          <summary className="cursor-pointer text-[12px] font-medium text-ink">Add or update profile</summary>
          <div className="mt-2 grid gap-2 sm:grid-cols-2 lg:grid-cols-6">
            <input aria-label="Profile name" placeholder="profile name" value={profile.name} onChange={(event) => setProfile({...profile, name: event.target.value})} className="rounded-md border border-line bg-canvas px-2 py-1.5 text-[11px] text-ink" />
            <select aria-label="Profile harness" value={profile.harness} onChange={(event) => setProfile({...profile, harness: event.target.value})} className="rounded-md border border-line bg-canvas px-2 py-1.5 text-[11px] text-ink"><option value="codex_exec">Codex CLI</option><option value="muse">Muse</option><option value="claude-code">Claude CLI</option></select>
            <input aria-label="Profile model" placeholder="exact model ID" value={profile.model} onChange={(event) => setProfile({...profile, model: event.target.value})} className="rounded-md border border-line bg-canvas px-2 py-1.5 text-[11px] text-ink" />
            <select aria-label="Profile effort" value={profile.effort} onChange={(event) => setProfile({...profile, effort: event.target.value})} className="rounded-md border border-line bg-canvas px-2 py-1.5 text-[11px] text-ink">{["low", "medium", "high", "xhigh", "max", "ultra"].map((value) => <option key={value}>{value}</option>)}</select>
            <select aria-label="Profile permission" value={profile.preset} onChange={(event) => setProfile({...profile, preset: event.target.value})} className="rounded-md border border-line bg-canvas px-2 py-1.5 text-[11px] text-ink"><option value="read-only">Read only</option><option value="workspace-write-offline">Write, offline</option><option value="workspace-write-network">Write, network</option></select>
            <button disabled={running || !profile.name.trim() || !profile.model.trim()} onClick={() => void saveProfile()} type="button" className="rounded-md bg-ink px-3 py-1.5 text-[11px] text-surface disabled:opacity-40">Save profile</button>
          </div>
          <p className="mt-2 text-[10px] text-muted">Exact manual values are preserved and marked unverified until the installed harness validates them.</p>
        </details>
        {levels && Object.keys(levels.profiles).length === 0 && <p className="mt-3 text-[12px] text-danger">No profiles configured. Add one above or run <code>tusker models profile-set</code>.</p>}
      </section>

      <section className="mb-4 min-w-0 overflow-hidden rounded-[10px] border border-line bg-surface px-4 py-[15px]" aria-labelledby="runner-conformance-title">
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
          <button type="button" disabled={running} onClick={() => void testHarness(false)} className="w-full rounded-md border border-line px-3 py-1.5 text-[12px] font-medium text-ink hover:bg-hover disabled:opacity-50 sm:w-auto">Run local checks</button>
          <button type="button" disabled={running} onClick={() => void testHarness(true)} className="inline-flex w-full items-center justify-center gap-1 rounded-md bg-ink px-3 py-1.5 text-[12px] font-medium text-surface disabled:opacity-50 sm:w-auto"><Play size={12} /> Run live canary</button>
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

      <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">{Object.entries(levels?.profiles || {}).map(([name, p]) => <div key={name} className="min-w-0 overflow-hidden rounded-[10px] border border-line px-4 py-[15px]"><div className="mb-3 flex min-w-0 flex-wrap items-center gap-2"><span className="break-all font-serif text-[15px] font-semibold text-ink">{name}</span><HarnessChip harness={p.harness} /></div><div className="flex min-w-0 flex-col gap-1 font-mono text-[11px]"><ProfileFact k="model" v={p.model}/><ProfileFact k="effort" v={p.effort}/><ProfileFact k="permission" v={p.permission_preset || "unavailable"}/><ProfileFact k="catalog" v={levels?.profile_states[name] || "unavailable"}/></div></div>)}</div>
    </div>
  );
}
