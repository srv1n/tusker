import type { RunSummary, TaskCapsule, WaveSummary } from "@/types/domain";

// Grouped wave overview — pure grouping logic (no JSX).
//
// Implements spec section 5 ("Work overview / Grouping rules") as a total,
// nonduplicating partition. Deliberately stricter than the legacy
// DeliveryScreens helpers, which it must NOT inherit from:
//
// - Only a fresh held execution earns "Running". Durable task status
//   (in_progress/review) without a fresh run never implies liveness.
// - Only the authoritative `startability` record earns "Ready to start".
//   Authorization=armed, all-tasks-ready, or wave.status=open alone prove
//   nothing and never qualify a wave as ready.
// - Only an authoritative completion fact (landedAt or a terminal status)
//   earns "Completed". A drained task list (fullyDrained, every task done)
//   is not success; cancellation/supersession is history, not completion.
// - Stale/unknown observations stay visibly unavailable and never become
//   ready or completed.

export type StartabilityState = "ready" | "blocked" | "unknown";

export interface Startability {
  state: StartabilityState;
  reason?: string;
}

export type OverviewGroupId =
  | "needs-you"
  | "running"
  | "ready"
  | "planned"
  | "completed"
  | "unavailable";

export interface WaveProgress {
  total: number;
  done: number;
  moving: number;
  attention: number;
}

export interface GroupedWave {
  wave: WaveSummary;
  group: OverviewGroupId;
  /** One primary state for the row. */
  stateLabel: string;
  /** Compact secondary fact: startability reason, freshness loss, history note. */
  stateDetail?: string;
  /** Needs-you row that also has fresh execution; rendered as a secondary badge. */
  runningAlso: boolean;
  progress: WaveProgress;
}

export interface OverviewGroup {
  id: OverviewGroupId;
  title: string;
  waves: GroupedWave[];
}

export interface GroupWavesInput {
  waves: WaveSummary[];
  tasks: TaskCapsule[];
  runs: RunSummary[];
  startability: Record<string, Startability>;
}

export interface GroupWavesResult {
  groups: OverviewGroup[];
  unassignedCount: number;
  totalCount: number;
}

const GROUP_ORDER: OverviewGroupId[] = [
  "needs-you",
  "running",
  "ready",
  "planned",
  "completed",
  "unavailable",
];

export const GROUP_TITLES: Record<OverviewGroupId, string> = {
  "needs-you": "Needs you",
  running: "Running",
  ready: "Ready to start",
  planned: "Planned",
  completed: "Completed",
  unavailable: "Status unavailable",
};

const ACTIVE_LEASE_STATES = new Set(["claimed", "starting", "running"]);

/** A fresh held execution — the only evidence that earns "Running". */
export function isFreshActiveRun(run: RunSummary | undefined): boolean {
  if (!run || run.terminal || run.liveness !== "fresh") return false;
  return ACTIVE_LEASE_STATES.has(
    (run.leaseStateRaw ?? run.leaseState ?? "").toLowerCase(),
  );
}

const COMPLETED_STATUSES = new Set([
  "landed",
  "closed",
  "delivered",
  "completed",
]);

const HISTORY_STATUSES = new Set(["cancelled", "superseded"]);

/**
 * Authoritative completion only. `landedAt` or a terminal wave status.
 * fullyDrained and all-tasks-done are deliberately ignored: draining work
 * is not success, and cancellation is history, not completion.
 */
export function isAuthoritativelyCompleted(wave: WaveSummary): boolean {
  if (wave.landedAt) return true;
  const status = (wave.status ?? "").toLowerCase();
  return COMPLETED_STATUSES.has(status) || HISTORY_STATUSES.has(status);
}

export function isHistoryOnly(wave: WaveSummary): boolean {
  if (wave.landedAt) return false;
  return HISTORY_STATUSES.has((wave.status ?? "").toLowerCase());
}

/** Required reads failed or are stale: overall state cannot be determined. */
export function isWaveStale(wave: WaveSummary): boolean {
  return wave.authorization.stale || wave.authorization.state === "stale";
}

function memberIdsOf(wave: WaveSummary): Set<string> {
  return new Set([
    ...wave.memberIds,
    ...wave.members.map((member) => member.id),
  ]);
}

/** Fresh actionable human decision on this wave's members or brief. */
function hasFreshHumanNeed(
  wave: WaveSummary,
  memberTasks: TaskCapsule[],
): boolean {
  if (wave.brief.humanAction.length > 0) return true;
  return memberTasks.some(
    (task) => task.hasGate || (task.openGates?.length ?? 0) > 0,
  );
}

function hasFreshRun(
  memberTasks: TaskCapsule[],
  runsByTask: Map<string, RunSummary>,
): boolean {
  return memberTasks.some((task) => isFreshActiveRun(runsByTask.get(task.id)));
}

function progressOf(
  memberTasks: TaskCapsule[],
  runsByTask: Map<string, RunSummary>,
  attentionIds: Set<string>,
): WaveProgress {
  return {
    total: memberTasks.length,
    done: memberTasks.filter((task) => task.status === "done").length,
    moving: memberTasks.filter((task) =>
      isFreshActiveRun(runsByTask.get(task.id)),
    ).length,
    attention: memberTasks.filter(
      (task) =>
        attentionIds.has(task.id) ||
        task.hasGate ||
        (task.openGates?.length ?? 0) > 0,
    ).length,
  };
}

