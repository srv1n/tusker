/*
  Tusker Serve — domain model.

  These types mirror the tusker runtime store & vault as the serve JSON API is
  expected to expose it. The backend is still being built; where a shape is a
  best-guess ahead of the API, it is marked `// TODO(api)` and tracked in
  BACKEND-GAPS.md. Screens render against these types via the mock layer today
  and the real API later — the shapes should not change when we swap.
*/

// ----------------------------------------------------------------------------
// Shared enums / unions
// ----------------------------------------------------------------------------

export type Runner = "codex" | "claude";
export type Lane = "execute" | "review";

/** Task lifecycle, mirrors tusker task status. */
export type TaskStatus =
  | "backlog"
  | "ready"
  | "in_progress"
  | "review"
  | "blocked"
  | "done";

export type Readiness = "ready" | "blocked_dependency" | "blocked_gate" | "draft";
export type Priority = "p0" | "p1" | "p2" | "p3";
export type Risk = "low" | "medium" | "high" | "critical";

/** Per-acceptance-row proof state. */
export type ProofStatus = "pending" | "pass" | "fail";

/** The five human-gate kinds the needs-me queue routes (packet §4.1). */
export type GateKind =
  | "clarify"
  | "provision"
  | "approve-spec"
  | "review"
  | "failed";

/** Run liveness derived from time-since-last-event (packet §4.2). */
export type Liveness = "fresh" | "stale" | "dead";

export type RunOutcome =
  | "idle"
  | "running"
  | "stale"
  | "succeeded"
  | "failed"
  | "interrupted"
  | "released"
  | "terminal"
  | "retry-queued"
  | "parked-no-progress"
  | "parked-budget";

export type DocKind = "spec" | "decision" | "knowledge" | "task" | "epic" | "dashboard";

// ----------------------------------------------------------------------------
// Projects & navigation
// ----------------------------------------------------------------------------

export interface ProjectSummary {
  id: string;
  name: string;
  /** Items in this project waiting on the human. Drives the sidebar badge. */
  needsCount: number;
  /** Live agent activity: any active runs? drives the pulse dot. */
  activeRuns: number;
  /** Highest-severity liveness across this project's active runs. */
  worstLiveness: Liveness | null;
  daemonConnected: boolean;
}

// ----------------------------------------------------------------------------
// Needs-me queue
// ----------------------------------------------------------------------------

export interface NeedBase {
  id: string;
  kind: GateKind;
  projectId: string;
  projectName: string;
  taskId: string;
  taskTitle: string;
  /** How much downstream work this item blocks — primary ranking key. */
  blocking: number;
  priority: Priority;
  /** ISO timestamp the item entered the human's queue. */
  since: string;
}

export interface ClarifyNeed extends NeedBase {
  kind: "clarify";
  question: string;
}
export interface ProvisionNeed extends NeedBase {
  kind: "provision";
  /** Concrete ask, e.g. "set S3/R2 keys". */
  ask: string;
  /** Where the credential/material is expected. */
  path?: string;
}
export interface ApproveSpecNeed extends NeedBase {
  kind: "approve-spec";
  specTitle: string;
  specPath: string;
}
export interface ReviewNeed extends NeedBase {
  kind: "review";
  acceptance: AcceptanceRow[];
}
export interface FailedNeed extends NeedBase {
  kind: "failed";
  lastError: string;
  attempts: number;
}

export type NeedItem =
  | ClarifyNeed
  | ProvisionNeed
  | ApproveSpecNeed
  | ReviewNeed
  | FailedNeed;

// ----------------------------------------------------------------------------
// Runs
// ----------------------------------------------------------------------------

export interface TokenTotals {
  input: number;
  output: number;
}

