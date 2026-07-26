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

/**
 * The run outcomes we style with a known hue/label. The trailing `(string & {})`
 * makes this an OPEN enum: the API may add outcomes (e.g. a review-complete /
 * awaiting-land state) that must still render — as a humanized label with a
 * neutral tone — rather than break a closed switch. Keep every consumer routing
 * through the tone helpers (`outcomeLabelOf` / `outcomeToneOf`) so a new value
 * never renders blank or throws.
 */
export type KnownRunOutcome =
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

export type RunOutcome = KnownRunOutcome | (string & {});

/** Lease state, likewise an open enum so new daemon lease states still display. */
export type LeaseState = "held" | "released" | "expired" | "unclaimed" | (string & {});

/** Result of POST /api/runs/:taskId/redrive — a redrive must never be silent. */
export interface RedriveResult {
  ok: boolean;
  refused?: boolean;
  requeued?: boolean;
  reason: string;
  taskId?: string;
  canonicalStatus?: string;
  leaseState?: string;
}

/** Result of POST /api/runs/:taskId/interrupt with canonical store readback. */
export interface InterruptResult {
  ok: boolean;
  refused?: boolean;
  interrupted: boolean;
  reason: string;
  taskId: string;
  leaseState?: LeaseState;
  leaseStateRaw?: string;
  processRunning: boolean;
}

export interface ActionIssue {
  code: string;
  message: string;
  path?: string | null;
  hint?: string | null;
  context?: unknown;
}

export interface ActionResult {
  ok: boolean;
  refused?: boolean;
  reason: string;
  command?: string;
  output?: string;
  issue?: ActionIssue;
  taskId?: string;
  gateId?: string;
  evidenceId?: string;
  feedbackPath?: string;
  canonicalStatus?: string;
  projectId?: string;
  discard?: DiscardImpact;
}

export interface DeliveryCrossScopeDependency {
  consumerTaskId?: string;
  consumerSourceKey: string;
  scope: string;
  sourceKey: string;
  taskId?: string;
  kind: "hard";
  persistedContractFingerprint?: string;
  contractProvenance: "persisted" | "prospective" | "missing" | "invalid";
  targetIntegrity: "resolved" | "missing" | "corrupt";
  producerState: string;
  producerLifecycle: "complete" | "incomplete" | "failed" | "unknown";
  blockerClass: "none" | "structural" | "lifecycle";
  satisfied: boolean;
  repair?: string;
  implication: string;
  taskHref?: string;
}

// Delivery intake is deliberately a product projection. These are the exact
// five CLI review sections, not a second client-side planner.
export interface DeliveryReview {
  schema: "tusker.delivery-review/v1";
  readOnly: true;
  ready: boolean;
  whatWillBeDelivered: Array<{ requirement: string; outcome: string; nonGoals: string[]; links: DeliveryReviewLink[] }>;
  howItWillBeProven: Array<{
    requirements: string[]; outcome: string; acceptance: string[]; tests: string[]; artifacts: string[];
    sourceKey: string; taskId?: string; taskHref?: string;
    checks: Array<{ covers: string; check: string; notes?: string; href?: string }>;
    artifactRefs: Array<{ kind: string; path: string; summary: string; acceptanceIds: string[]; href?: string }>;
    resourceRefs: string[];
  }>;
  howWorkFlows: {
    frontiers: string[][]; expectedConcurrency: number; integration: string;
    sharedResources: Array<{ sourceKey: string; kind: string; capacity?: number; capacityStatus: string; constraints: string[]; referencedBy: string[]; taskLinks: DeliveryReviewLink[] }>;
    crossScopeDependencies: DeliveryCrossScopeDependency[];
    warnings: string[]; waveId?: string; waveHref?: string;
  };
  whatNeedsYourDecision: Array<{
    title: string; action: string; why: string; sourceKey?: string; gateId?: string; gateHref?: string;
    taskSourceKey?: string; taskId?: string; acceptanceIds: string[]; verification?: string;
  }>;
  startBoundary: {
    planFingerprint: string; planIdentity?: string; contextFingerprint?: string; authorization: string; readiness: string;
    blockers: string[]; nextAction: string; state: DeliveryReviewState; stateLabel: string; actionHref?: string;
  };
  nonGoals: string[];
}

export interface DeliveryReviewLink { label: string; href: string }
export type DeliveryReviewState =
  | "held" | "invalid" | "changed" | "disabled" | "daemon-off" | "runner-blocked"
  | "shared-workspace" | "gated" | "armed" | "running" | "parked" | "completed";

export interface DeliveryStartResult {
  schema: "tusker.delivery-start/v1";
  waveId: string;
  planFingerprint: string;
  contextFingerprint: string;
  authorizationFingerprint: string;
  firstFrontier: string[];
  expectedConcurrency: number;
  integrationLane: string;
  statusLink: string;
  replayed: boolean;
  nextAction?: string;
}

