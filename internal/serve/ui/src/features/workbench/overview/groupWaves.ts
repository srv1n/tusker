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
  /** Per-wave only; page-level error dedupe needs a backend-issued issue ID. */
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

/**
 * Stable row contract for the quiet-list consumer. This projection only ever
 * offers navigation: start/review require a separately supplied, authoritative
 * endpoint and must not be inferred from grouping.
 */
export interface OverviewRow {
  primaryAction: "open";
  secondary?: {
    kind: "human-action" | "blocker" | "running" | "outcome" | "unavailable" | "history";
    text: string;
    /** Unshortened source fact for detail/disclosure surfaces. */
    detail?: string;
  };
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
  row: OverviewRow;
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
  // This compatible ID contains successful completions and terminal history.
  // Naming the section History prevents cancelled work looking successful.
  completed: "History",
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

const HISTORY_STATUSES = new Set(["cancelled", "stopped", "superseded"]);
const PENDING_PROMOTION_STATUSES = new Map([
  ["pending_promotion", "Pending promotion"],
  ["promotion_pending", "Pending promotion"],
]);
// `fullyDrained` never substitutes for one of these structured closeout states.

/**
 * Authoritative completion only. `landedAt` or a terminal wave status.
 * fullyDrained and all-tasks-done are deliberately ignored: draining work
 * is not success, and cancellation is history, not completion.
 */
export function isAuthoritativelyCompleted(wave: WaveSummary): boolean {
  if (wave.landedAt) return true;
  const status = (wave.status ?? "").toLowerCase();
  return COMPLETED_STATUSES.has(status);
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

/** First non-empty authored source string. */
function firstText(...values: Array<string | null | undefined>): string | undefined {
  return values.find((value) => value?.trim())?.trim();
}

/** Fresh actionable human decision on this wave's members or brief. */
function hasFreshHumanNeed(
  wave: WaveSummary,
  memberTasks: TaskCapsule[],
): string | undefined {
  const briefAction = wave.brief.humanAction
    .map((item) => firstText(item.action))
    .find(Boolean);
  if (briefAction) return briefAction;

  for (const task of memberTasks) {
    for (const gate of task.openGates ?? []) {
      const gateAction = firstText(
        gate.action,
        gate.ask,
        gate.question ?? undefined,
        gate.reason,
        gate.whyAgentCannot,
      );
      if (gateAction) return gateAction;
    }
  }

  // `hasGate` remains an authoritative gate fact even where this projection
  // lacks the gate detail. Do not turn absent startability into a human gate.
  return wave.brief.humanAction.length > 0 || memberTasks.some(
    (task) => task.hasGate || (task.openGates?.length ?? 0) > 0,
  ) ? "Human action required." : undefined;
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

function row(secondary?: OverviewRow["secondary"]): OverviewRow {
  return { primaryAction: "open", secondary };
}

function runningText(progress: WaveProgress): string {
  return `${progress.moving} ${progress.moving === 1 ? "task" : "tasks"} running.`;
}

function authoredOutcome(wave: WaveSummary): string | undefined {
  return firstText(wave.expectedOutcome, wave.brief.expectedOutcome);
}

function unavailableDetail(startability: Startability | undefined): string {
  return startability?.reason
    ? `Start status unavailable: ${startability.reason}`
    : "Authoritative start status is unavailable.";
}

function isStartabilityUnknown(startability: Startability | undefined): boolean {
  return !startability || startability.state === "unknown";
}

function plannedLabel(
  wave: WaveSummary,
  startability: Startability | undefined,
): { stateLabel: string; stateDetail?: string } {
  if (wave.authorization.state === "paused")
    return { stateLabel: "Paused", stateDetail: wave.authorization.action || undefined };
  if (startability?.state === "blocked")
    return { stateLabel: "Blocked", stateDetail: startability.reason };
  if (wave.authorization.state === "disarmed")
    return {
      stateLabel: "Not started",
      stateDetail: wave.authorization.action || undefined,
    };
  if (startability?.state === "unknown")
    return {
      stateLabel: "Planned",
      stateDetail: unavailableDetail(startability),
    };
  if (wave.authorization.state === "armed")
    return { stateLabel: "Planned", stateDetail: "Authorized." };
  return { stateLabel: "Planned" };
}

function pendingPromotionLabel(wave: WaveSummary): string | undefined {
  return PENDING_PROMOTION_STATUSES.get((wave.status ?? "").toLowerCase());
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

  if (isAuthoritativelyCompleted(wave) || isHistoryOnly(wave)) {
    const history = isHistoryOnly(wave);
    const historyDetail = history ? "Ended without delivery." : authoredOutcome(wave);
    return {
      wave,
      group: "completed",
      stateLabel: history
        ? wave.status.charAt(0).toUpperCase() + wave.status.slice(1).toLowerCase()
        : "Completed",
      stateDetail: history ? historyDetail : undefined,
      runningAlso: false,
      progress,
      row: row(historyDetail ? {
        kind: history ? "history" : "outcome",
        text: historyDetail,
        detail: historyDetail,
      } : undefined),
    };
  }

  if (isWaveStale(wave)) {
    const need = hasFreshHumanNeed(wave, memberTasks);
    const detail = need || running
      ? "Freshness lost for a needs-you or running fact."
      : "Required reads failed or are stale.";
    return {
      wave,
      group: "unavailable",
      stateLabel: "Status unavailable",
      stateDetail: detail,
      runningAlso: false,
      progress,
      row: row({ kind: "unavailable", text: detail, detail }),
    };
  }

  const need = hasFreshHumanNeed(wave, memberTasks);
  if (need) {
    const detail = running ? `${need} Running.` : need;
    return {
      wave,
      group: "needs-you",
      stateLabel: "Needs you",
      stateDetail: detail,
      runningAlso: running,
      progress,
      row: row({ kind: "human-action", text: detail, detail: need }),
    };
  }

  if (running) {
    const detail = isStartabilityUnknown(startability)
      ? `${runningText(progress)} ${unavailableDetail(startability)}`
      : runningText(progress);
    return {
      wave,
      group: "running",
      stateLabel: "Running",
      stateDetail: detail,
      runningAlso: false,
      progress,
      row: row({ kind: "running", text: detail, detail }),
    };
  }

  const pendingPromotion = pendingPromotionLabel(wave);
  if (pendingPromotion) {
    return {
      wave,
      group: "planned",
      stateLabel: pendingPromotion,
      stateDetail: undefined,
      runningAlso: false,
      progress,
      row: row({ kind: "blocker", text: pendingPromotion }),
    };
  }

  // Authoritative readiness only: armed/open/all-ready never imply runnable.
  if (startability?.state === "ready") {
    const outcome = authoredOutcome(wave);
    return {
      wave,
      group: "ready",
      stateLabel: "Ready to start",
      stateDetail: startability.reason,
      runningAlso: false,
      progress,
      row: row(outcome ? { kind: "outcome", text: outcome, detail: outcome } : undefined),
    };
  }

  const planned = plannedLabel(wave, startability);
  if (
    isStartabilityUnknown(startability) &&
    wave.authorization.state !== "paused" &&
    wave.authorization.state !== "disarmed"
  ) {
    const detail = unavailableDetail(startability);
    return {
      wave,
      group: "unavailable",
      stateLabel: "Start status unavailable",
      stateDetail: detail,
      runningAlso: false,
      progress,
      row: row({ kind: "unavailable", text: detail, detail }),
    };
  }
  const outcome = authoredOutcome(wave);
  return {
    wave,
    group: "planned",
    stateLabel: planned.stateLabel,
    stateDetail: planned.stateDetail,
    runningAlso: false,
    progress,
    row: row(planned.stateDetail
      ? { kind: "blocker", text: planned.stateDetail, detail: planned.stateDetail }
      : outcome ? { kind: "outcome", text: outcome, detail: outcome } : undefined),
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
