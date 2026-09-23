import { cardClass } from "@/components/ui/primitives";
import type { WaveListItem } from "@/types/domain";

const done = (wave: WaveListItem) => Boolean(wave.landedAt) || (wave.memberCount > 0 && wave.doneCount === wave.memberCount) || ["landed", "closed", "delivered", "completed", "cancelled", "superseded"].includes(wave.status.toLowerCase());
const autoRun = (wave: WaveListItem) => wave.authorization === "armed";
const running = (wave: WaveListItem) => !done(wave) && autoRun(wave) && wave.liveRun === true;

/** A chip only when the row is not simply queued, running, or done. */
function unusual(wave: WaveListItem, backgroundWorkEnabled?: boolean): { label: string; why?: string } | null {
  if (done(wave)) return null;
  if (wave.authorization === "paused") return { label: "Paused", why: wave.recovery?.blockingCause };
  if (wave.authorization === "stale") return { label: "Needs review", why: wave.recovery?.blockingCause };
  if (autoRun(wave) && backgroundWorkEnabled === false) return { label: "Blocked", why: "Background work is off for this project." };
  const code = wave.recovery?.causeCode ?? "";
  if (autoRun(wave) && (code === "project_disabled" || code.startsWith("doctor-") || code === "reconcile-overdue")) return { label: "Blocked", why: wave.recovery?.blockingCause };
  return null;
}

// Wave IDs are issued in sequence, so ID order is creation order.
const byId = (a: WaveListItem, b: WaveListItem) => a.id.localeCompare(b.id, undefined, { numeric: true });
// ponytail: the list API carries no created/updated date; newest-landed first, then ID. Add dates when the API sends them.
const byLanded = (a: WaveListItem, b: WaveListItem) => (b.landedAt ?? "").localeCompare(a.landedAt ?? "") || byId(b, a);

type Tone = "on" | "attention" | "off" | "done";
const DOT: Record<Tone, string> = {
  on: "bg-pass",
  attention: "bg-warn",
  off: "border border-faint",
  done: "bg-faint/50",
};
const TONE_LABEL: Record<Tone, string> = { on: "Auto-run on", attention: "Needs you", off: "Auto-run off", done: "Done" };

function Dot({ tone }: { tone: Tone }) {
  return <span aria-hidden="true" className={`inline-block h-2 w-2 shrink-0 rounded-full ${DOT[tone]}`} />;
}

export function WaveList({ waves, backgroundWorkEnabled, query, onOpenWave, loading, error }: {
  waves: WaveListItem[];
  backgroundWorkEnabled?: boolean;
  query: string;
  onOpenWave: (id: string) => void;
  loading: boolean;
  error?: string;
}) {
  if (loading) return <p role="status" aria-label="Loading waves" className="text-[13px] text-muted">Loading waves…</p>;
  if (error) return <p role="alert" className="text-[13px] text-fail">Wave list unavailable. {error}</p>;
  const needle = query.trim().toLowerCase();
  const tone = (wave: WaveListItem): Tone => done(wave) ? "done" : unusual(wave, backgroundWorkEnabled) ? "attention" : autoRun(wave) ? "on" : "off";
  const matches = waves.filter((wave) => `${wave.id} ${wave.title} ${wave.summary ?? ""}`.toLowerCase().includes(needle));
  const groups = [
    { title: "Needs you", rows: matches.filter((wave) => tone(wave) === "attention").sort(byId) },
    { title: "Running", rows: matches.filter((wave) => tone(wave) === "on" && running(wave)).sort(byId) },
    { title: "Review wait", rows: matches.filter((wave) => tone(wave) === "on" && wave.reviewWait && !running(wave)).sort(byId) },
    { title: "Up next", rows: matches.filter((wave) => (tone(wave) === "on" && !wave.reviewWait && !running(wave)) || tone(wave) === "off").sort(byId) },
    { title: "Done", rows: matches.filter(done).sort(byLanded) },
  ].filter((group) => group.rows.length > 0);
  const open = waves.filter((wave) => !done(wave));
  const legend = (["on", "attention", "off"] as const).map((key) => ({ key, count: open.filter((wave) => tone(wave) === key).length })).filter((item) => item.count > 0);
  return <div className="space-y-5">
    {legend.length > 0 ? <p aria-label="Auto-run legend" className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-muted">{legend.map((item) => <span key={item.key} className="inline-flex items-center gap-1.5"><Dot tone={item.key} />{item.count} {TONE_LABEL[item.key].toLowerCase()}</span>)}</p> : null}
    {groups.length === 0 ? <p className="text-[13px] text-muted">{waves.length ? "No waves match." : "No waves yet."}</p> : null}
    {groups.map((group) => <section key={group.title} aria-label={group.title}>
      <h2 className="mb-1.5 text-[12px] font-medium text-muted">{group.title} · {group.rows.length}</h2>
      <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{group.rows.map((wave) => {
        const chip = unusual(wave, backgroundWorkEnabled);
        const rowTone = tone(wave);
        return <li key={wave.id} className="min-w-0">
          <button type="button" onClick={() => onOpenWave(wave.id)} aria-label={`Open wave ${wave.title}, ${TONE_LABEL[rowTone].toLowerCase()}`} title={TONE_LABEL[rowTone]} className={`${cardClass(true)} flex h-full w-full flex-col gap-2 p-4`}>
            <span className="flex w-full items-center gap-2">
              <Dot tone={rowTone} />
              <span className="font-mono text-[11px] text-faint">{wave.id}</span>
              {chip ? <span title={chip.why} className="ml-auto rounded-full border border-warn/30 bg-warn-soft px-2 py-0.5 text-[11px] font-medium text-warn">{chip.label}</span> : null}
            </span>
            <span className={`line-clamp-2 text-[14px] font-semibold leading-5 ${rowTone === "done" ? "text-muted" : "text-ink"}`}>{wave.title}</span>
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
