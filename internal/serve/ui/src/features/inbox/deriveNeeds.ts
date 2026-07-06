/*
  "Needs me" is a DERIVED signal, never a hand-authored list.

  This is the executable form of the closed five-signal definition in
  docs/design/serve-ui-supplement.md §3. An item enters the panel iff at least
  one signal fires; everything else is an explicit non-signal and is dropped.
  A false "needs you" count trains the operator to ignore the panel, which
  defeats attention routing — so the guards below are as important as the signals.

  SIGNALS (any one qualifies):
    1. High/critical review   — status=review AND risk ∈ {high, critical}
                                 (the reviewer agent cannot auto-close these).
    2. Human-owned gate       — an unsatisfied gate whose owner is a human.
    3. Rework ping-pong       — a task bounced runner→reviewer→rework ≥ 2 times.
    4. Terminal run failure   — a run that exhausted retries with no lease to continue.
    5. Wave boundary          — the batch-review trigger, as ONE card.

  EXPLICIT NON-SIGNALS (never enter the panel):
    - low/medium tasks in review        → the reviewer agent's job
    - dependency-blocked tasks          → the dependency's job
    - capacity-blocked ready tasks      → the queue's job
    - deliberately parked/interrupted leases

  Signals 3 and 5 need fields the mock/daemon don't expose yet (a rework counter;
  a cohort-drain trigger). They are OMITTED here rather than faked — see
  internal/serve/ui/BACKEND-GAPS.md. This whole computation should move
  server-side (SRV-T-0002); the client should ultimately just render /api/needs.
*/

import type {
  AcceptanceRow,
  NeedItem,
  RunSummary,
  TaskCapsule,
  TaskDetail,
} from "@/types/domain";

export interface DeriveNeedsInput {
  /** Board-level task rows (status, risk) — signal 1. */
  capsules: TaskCapsule[];
  /** Full task records keyed by id (gates, acceptance) — signal 2 + payloads. */
  details: Record<string, TaskDetail>;
  /** Run summaries (outcome, attempts) — signal 4. */
  runs: RunSummary[];
  /** Single-project fixtures carry no per-project ownership; default here. */
  projectId?: string;
  projectName?: string;
}

/** A gate owner string like "human:sarav" is human-owned; "agent:reviewer" is not. */
function isHumanOwner(owner: string): boolean {
  return /^human\b/i.test(owner.trim());
}

/** How much downstream work a task blocks — the primary ranking key. */
function blockingCount(taskId: string, details: Record<string, TaskDetail>): number {
  let n = 0;
  for (const t of Object.values(details)) {
    if (t.deps?.some((d) => d.id === taskId)) n += 1;
  }
  return n;
}

function gateToNeed(
  gate: { id: string; kind: string; owner: string },
  cap: TaskCapsule,
  ctx: { projectId: string; projectName: string; blocking: number },
): NeedItem | null {
  const base = {
    id: `need-gate-${gate.id}`,
    projectId: ctx.projectId,
    projectName: ctx.projectName,
    taskId: cap.id,
    taskTitle: cap.title,
    blocking: ctx.blocking,
    priority: cap.priority,
    since: cap.updatedAt,
  };
  switch (gate.kind) {
    case "clarify":
      // TODO(api): gate payload must carry the actual question text.
      return { ...base, kind: "clarify", question: `Human input needed on ${cap.title}.` };
    case "provision":
      // TODO(api): gate payload must carry the concrete ask + material path.
      return { ...base, kind: "provision", ask: `Provisioning required for ${cap.title}.` };
    case "approve-spec":
      // TODO(api): gate payload must carry spec title + vault path.
      return { ...base, kind: "approve-spec", specTitle: cap.title, specPath: "" };
    default:
      // A gate whose kind is itself "review"/"failed" is unusual; ignore rather
      // than manufacture a need of the wrong shape.
      return null;
  }
}

export function deriveNeeds(input: DeriveNeedsInput): NeedItem[] {
  const projectId = input.projectId ?? "tusker";
  const projectName = input.projectName ?? "tusker";
  const needs: NeedItem[] = [];
  const capById = new Map(input.capsules.map((c) => [c.id, c]));

  for (const cap of input.capsules) {
    const detail = input.details[cap.id];
    const blocking = blockingCount(cap.id, input.details);

    // SIGNAL 1 — high/critical review. The reviewer agent cannot auto-close a
    // task at the top risk tiers; it routes to the human. `critical` is a real
    // tier in the Go schema (cmd/tusker/schema.go: low|medium|high|critical).
    if (cap.status === "review") {
      if (cap.risk === "high" || cap.risk === "critical") {
        const acceptance: AcceptanceRow[] = detail?.acceptance ?? [];
        needs.push({
          id: `need-review-${cap.id}`,
          kind: "review",
          projectId,
          projectName,
          taskId: cap.id,
          taskTitle: cap.title,
          blocking,
          priority: cap.priority,
          since: cap.updatedAt,
          acceptance,
        });
      }
      // NON-SIGNAL: low/medium review → the reviewer agent auto-closes it.
    }

    // SIGNAL 2 — human-owned, unsatisfied gate.
    for (const gate of detail?.gates ?? []) {
      if (!isHumanOwner(gate.owner)) continue; // agent-owned gate → non-signal
      const need = gateToNeed(gate, cap, { projectId, projectName, blocking });
      if (need) needs.push(need);
    }

    // NON-SIGNALS (explicit): dependency-blocked and capacity-blocked ready tasks
    // resolve themselves; they never enter the panel.
  }

  // SIGNAL 4 — terminal run failure (exhausted retries; not retry-queued/parked).
  for (const run of input.runs) {
    if (run.outcome !== "failed") continue; // interrupted/retry-queued → non-signal
    const cap = capById.get(run.taskId);
    needs.push({
      id: `need-failed-${run.taskId}`,
      kind: "failed",
      projectId,
      projectName,
      taskId: run.taskId,
      taskTitle: run.taskTitle,
      blocking: blockingCount(run.taskId, input.details),
      priority: cap?.priority ?? "p2",
      since: cap?.updatedAt ?? "",
      // TODO(api): the daemon must surface the terminal error text + attempt count.
      lastError: "Run exhausted its retry budget with no lease able to continue.",
      attempts: run.attemptCount,
    });
  }

  // Primary sort: how much each item blocks (desc), then priority.
  const prRank: Record<string, number> = { p0: 0, p1: 1, p2: 2, p3: 3 };
  return needs.sort(
    (a, b) => b.blocking - a.blocking || (prRank[a.priority] ?? 9) - (prRank[b.priority] ?? 9),
  );
}
