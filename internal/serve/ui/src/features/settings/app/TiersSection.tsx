import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { ModelLevelsReport } from "@/types/domain";

const TIERS = { light: "Tier 1 · Light", standard: "Tier 2 · Standard", demanding: "Tier 3 · Demanding" } as const;

export function TiersSection({ projectId, scope = "global" }: { projectId?: string; scope?: "global" | "project" }) {
  const [levels, setLevels] = useState<ModelLevelsReport>();
  const [drafts, setDrafts] = useState<Record<string, string[]>>({});
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    void (scope === "global" ? api.modelLevels(undefined, scope) : api.modelLevels(projectId)).then((next) => {
      setLevels(next);
      setDrafts(Object.fromEntries(next.levels.flatMap((row) => [[`${row.level}:execute`, row.execute.profiles], [`${row.level}:review`, row.review.profiles]])));
    }).catch((cause) => setError(cause instanceof ApiError ? cause.message : String(cause)));
  }, [projectId]);
  const names = (tier: string, lane: "execute" | "review") => drafts[`${tier}:${lane}`] || [];
  const setNames = (tier: string, lane: "execute" | "review", next: string[]) => setDrafts((old) => ({ ...old, [`${tier}:${lane}`]: next }));
  const label = (name: string) => { const profile = levels?.profiles[name]; return profile ? `${profile.display_name || name} · ${profile.model} · ${profile.effort}` : `${name} · unavailable`; };
  const eligible = (tier: keyof typeof TIERS) => Object.keys(levels?.profiles || {}).filter((name) => {
    const profile = levels!.profiles[name];
    return !profile.disabled && !["disabled", "unavailable"].includes(levels!.profile_states[name]) && profile.eligible_tiers.includes(tier);
  });
  async function save(tier: keyof typeof TIERS, lane: "execute" | "review") {
    if (!levels) return;
    setSaving(true); setError("");
    try {
      const next = await api.modelLevelsSet(tier, lane, names(tier, lane), levels.revision, scope, projectId);
      setLevels(next);
      setNames(tier, lane, next.levels.find((row) => row.level === tier)?.[lane].profiles || []);
    } catch (cause) { setError(cause instanceof ApiError ? cause.message : String(cause)); }
    finally { setSaving(false); }
  }
  return <section className="animate-rise" aria-labelledby="tiers-title">
    <div className="mb-4"><h2 id="tiers-title" className="font-serif text-[20px] font-semibold text-ink">Tiers</h2><p className="mt-1 max-w-[62ch] text-[13px] leading-relaxed text-muted">Choose eligible primary workers and reviewers. Fallbacks are only used when explicitly ordered here.</p></div>
    {error && <p role="alert" className="mb-3 rounded-md border border-danger/25 bg-danger-soft px-3 py-2 text-[12px] text-danger">{error}</p>}
    {!levels && !error && <p className="text-[13px] text-muted">Loading tier assignments…</p>}
    <div className="divide-y divide-line rounded-[10px] border border-line bg-surface">{levels?.levels.map((row) => <div key={row.level} className="grid gap-3 px-4 py-4 md:grid-cols-[10rem_1fr_1fr]">
      <div><h3 className="text-[13px] font-semibold text-ink">{TIERS[row.level]}</h3><p className="mt-1 text-[10px] text-muted">{scope === "project" ? `Inherited from ${row.execute.source}` : "Global default"}</p></div>
      {(["execute", "review"] as const).map((lane) => {
        const selected = names(row.level, lane); const choices = eligible(row.level);
        return <div key={lane}>
          <label className="grid gap-1 text-[11px] font-medium text-muted">{lane === "execute" ? "Worker" : "Reviewer"}
            <select aria-label={`${TIERS[row.level]} ${lane}`} value={selected[0] || ""} onChange={(event) => setNames(row.level, lane, event.target.value ? [event.target.value, ...selected.slice(1).filter((name) => name !== event.target.value)] : selected.slice(1))} className="rounded-md border border-line bg-canvas px-2 py-2 text-[12px] text-ink">
              <option value="">Choose profile</option>{[...new Set([...choices, ...selected])].map((name) => <option key={name} value={name} disabled={!choices.includes(name)}>{label(name)}{choices.includes(name) ? "" : " · unavailable"}</option>)}
            </select>
          </label>
          <details className="mt-2 text-[11px] text-muted"><summary className="cursor-pointer">Fallbacks ({Math.max(0, selected.length - 1)})</summary><div className="mt-1 grid gap-1">
            {selected.slice(1).map((name, index) => <div key={`${name}:${index}`} className="flex items-center gap-1"><span className="min-w-0 flex-1 truncate rounded border border-line-soft px-2 py-1">{label(name)}</span><button type="button" aria-label={`Move ${name} fallback earlier`} disabled={index === 0} onClick={() => { const next = [...selected]; [next[index], next[index + 1]] = [next[index + 1], next[index]]; setNames(row.level, lane, next); }} className="px-1 disabled:opacity-30">↑</button><button type="button" aria-label={`Move ${name} fallback later`} disabled={index === selected.length - 2} onClick={() => { const next = [...selected]; [next[index + 1], next[index + 2]] = [next[index + 2], next[index + 1]]; setNames(row.level, lane, next); }} className="px-1 disabled:opacity-30">↓</button><button type="button" aria-label={`Remove ${name} fallback`} onClick={() => setNames(row.level, lane, selected.filter((_, itemIndex) => itemIndex !== index + 1))} className="px-1 text-danger">×</button></div>)}
            <select aria-label={`Add ${TIERS[row.level]} ${lane} fallback`} defaultValue="" onChange={(event) => { if (event.target.value) { setNames(row.level, lane, [...selected, event.target.value]); event.target.value = ""; } }} className="rounded-md border border-line bg-canvas px-2 py-1 text-[11px] text-ink"><option value="">Add fallback…</option>{choices.filter((name) => !selected.includes(name)).map((name) => <option key={name} value={name}>{label(name)}</option>)}</select>
          </div></details>
          <div className="mt-2 flex gap-2"><button type="button" disabled={saving} onClick={() => void save(row.level, lane)} className="text-[11px] text-ink underline disabled:opacity-40">Save changes</button>{scope === "project" && row[lane].overridden && <button type="button" disabled={saving} onClick={() => void api.modelLevelsReset(row.level, lane, levels.revision, scope, projectId).then(setLevels).catch((cause) => setError(cause instanceof ApiError ? cause.message : String(cause)))} className="text-[11px] text-muted underline disabled:opacity-40">Reset</button>}</div>
        </div>;
      })}
    </div>)}</div>
  </section>;
}
