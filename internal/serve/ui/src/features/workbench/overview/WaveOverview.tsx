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
  query: string;
  showCompleted: boolean;
  onQueryChange: (value: string) => void;
  onShowCompletedChange: (value: boolean) => void;
  onOpenWave: (id: string) => void;
  onOpenUnassigned: () => void;
  loading?: boolean;
  error?: string;
}

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
 * One row per wave; no repeated ticket inventory. The title itself is the
 * navigation control — there is no separate Open column. Descriptions render
 * only from supplied authored data; missing descriptions leave no filler.
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
        props.showCompleted,
      ),
    [props.waves, props.tasks, props.runs, props.startability, props.descriptions, props.query, props.showCompleted],
  );

  const unassignedCount = useMemo(() => {
    const membered = new Set<string>();
    for (const wave of props.waves) {
      for (const id of wave.memberIds) membered.add(id);
      for (const member of wave.members) membered.add(member.id);
    }
    return props.tasks.filter((task) => !membered.has(task.id)).length;
  }, [props.waves, props.tasks]);

  const completedCount = useMemo(
    () =>
      groupWaves({
        waves: props.waves,
        tasks: props.tasks,
        runs: props.runs,
        startability: props.startability,
      }).groups.find((group) => group.id === "completed")?.waves.length ?? 0,
    [props.waves, props.tasks, props.runs, props.startability],
  );

  const visibleCount = groups.reduce((n, group) => n + group.waves.length, 0);

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
      <div className="wux-ov-controls">
        <label className="wux-ov-search">
          <span className="wux-ov-search-label">Search waves</span>
          <input
            type="search"
            value={props.query}
            onChange={(event) => props.onQueryChange(event.target.value)}
            placeholder="Search title or description"
            aria-label="Search waves by title or description"
          />
        </label>
        <label className="wux-ov-check">
          <input
            type="checkbox"
            checked={props.showCompleted}
            onChange={(event) => props.onShowCompletedChange(event.target.checked)}
          />
          Show completed{completedCount > 0 ? ` (${completedCount})` : ""}
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
          <p className="wux-ov-empty-title">No waves match.</p>
          <p className="wux-ov-empty-detail">
            {props.waves.length === 0
              ? "Reviewed plans will appear here once they are authorized as delivery boundaries."
              : "Adjust the search or show completed waves."}
          </p>
        </div>
      ) : null}

      {groups.map((group) =>
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
                    <div className="wux-ov-main">
                      <button
                        type="button"
                        className="wux-ov-title"
                        onClick={() => props.onOpenWave(entry.wave.id)}
                        aria-label={`Open wave ${entry.wave.title}`}
                      >
                        {entry.wave.title}
                      </button>
                      {description ? (
                        <p className="wux-ov-desc">{description}</p>
                      ) : null}
                      <p className="wux-ov-progress">{progressText(entry)}</p>
                      {entry.stateDetail ? (
                        <p className="wux-ov-detail">{entry.stateDetail}</p>
                      ) : null}
                    </div>
                    <div className="wux-ov-side">
                      <span className={cn("wux-ov-state", GROUP_TONE[entry.group])}>
                        {entry.stateLabel}
                      </span>
                      {entry.runningAlso ? (
                        <span className="wux-ov-state wux-ov-tone-info">Running</span>
                      ) : null}
                    </div>
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