function plannedLabel(
  wave: WaveSummary,
  startability: Startability | undefined,
): { stateLabel: string; stateDetail?: string } {
  if (wave.authorization.state === "paused")
    return { stateLabel: "Paused", stateDetail: wave.authorization.action || undefined };
  if (wave.authorization.state === "disarmed")
    return {
      stateLabel: "Not authorized",
      stateDetail: wave.authorization.action || undefined,
    };
  if (startability?.state === "blocked")
    return { stateLabel: "Blocked", stateDetail: startability.reason };
  if (startability?.state === "unknown")
    return {
      stateLabel: "Planned",
      stateDetail: startability.reason
        ? `Start status unavailable: ${startability.reason}`
        : "Start status unavailable.",
    };
  if (wave.authorization.state === "armed")
    return { stateLabel: "Planned", stateDetail: "Authorized." };
  return { stateLabel: "Planned" };
}

function groupOneWave(
  wave: WaveSummary,
  memberTasks: TaskCapsule[],
  runsByTask: Map<string, RunSummary>,
  startability: Startability | undefined,
): GroupedWave {
  const attentionIds = new Set(
    wave.brief.humanAction.flatMap((item) => item.blockedTaskIds),
  );
  const progress = progressOf(memberTasks, runsByTask, attentionIds);
  const running = hasFreshRun(memberTasks, runsByTask);

  if (isAuthoritativelyCompleted(wave)) {
    const history = isHistoryOnly(wave);
    return {
      wave,
      group: "completed",
      stateLabel: history
        ? wave.status.charAt(0).toUpperCase() + wave.status.slice(1).toLowerCase()
        : "Completed",
      stateDetail: history ? "Ended without delivery." : undefined,
      runningAlso: false,
      progress,
    };
  }

  if (isWaveStale(wave)) {
    const need = hasFreshHumanNeed(wave, memberTasks);
    return {
      wave,
      group: "unavailable",
      stateLabel: "Status unavailable",
      stateDetail: need || running
        ? "Freshness lost for a needs-you or running fact."
        : "Required reads failed or are stale.",
      runningAlso: false,
      progress,
    };
  }

  const need = hasFreshHumanNeed(wave, memberTasks);
  if (need) {
    return {
      wave,
      group: "needs-you",
      stateLabel: "Needs you",
      stateDetail:
        startability?.state === "unknown"
          ? startability.reason
            ? `Start status unavailable: ${startability.reason}`
            : "Start status unavailable."
          : undefined,
      runningAlso: running,
      progress,
    };
  }

  if (running) {
    return {
      wave,
      group: "running",
      stateLabel: "Running",
      stateDetail:
        startability?.state === "unknown"
          ? startability.reason
            ? `Start status unavailable: ${startability.reason}`
            : "Start status unavailable."
          : undefined,
      runningAlso: false,
      progress,
    };
  }

  // Authoritative readiness only: armed/open/all-ready never imply runnable.
  if (startability?.state === "ready") {
    return {
      wave,
      group: "ready",
      stateLabel: "Ready to start",
      stateDetail: startability.reason,
      runningAlso: false,
      progress,
    };
  }

  const planned = plannedLabel(wave, startability);
  return {
    wave,
    group: "planned",
    stateLabel: planned.stateLabel,
    stateDetail: planned.stateDetail,
    runningAlso: false,
    progress,
  };
}

/**
 * Total, nonduplicating partition: every input wave appears in exactly one
 * group. Within a group, source order wins, then ID (stable until a
 * canonical timestamp exists).
 */
export function groupWaves(input: GroupWavesInput): GroupWavesResult {
  const tasksById = new Map(input.tasks.map((task) => [task.id, task]));
  const runsByTask = new Map(input.runs.map((run) => [run.taskId, run]));
  const allMemberIds = new Set<string>();

  const grouped = input.waves.map((wave, index) => {
    const ids = memberIdsOf(wave);
    for (const id of ids) allMemberIds.add(id);
    const memberTasks = [...ids]
      .map((id) => tasksById.get(id))
      .filter((task): task is TaskCapsule => task !== undefined);
    return { grouped: groupOneWave(wave, memberTasks, runsByTask, input.startability[wave.id]), index };
  });

  const byGroup = new Map<OverviewGroupId, Array<{ entry: GroupedWave; index: number }>>(
    GROUP_ORDER.map((id) => [id, []]),
  );
  for (const { grouped: entry, index } of grouped) {
    byGroup.get(entry.group)?.push({ entry, index });
  }
  // Stable tie-break: source order, then ID.
  const sorted = new Map<OverviewGroupId, GroupedWave[]>();
  for (const [id, list] of byGroup) {
    list.sort(
      (a, b) => a.index - b.index || a.entry.wave.id.localeCompare(b.entry.wave.id),
    );
    sorted.set(id, list.map((item) => item.entry));
  }

  const unassignedCount = input.tasks.filter(
    (task) => !allMemberIds.has(task.id),
  ).length;

  return {
    groups: GROUP_ORDER.map((id) => ({
      id,
      title: GROUP_TITLES[id],
      waves: sorted.get(id) ?? [],
    })),
    unassignedCount,
    totalCount: input.waves.length,
  };
}

/** Search matches title or supplied description; completed hides by default. */
export function filterGroupedWaves(
  groups: OverviewGroup[],
  descriptions: Record<string, string> | undefined,
  query: string,
  showCompleted: boolean,
): OverviewGroup[] {
  const needle = query.trim().toLowerCase();
  return groups
    .filter((group) => group.id !== "completed" || showCompleted)
    .map((group) => ({
      ...group,
      waves: group.waves.filter((entry) => {
        if (needle.length === 0) return true;
        const description = descriptions?.[entry.wave.id] ?? "";
        return (
          entry.wave.title.toLowerCase().includes(needle) ||
          description.toLowerCase().includes(needle)
        );
      }),
    }));
}
