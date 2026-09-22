import type { WaveListItem } from "@/types/domain";
import "./overview.css";

const history = (wave: WaveListItem) => Boolean(wave.landedAt) || ["landed", "closed", "delivered", "completed", "cancelled", "superseded"].includes(wave.status.toLowerCase());

function tag(wave: WaveListItem): string {
  if (wave.landedAt) return "Completed";
  if (wave.status && wave.status !== "open") return `${wave.status.replaceAll("_", " ")} on record`;
  if (wave.authorization === "armed") return "Armed on record";
  if (wave.authorization === "paused") return "Paused on record";
  if (wave.authorization === "stale") return "State needs review";
  return "Not armed";
}

export function WaveList({ waves, projectName, backgroundWorkEnabled, query, onQueryChange, onOpenWave, loading, error }: {
  waves: WaveListItem[];
  projectName: string;
  backgroundWorkEnabled?: boolean;
  query: string;
  onQueryChange: (value: string) => void;
  onOpenWave: (id: string) => void;
  loading: boolean;
  error?: string;
}) {
  if (loading) return <div className="wux-ov" aria-label="Loading wave overview">{[0, 1, 2].map((i) => <div key={i} className="wux-ov-skeleton" />)}</div>;
  if (error) return <div className="wux-ov" role="alert">Wave list unavailable. {error}</div>;
  const needle = query.trim().toLowerCase();
  const matches = waves.filter((wave) => `${wave.id} ${wave.title} ${wave.summary ?? ""}`.toLowerCase().includes(needle));
  return <div className="wux-ov">
    <header className="mb-5 border-b border-line pb-4">
      <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Project</p>
      <h1 className="mt-1 font-serif text-[26px] font-semibold text-ink">{projectName}</h1>
    </header>
    <div className="wux-ov-controls"><label className="wux-ov-search">
      <span className="wux-ov-search-label">Search waves</span>
      <input type="search" value={query} onChange={(event) => onQueryChange(event.target.value)} placeholder="Search ID, title, or description" aria-label="Search waves by ID, title, or description" />
    </label></div>
    {matches.length === 0 ? <p className="wux-ov-empty">{waves.length ? "No waves match." : "No waves yet."}</p> : null}
    {([false, true] as const).map((past) => {
      const rows = matches.filter((wave) => history(wave) === past);
      return rows.length ? <section key={String(past)} aria-label={past ? "History" : "Waves"} className="wux-ov-group">
        <div className="wux-ov-group-head"><h2>{past ? "History" : "Waves"}</h2><span className="wux-ov-count">{rows.length}</span></div>
        <ul className="wux-ov-rows">{rows.map((wave) => <li key={wave.id} className="wux-ov-row">
          <button type="button" className="wux-ov-card" onClick={() => onOpenWave(wave.id)} aria-label={`Open wave ${wave.title}`}>
            <div className="wux-ov-main">
              <span className="wux-ov-title">{wave.title}</span><span className="wux-ov-id">{wave.id}</span>
              {wave.summary ? <p className="wux-ov-desc">{wave.summary}</p> : null}
              <p className="wux-ov-progress">{wave.doneCount} of {wave.memberCount} tasks done</p>
              {!past && wave.authorization === "armed" && backgroundWorkEnabled === false ? <p className="wux-ov-detail">Background work is off. Turn it on in Settings to dispatch this wave.</p> : null}
            </div>
            <div className="wux-ov-side"><span className="wux-ov-state wux-ov-tone-neutral">{tag(wave)}</span></div>
          </button>
        </li>)}</ul>
      </section> : null;
    })}
  </div>;
}
