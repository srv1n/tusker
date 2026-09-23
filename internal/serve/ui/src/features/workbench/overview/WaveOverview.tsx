import { useMemo } from "react";
import type { RunSummary, TaskCapsule, WaveSummary } from "@/types/domain";
import { cn } from "@/lib/cn";
import {
  filterGroupedWaves,
  groupWaves,
  type OverviewGroupId,
  type Startability,
} from "./groupWaves";
import "./overview.css";

export interface WaveOverviewProps {
  waves: WaveSummary[];
  tasks: TaskCapsule[];
  runs: RunSummary[];
  startability: Record<string, Startability>;
  descriptions?: Record<string, string>;
  projectName?: string;
  backgroundWorkEnabled?: boolean;
  query: string;
  category: WaveOverviewFilter;
  onQueryChange: (value: string) => void;
  onCategoryChange: (value: WaveOverviewFilter) => void;
  onOpenWave: (id: string) => void;
  onOpenUnassigned: () => void;
  loading?: boolean;
  error?: string;
}

export type WaveOverviewFilter = OverviewGroupId | "all";

const GROUP_TONE: Record<OverviewGroupId, string> = {
  "needs-you": "wux-ov-tone-warn",
  running: "wux-ov-tone-info",
  ready: "wux-ov-tone-pass",
  planned: "wux-ov-tone-neutral",
  completed: "wux-ov-tone-muted",
  unavailable: "wux-ov-tone-fail",
};

function progressText(
  entry: { progress: { total: number; done: number; moving: number; attention: number }; group: OverviewGroupId },
): string {
  const { total, done, moving, attention } = entry.progress;
  const noun = total === 1 ? "task" : "tasks";
  if (entry.group === "completed") return `${total} ${noun} in history`;
  const parts = [`${done} of ${total} ${noun} done`];
  if (moving > 0) parts.push(`${moving} moving`);
  if (attention > 0) parts.push(`${attention} need attention`);
  return parts.join(" · ");
}

/**
 * Grouped wave overview (spec section 5).
 *
 * One card per wave; no repeated ticket inventory. The whole card is the
 * navigation control. Descriptions render only from supplied authored data;
 * missing descriptions leave no filler.
 */
