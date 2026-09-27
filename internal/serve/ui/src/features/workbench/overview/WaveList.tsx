import { cardClass } from "@/components/ui/primitives";
import { TaskStateBadge } from "@/components/ui/chips";
import type { TaskStateCode, WaveListItem } from "@/types/domain";

/** Waves group by their server-computed state, most urgent first (task-states spec rule 5). */
const GROUP_ORDER: TaskStateCode[] = ["blocked", "needs_input", "working", "in_review", "planned", "backlog", "done", "canceled"];

// Wave IDs are issued in sequence, so ID order is creation order.
const byId = (a: WaveListItem, b: WaveListItem) => a.id.localeCompare(b.id, undefined, { numeric: true });
// ponytail: the list API carries no created/updated date; newest-landed first, then ID. Add dates when the API sends them.
const byLanded = (a: WaveListItem, b: WaveListItem) => (b.landedAt ?? "").localeCompare(a.landedAt ?? "") || byId(b, a);

export function WaveList({ waves, query, onOpenWave, loading, error }: {
  waves: WaveListItem[];
  query: string;
  onOpenWave: (id: string) => void;
  loading: boolean;
  error?: string;
}) {
  if (loading) return <p role="status" aria-label="Loading waves" className="text-[13px] text-muted">Loading waves…</p>;
  if (error) return <p role="alert" className="text-[13px] text-fail">Wave list unavailable. {error}</p>;
  const needle = query.trim().toLowerCase();
  const matches = waves.filter((wave) => `${wave.id} ${wave.title} ${wave.summary ?? ""}`.toLowerCase().includes(needle));
  const groups = GROUP_ORDER.map((code) => {
    const rows = matches.filter((wave) => wave.state.state === code).sort(code === "done" ? byLanded : byId);
    return { code, title: rows[0]?.state.label ?? code, rows };
  }).filter((group) => group.rows.length > 0);
  return <div className="space-y-5">
    {groups.length === 0 ? <p className="text-[13px] text-muted">{waves.length ? "No waves match." : "No waves yet."}</p> : null}
    {groups.map((group) => <section key={group.code} aria-label={group.title}>
      <h2 className="mb-1.5 text-[12px] font-medium text-muted">{group.title} · {group.rows.length}</h2>
      <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{group.rows.map((wave) => {
        const closed = wave.state.category === "closed";
        return <li key={wave.id} className="min-w-0">
          <button type="button" onClick={() => onOpenWave(wave.id)} aria-label={`Open wave ${wave.title}, ${wave.state.label}`} className={`${cardClass(true)} flex h-full w-full flex-col gap-2 p-4`}>
            <span className="flex w-full items-center gap-2">
              <span className="font-mono text-[11px] text-faint">{wave.id}</span>
              <TaskStateBadge state={wave.state} className="ml-auto" />
            </span>
            <span className={`line-clamp-2 text-[14px] font-semibold leading-5 ${closed ? "text-muted" : "text-ink"}`}>{wave.title}</span>
            {wave.state.reason ? <span className="text-[12px] text-muted">{wave.state.reason}</span> : null}
            {wave.summary ? <span className="line-clamp-2 text-[12.5px] leading-[18px] text-muted">{wave.summary}</span> : null}
            <span className="mt-auto flex w-full items-center gap-2.5 pt-1">
              <span className="h-1 flex-1 overflow-hidden rounded-full bg-panel" aria-hidden="true"><span className="block h-full bg-pass" style={{ width: `${wave.memberCount ? (wave.doneCount / wave.memberCount) * 100 : 0}%` }} /></span>
              <span className="font-mono text-[11px] text-muted" aria-label={`${wave.doneCount} of ${wave.memberCount} tasks done`}>{wave.doneCount}/{wave.memberCount}</span>
            </span>
          </button>
        </li>;
      })}</ul>
    </section>)}
  </div>;
}