export interface DeliveryErrorPayload {
  schema: "tusker.serve-delivery-error/v1";
  error: ActionIssue;
}

export interface DiscardDependent {
  id: string;
  title: string;
  status: string;
}

export interface DiscardImpact {
  taskId: string;
  title: string;
  status: string;
  directDependents: DiscardDependent[];
  cascadeDependents: DiscardDependent[];
  openGates: string[];
  requiresResolution: boolean;
  preservesHistory: boolean;
}

export type DocKind = "spec" | "decision" | "knowledge" | "task" | "epic" | "dashboard";

// ----------------------------------------------------------------------------
// Projects & navigation
// ----------------------------------------------------------------------------

export interface ProjectSummary {
  id: string;
  name: string;
  repoRoot: string;
  vaultRoot: string;
  automationEnabled: boolean;
  automationSource?: string;
  workspaceMode?: string;
  workspaceSource?: string;
  maxActiveRunsPerProject?: number;
  concurrencySource?: string;
  health: string;
  lastError?: string | null;
  /** Items in this project waiting on the human. Drives the sidebar badge. */
  needsCount: number;
  /** Live agent activity: any active runs? drives the pulse dot. */
  activeRuns: number;
  /** Highest-severity liveness across this project's active runs. */
  worstLiveness: Liveness | null;
  daemonConnected: boolean;
  /** Adaptive safety reconciliation; CLI/UI activity resets a project to hot. */
  reconciliation?: {
    tier: "" | "live" | "hot" | "warm" | "cool" | "cold";
    cadenceMs: number;
    lastActivityAt?: string;
    lastActivityReason?: string;
    lastPollAt?: string;
    nextDueAt?: string;
  };
}

export interface ProjectRegistrationResult extends ActionResult {
  projectId?: string;
}

// One versioned, read-only operations contract is shared by CLI, Serve, and
// the desktop shell. Keep these names aligned with factory_operations.go.
export interface FactoryOperationsProjection {
  schema: "tusker.factory-operations/v1";
  readOnly: true;
  generatedAt: string;
  project: {
    id: string;
    name: string;
    registered: boolean;
    enabled: boolean;
    health: string;
    automationEnabled: boolean;
    automationProvenance: string;
    dispatchScope: FactoryOperationsMode;
    completionMode: FactoryOperationsMode;
    promotionMode: {
      configured: boolean;
      mode: string;
      provenance: string;
      observe: boolean;
      stage: boolean;
      promote: boolean;
      release: boolean;
    };
  };
  authority: {
    defaultRef: string;
    defaultSha?: string;
    waves: FactoryOperationsWaveAuthority[];
  };
  capacity: {
    global: FactoryOperationsCapacityLimit;
    project: FactoryOperationsCapacityLimit;
    resourceHolds: FactoryOperationsResourceHold[];
  };
  sectionOrder: ["delivered", "workingNow", "reviewOrRework", "blocked", "needsYourDecision", "nextFrontier"];
  delivered: FactoryOperationsItem[];
  workingNow: FactoryOperationsItem[];
  reviewOrRework: FactoryOperationsItem[];
  blocked: FactoryOperationsItem[];
  needsYourDecision: FactoryOperationsDecision[];
  nextFrontier: FactoryOperationsItem[];
}

export interface FactoryOperationsMode {
  configured?: string;
  effective: string;
  provenance: string;
  warning?: string;
  repair?: string;
}

export interface FactoryOperationsWaveAuthority {
  waveId: string;
  title: string;
  state: string;
  fingerprintHealth: string;
  currentFingerprint?: string;
  authorizedFingerprint?: string;
  integrationRef: string;
  integrationSha?: string;
  safeAction: string;
  href: string;
}

export interface FactoryOperationsCapacityLimit {
  active: number;
  limit: number;
  available: number;
}

export interface FactoryOperationsResourceHold {
  name: string;
  purpose: string;
  projectId: string;
  taskId?: string;
}

export interface FactoryOperationsArtifact {
  taskId: string;
  taskHref: string;
  kind: string;
  priority: number;
  summary: string;
  acceptanceIds: string[];
  evidenceRef: string;
  artifactRef?: string;
  evidenceHref: string;
}

export interface FactoryOperationsItem {
  id: string;
  kind: string;
  taskId?: string;
  waveId?: string;
  title: string;
  state: string;
  productOutcome: string;
  cause?: string;
  affectedTaskIds: string[];
  automaticNextAction: string;
  safeAction: string;
  acceptedArtifacts: FactoryOperationsArtifact[];
  revisions: {
    stateRevision?: string;
    workRevision?: number;
    implementationSha?: string;
    resultRevision?: string;
    integrationRef?: string;
    integrationSha?: string;
    defaultRef?: string;
    defaultSha?: string;
  };
  href: string;
}