export function WaveOverview(props: WaveOverviewProps) {
  const groups = useMemo(
    () =>
      filterGroupedWaves(
        groupWaves({
          waves: props.waves,
          tasks: props.tasks,
          runs: props.runs,
          startability: props.startability,
        }).groups,
        props.descriptions,
        props.query,
        true,
      ),
    [props.waves, props.tasks, props.runs, props.startability, props.descriptions, props.query],
  );

  const unassignedCount = useMemo(() => {
    const membered = new Set<string>();
    for (const wave of props.waves) {
      for (const id of wave.memberIds) membered.add(id);
      for (const member of wave.members) membered.add(member.id);
    }
    return props.tasks.filter((task) => !membered.has(task.id)).length;
  }, [props.waves, props.tasks]);

  const visibleGroups = groups.filter((group) => props.category === "all" ? group.id !== "completed" : group.id === props.category);
  const visibleCount = visibleGroups.reduce((n, group) => n + group.waves.length, 0);
  const filterGroups = groups.filter((group) => group.waves.length > 0 || group.id === props.category);
  const resetFilters = () => {
    props.onCategoryChange("all");
    props.onQueryChange("");
  };

  if (props.loading) {
    return (
      <div className="wux-ov" aria-label="Loading wave overview">
        {[0, 1, 2].map((row) => (
          <div key={row} className="wux-ov-skeleton" />
        ))}
      </div>
    );
  }

  if (props.error) {
    return (
      <div className="wux-ov" role="alert">
        <div className="wux-ov-error">
          Wave overview unavailable. {props.error} Nothing below is inferred
          from partial data.
        </div>
      </div>
    );
  }

  return (
    <div className="wux-ov">
      <header className="mb-5 border-b border-line pb-4">
        <p className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">Project</p>
        <h1 className="mt-1 font-serif text-[26px] font-semibold text-ink">{props.projectName || "Project workspace"}</h1>
      </header>
      <div className="wux-ov-controls">
        <label className="wux-ov-search">
          <span className="wux-ov-search-label">Search waves</span>
          <input
            type="search"
            value={props.query}
            onChange={(event) => props.onQueryChange(event.target.value)}
            placeholder="Search ID, title, or description"
            aria-label="Search waves by ID, title, or description"
          />
        </label>
      </div>

      {unassignedCount > 0 ? (
        <div className="wux-ov-unassigned">
          <button type="button" onClick={props.onOpenUnassigned}>
            Unassigned tasks ({unassignedCount}): open board
          </button>
        </div>
      ) : null}

      {visibleCount === 0 ? (
        <div className="wux-ov-empty">
          <p className="wux-ov-empty-title">{props.category === "all" ? "No active waves match." : `No ${groups.find((group) => group.id === props.category)?.title.toLowerCase()} waves match.`}</p>
          <p className="wux-ov-empty-detail">
            {props.waves.length === 0
              ? "Authored waves will appear here before or after they are started."
              : "Adjust the search or reset the status filter."}
          </p>
          {props.waves.length > 0 ? <button type="button" onClick={resetFilters} className="mt-3 rounded-md border border-line px-3 py-2 text-[12px] font-medium text-ink">Clear filters</button> : null}
        </div>
      ) : null}

      <nav aria-label="Filter waves by status" className="mb-5 flex flex-wrap gap-2">
        <button type="button" aria-pressed={props.category === "all"} onClick={() => props.onCategoryChange("all")} className={cn("rounded-md border border-line px-3 py-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50", props.category === "all" && "bg-ink text-surface")}>All ({groups.filter((group) => group.id !== "completed").reduce((count, group) => count + group.waves.length, 0)})</button>
        {filterGroups.map((group) => <button key={group.id} type="button" aria-pressed={props.category === group.id} onClick={() => props.onCategoryChange(group.id)} className={cn("rounded-md border border-line px-3 py-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50", props.category === group.id && "bg-ink text-surface")}>{group.title} ({group.waves.length})</button>)}
      </nav>
      {visibleGroups.map((group) =>
        group.waves.length === 0 ? null : (
          <section key={group.id} aria-label={group.title} className="wux-ov-group">
            <div className="wux-ov-group-head">
              <h2>{group.title}</h2>
              <span className="wux-ov-count">{group.waves.length}</span>
            </div>
            <ul className="wux-ov-rows">
              {group.waves.map((entry) => {
                const description = props.descriptions?.[entry.wave.id];
                return (
                  <li key={entry.wave.id} className="wux-ov-row">
                    <button
                      type="button"
                      className="wux-ov-card"
                      onClick={() => props.onOpenWave(entry.wave.id)}
                      aria-label={`Open wave ${entry.wave.title}`}
                    >
                      <div className="wux-ov-main">
                        <span className="wux-ov-title">{entry.wave.title}</span>
                        <span className="wux-ov-id">{entry.wave.id}</span>
                        {description ? (
                          <p className="wux-ov-desc">{description}</p>
                        ) : null}
                        <p className="wux-ov-progress">{progressText(entry)}</p>
                        {entry.group !== "completed" && entry.wave.authorization.state === "armed" && props.backgroundWorkEnabled === false ? (
                          <p className="wux-ov-detail">Background work is off. Turn it on in Settings to dispatch this wave.</p>
                        ) : null}
                        {entry.group !== "completed" && entry.wave.recovery?.blockingCause && !(props.backgroundWorkEnabled === false && entry.wave.recovery.causeCode === "project_disabled") ? (
                          <p className="wux-ov-detail">{entry.wave.recovery.blockingCause}</p>
                        ) : null}
                        {entry.stateDetail ? (
                          <p className="wux-ov-detail">{entry.stateDetail}</p>
                        ) : null}
                      </div>
                      <div className="wux-ov-side">
                        {entry.group !== "completed" && (entry.wave.authorization.state === "armed" || entry.wave.authorization.state === "disarmed") ? <span className="wux-ov-state wux-ov-tone-neutral">{entry.wave.authorization.state === "armed" ? (entry.wave.recovery?.queued ? "Armed + Queued" : "Armed") : "Not armed"}</span> : null}
                        <span className={cn("wux-ov-state", GROUP_TONE[entry.group])}>
                          {entry.stateLabel}
                        </span>
                        {entry.runningAlso ? (
                          <span className="wux-ov-state wux-ov-tone-info">Running</span>
                        ) : null}
                      </div>
                    </button>
                  </li>
                );
              })}
            </ul>
          </section>
        ),
      )}
    </div>
  );
}