export interface RunSummary {
  /** Runs are keyed by the task they execute. */
  taskId: string;
  taskTitle: string;
  projectId: string;
  runner: Runner;
  model: string;
  lane: Lane;
  leaseState: "held" | "released" | "expired" | "unclaimed";
  outcome: RunOutcome;
  /** Elapsed wall-clock seconds for the active/last attempt. */
  elapsedSec: number;
  /** Seconds since the last protocol event — feeds the liveness indicator. */
  sinceLastEventSec: number;
  liveness: Liveness;
  tokens: TokenTotals;
  attemptCount: number;
  terminal?: boolean;
  error?: string | null;
  lastHeartbeatAt?: string | null;
  nextWakeAt?: string | null;
}

export interface Attempt {
  n: number;
  outcome: RunOutcome;
  durationSec: number;
  tokens: TokenTotals;
  startedAt: string;
}

export interface RunEvent {
  ts: string;
  /** Protocol event kind (filtered, not raw JSONL). */
  kind: string;
  text: string;
  level?: "info" | "warn" | "error";
}

export interface RunDetail extends RunSummary {
  workspacePath: string;
  attempts: Attempt[];
  events: RunEvent[];
}

// ----------------------------------------------------------------------------
// Work: epics & task contracts
// ----------------------------------------------------------------------------

export interface EpicSummary {
  id: string;
  title: string;
  counts: Record<TaskStatus, number>;
}

export interface TaskCapsule {
  id: string;
  title: string;
  epicId: string;
  epicTitle: string;
  status: TaskStatus;
  readiness: Readiness;
  priority: Priority;
  risk: Risk;
  hasGate: boolean;
  updatedAt: string;
}

export interface AcceptanceRow {
  id: string;
  text: string;
  proof: ProofStatus;
}

export interface VerificationRow {
  id: string;
  command: string;
  result: "pass" | "fail" | "pending";
  detail?: string;
}

export interface EvidenceCard {
  id: string;
  label: string;
  kind: "file" | "log" | "image" | "link" | "diff";
  ref: string;
}

export interface TaskDetail extends TaskCapsule {
  /** Intent, rendered as prose (markdown). */
  intent: string;
  acceptance: AcceptanceRow[];
  nonGoals: string[];
  verification: VerificationRow[];
  evidence: EvidenceCard[];
  knowledgeDelta?: string;
  deps: Array<{ id: string; title: string; status: TaskStatus }>;
  gates: Array<{ id: string; kind: GateKind; owner: string }>;
  runHistory: RunSummary[];
}

// ----------------------------------------------------------------------------
// Docs / reader-editor
// ----------------------------------------------------------------------------

export interface DocOutlineEntry {
  level: 2 | 3;
  text: string;
  slug: string;
}

export interface DocMeta {
  path: string;
  title: string;
  kind: DocKind;
  updatedAt: string;
  /** Locked frontmatter facts, shown in the property panel (never raw YAML). */
  frontmatter: Array<{ key: string; value: string; locked: boolean }>;
}

export interface DocContent extends DocMeta {
  /** Round-tripped markdown source. */
  markdown: string;
  outline: DocOutlineEntry[];
  /** CAS token; a save carrying a stale rev is rejected (packet §4.6). */
  rev: string;
}

export interface DocListEntry {
  path: string;
  title: string;
  kind: DocKind;
  updatedAt: string;
}

// ----------------------------------------------------------------------------
// Daemon / global status
// ----------------------------------------------------------------------------

export interface DaemonStatus {
  connected: boolean;
  addr: string;
  activeRuns: number;
  queuedTasks: number;
  parkedBudgetRuns?: number;
  budgetCircuit?: {
    open: boolean;
    reason?: string;
    reset_at?: string;
    input_tokens?: number;
    output_tokens?: number;
    input_token_limit?: number;
    output_token_limit?: number;
  } | null;
  invariantCircuit?: {
    open: boolean;
    reason?: string;
    summary?: string;
    opened_at?: string;
    last_checked_at?: string;
    violations?: Array<{
      check: string;
      detail: string;
      project_id?: string;
      record_id?: string;
      item_id?: string;
      lane?: string;
      lease_state?: string;
      fields?: Record<string, unknown>;
    }>;
  } | null;
}