export interface FactoryOperationsDecision {
  gateId: string;
  owner: string;
  action: string;
  verification: string;
  whyHuman: string;
  affectedTaskIds: string[];
  automaticNextAction: string;
  safeAction: string;
  href: string;
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
  /** Present when this need is backed by a first-class human gate. */
  gateId?: string;
  blockedTaskIds?: string[];
  /** How much downstream work this item blocks — primary ranking key. */
  blocking: number;
  priority: Priority;
  /** ISO timestamp the item entered the human's queue. */
  since: string;
  humanAction?: HumanAction;
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

export interface RunSummary {
  /** Runs are keyed by the task they execute. */
  taskId: string;
  taskTitle: string;
  projectId: string;
  runner: Runner;
  model: string;
  lane: Lane;
  leaseState: LeaseState;
  /** Canonical runtime-store state, before the display lease is normalized. */
  leaseStateRaw?: string;
  /** Verified OS process identity, not inferred from the lease row. */
  processRunning?: boolean;
  /** True when the work was picked up by hand in a live session, not handed out by the daemon. */
  handRun?: boolean;
  outcome: RunOutcome;
  /** Elapsed wall-clock seconds for the active/last attempt. */
  elapsedSec: number;
  /** Seconds since the last protocol event — feeds the liveness indicator. */
  sinceLastEventSec: number;
  liveness: Liveness;
  attemptCount: number;
  terminal?: boolean;
  error?: string | null;
  lastHeartbeatAt?: string | null;
  nextWakeAt?: string | null;
  workspacePath?: string;
  workspaceMode?: string;
}

export interface Attempt {
  n: number;
  outcome: RunOutcome;
  durationSec: number;
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
  authorization?: { source: string; actor: string; trigger: string; project_automation_enabled: boolean; created_at: string };
  identity?: { repo_root: string; workspace_path: string; workspace_mode: string; runner: string; branch?: string; head?: string };
  session?: { session_ref: string; state: string; resumable: boolean; last_seen_at: string; last_error?: string };
  resume?: { supported: boolean; command?: string; reason?: string };
  delivery?: { summary?: string; verification?: string; proofStatus: string; artifact?: string };
}

// ----------------------------------------------------------------------------
// Work: epics & task contracts
// ----------------------------------------------------------------------------

export interface EpicSummary {
  id: string;
  title: string;
  counts: Record<TaskStatus, number>;
}

export interface WaveTaskSummary {
  id: string;
  title: string;
  group: string;
  status: string;
  proof: string;
}

export interface WaveSummary {
  id: string;
  title: string;
  status: string;
  landedAt?: string | null;
  memberIds: string[];
  members: WaveTaskSummary[];
  counts: Record<string, number>;
  authorization: { state: "disarmed" | "armed" | "paused" | "stale"; stale: boolean; action: string; actor?: string | null; at?: string | null };
  brief: WaveBrief;
}

export interface WaveTaskDeliveryState {
  taskId: string; title: string; taskHref: string;
  implementation: "absent" | "present";
  proof: string; review: string; landing: string; documentation: string;
  firstActionableFailure?: string;
}

export interface WaveArtifactCard {
  taskId: string; taskHref: string; kind: string; priority: number; summary: string;
  acceptanceIds: string[]; evidenceRef: string; artifactRef?: string; evidenceHref: string;
}

export interface WaveBrief {
  schema: "tusker.wave-brief/v1"; waveId: string; title: string; waveHref: string;
  sectionOrder: ["outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"];
  outcome: { summary: string; fullyDrained: boolean; counts: Record<string, number>; tasks: WaveTaskDeliveryState[] };
  seeIt: WaveArtifactCard[];
  landed: Array<{ taskId: string; title: string; taskHref: string; commit?: string; target?: string }>;
  reworkParked: Array<{ taskId: string; title: string; taskHref: string; state: string; firstActionableFailure: string; affectedTaskIds: string[] }>;
  humanAction: Array<{ gateId: string; gateHref: string; action: string; resumeId: string; blockedTaskIds: string[] }>;
  documentation: Array<{ taskId: string; taskHref: string; node: string; nodeHref: string; state: string }>;
}

export interface TaskCapsule {
  id: string;
  projectId?: string;
  title: string;
  epicId: string;
  epicTitle: string;
  status: TaskStatus;
  readiness: Readiness;
  priority: Priority;
  risk: Risk;
  hasGate: boolean;
  /** Open human-owned gates only; task status remains independent. */
  openGates?: GateDetail[];
  updatedAt: string;
  rawStatus?: string;
  rawReadiness?: string;
  /** Board-only projection; durable status remains in rawStatus. */
  liveRun?: boolean;
  latestAttemptOutcome?: RunOutcome;
  latestAttemptAt?: string | null;
}

export interface AcceptanceRow {
  id: string;
  text: string;
  proof: ProofStatus;
}

/** Server-derived contract for one open human-owned V7 gate. */
export interface HumanAction {
  kind: string;
  rawKind: string;
  title: string;
  action: string;
  whyAgentCannot: string;
  completionCondition: string;
  gateId: string;
  blockedTaskIds?: string[];
  covers: string[];
  acceptance: AcceptanceRow[];
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
  humanAction?: HumanAction;
  humanActions?: HumanAction[];
  runHistory: RunSummary[];
  runDirective?: {
    state: "queued" | "consumed" | "lapsed";
    actor: string;
    createdAt: string;
    expiresAt: string;
    reason?: string;
  };
}

export interface GateDetail {
  id: string;
  kind: GateKind;
  rawKind: string;
  title: string;
  status: string;
  owner: string;
  satisfied: boolean;
  blocking: boolean;
  blocks: string[];
  reason?: string;
  updatedAt?: string;
  question?: string | null;
  ask?: string | null;
  path?: string | null;
  specTitle?: string | null;
  specPath?: string | null;
  action?: string;
  whyAgentCannot?: string;
  completionCondition?: string;
  humanOwned?: boolean;
}

export interface EvidenceDoc {
  id: string;
  taskId: string;
  title: string;
  kind: string;
  status: string;
  covers: string[];
  artifactPaths: string[];
  createdBy: string;
  createdAt: string;
  acceptedBy?: string;
  acceptedAt?: string;
  summary?: string;
  relativePath: string;
}

export interface DecisionDoc {
  id: string;
  title: string;
  epicId: string;
  status: string;
  decision: string;
  decidedBy?: string;
  decidedAt?: string;
  workStreams: string[];
  relativePath: string;
}

export interface FeedbackDoc {
  id: string;
  date: string;
  actor: string;
  slug: string;
  relativePath: string;
  context: string;
  friction: string;
  productIdea: string;
  impact: string;
  related: string[];
  theme?: string;
  priorityHint?: string;
  affectedCommand?: string;
  fields: Record<string, string>;
}

export interface RunTurn {
  attempt_id: string;
  project_id: string;
  record_id: string;
  turn_id: string;
  turn_index: number;
  session_ref: string;
  status: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  lease_generation?: number;
  started_at: string;
  completed_at: string;
  last_event_at: string;
  last_error: string;
}

export interface AttemptDetail {
  id: string;
  taskId: string;
  projectId: string;
  runner: string;
  lane: string;
  outcome: RunOutcome;
  startedAt: string;
  finishedAt?: string;
  durationSec: number;
  workspacePath?: string;
  branchName?: string;
  pullRequestUrl?: string;
  promptPath?: string;
  eventSinkPath?: string;
  rawLogPath?: string;
  statusPath?: string;
  lastError?: string;
  logsSummary?: string;
  finalSummary?: string;
  turns: RunTurn[];
  events: RunEvent[];
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
  /** Structured frontmatter facts, shown as typed controls (never raw YAML). */
  frontmatter: Array<{ key: string; value: string; locked: boolean; lockReason?: string }>;
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

export interface DiskPressureConfig {
  enabled: boolean;
  min_free_bytes: number;
  min_free_percent: number;
  source: string;
}

export interface DiskPressureFilesystem {
  kind: string;
  path: string;
  filesystem_path: string;
  filesystem_id?: string;
  available_bytes: number;
  available_percent: number;
  total_bytes: number;
  effective_threshold_bytes: number;
  warning_threshold_bytes: number;
  state: string;
  checked_at?: string;
  error?: string;
}

export interface DiskPressureStatus {
  state: string;
  enabled: boolean;
  dispatch_paused: boolean;
  warning: boolean;
  recovered: boolean;
  checked_at?: string;
  reason?: string;
  min_free_bytes: number;
  min_free_percent: number;
  effective_threshold_bytes: number;
  warning_threshold_bytes: number;
  filesystems: DiskPressureFilesystem[];
  config: DiskPressureConfig;
}

export interface DaemonStatus {
  connected: boolean;
  addr: string;
  activeRuns: number;
  maxActiveRuns?: number;
  queuedTasks: number;
  daemonAlive?: boolean;
  daemonDownReason?: string | null;
  daemonPid?: number;
  daemonStartedAt?: string | null;
  daemonLastPollAt?: string | null;
  diskPressure?: DiskPressureStatus;
  persistentEscalationBanner?: boolean;
  crashLoop?: {
    open: boolean;
    reason?: string;
    summary?: string;
    opened_at?: string;
    last_checked_at?: string;
    last_restart_cause?: string;
    restart_count?: number;
    window_seconds?: number;
    burst?: number;
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
