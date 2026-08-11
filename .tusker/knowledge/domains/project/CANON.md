---
schema: "tusker.domain-canon/v7"
kind: "domain_canon"
id: "project/canon"
project: "tusker"
domain: "project"
title: "Project Canon"
status: "current"
summary: "Current durable rules for Tusker's own repository."
capsule:
  skip_when:
    - "You only need a task contract or runtime log."
  use_when:
    - "Changing dispatch states, proof policy, automation, or project knowledge rules."
  what: "Current durable project rules for Tusker V7 lifecycle, routing, and validation."
source_of_truth:
  - ".tusker/SKILL.md"
  - ".tusker/WORKFLOW.md"
  - "tusker.yaml"
created_at: "2026-06-04 00:00:00 +0000 UTC"
updated_at: "2026-08-11T05:40:00Z"
state_rev: "sha256:dc92b6e268ba6df9b4a1733ab928913177b975ee019380a074a82e75f04e7dab"
---

# Project Canon

## Current Truth

- V7 is the only product surface in this repository.
- Durable task status never uses `active`.
- Dispatchable task states are `ready` and `rework`.
- Runtime activity is represented by run leases, attempts, sessions, and workspaces.
- Execution has four disjoint roles: a human-opened interactive session owns one tracked task through `tusker work start` and executes directly with its own tools; the independently running resident daemon is the only background dispatcher; an implementation worker handles exactly one injected task/attempt; and an independent read-only reviewer verifies one handoff and submits only a typed verdict. Interactive sessions and dispatched workers never launch nested model runners or foreground daemons.
- Creating, reviewing, doctoring, dry-running, importing held delivery records, grooming tasks, and recording proof are inert. Delivery review reports `planValid`, `importReady`, and `startReady` independently. Only fingerprint-confirmed Start and resident-daemon claims consume unattended project, authorization, runner, workspace, integration, and daemon readiness.
- Direct interactive work does not require project automation, daemon liveness, wave authorization, automated critical-risk policy, or capacity. Its universal work session checks task status, dependencies, genuine human gates, owner/path conflicts, revision, and exact workspace safety; it grants no dispatch, arm, release, land, or spending authority.
- Canonical task state owns runtime liveness: when a task is closed, cancelled, superseded, or moved back to backlog, every non-terminal runtime row for that task is retired with an actor/reason audit stamp.
- Abandoned work uses the dedicated discard ceremony: `cancelled` is a durable tombstone, active views hide it by default, open gates become obsolete, and task/attempt/evidence/event history remains addressable. Physical task deletion is not a normal lifecycle operation.
- Discard never weakens downstream contracts silently. Active dependents require an explicit choice to detach the discarded prerequisite or discard the complete downstream dependency closure.
- Every attempt-creating path uses the shared attempt-cap guard before dispatch. Fresh dispatch, failure retry, continuation retry, reclaim replacement, and redriven retry-queued dispatch all count against the active redrive window; reclaim-caused replacements are not free attempts.
- The resident daemon claims before spawning a worker. Dispatched workers verify injected ownership but never claim again; the runner harness owns session attachment, heartbeats, and normalized runtime outcomes, while the worker owns implementation, proof, review request, or a concrete blocker/gate.
- Runner exit classification is tracker-aware on every outcome write path, including daemon status observation and wrapper direct-store recording: exit code 0 with tracker state still in `ready` or `rework` is attempt outcome `early_exit`, not clean completion, and counts against the no-progress continuation cap.
- Codex ACP is the primary local Codex lane. `tusker acp setup` seals an exact existing Node/adapter/Codex npm closure and writes ignored machine-local configuration; attempts launch that absolute verified argv directly, one fresh ACP process/session per attempt, with no npm/npx, shell, PATH lookup, automatic resume, or runtime download. `codex_exec` remains an explicitly selected emergency profile only, never an automatic fallback after possible prompt delivery; `codex_cloud` remains a separate remote lifecycle.
- Automated review is independent, read-only, one review per implementation handoff, and capped at three review cycles per task before operator intervention.
- The resolved Codex ACP profile is rebound at factory and wrapper boundaries to the exact attempt, lease generation, workspace, bundle receipt, auth-principal digest, model, effort, sandbox, and network policy. The permission broker grants only canonical workspace-confined, budget-authorized `allow_once` operations; full access is refused. ACP updates remain bounded observations and `delivery_unknown` cannot retry, resume, or fall back automatically.
- Codex exec event-stream silence is not evidence of runner death while raw JSONL shows a command execution started and not completed; the heartbeat watchdog uses `codex.turn_timeout_ms` as the in-flight command cap and reserves the shorter idle heartbeat reap for true silence.
- Execution observability has its canonical contract in `docs/specs/24-execution-observability.md` and operator/system reference in `docs/system/execution-observability.md`. Every direct invocation and wave-authorization generation receives an immutable execution root; display names, task/wave bindings, provider IDs, and agent types remain correlation fields. Provider observations are untrusted, replay-safe facts only. Binding is non-retroactive, and timeline recovery is authoritative-fetch based. ORC-T-0046 consumes the graph-specific Operations projection; ORC-T-0047 consumes the focused reliability fixture rather than duplicating either scope.
- Human-owned gates set `agent_action: stop_until_human_response` and `readiness: waiting_on_human`.
- Human gates are reserved for human capability/authority, genuinely unresolved contract intent, or explicitly subjective final-artifact acceptance. Approved task/spec decisions are already accepted; agents must not turn their implementation into new human approval questions. Risk controls proof depth, reviewer strength, and landing safeguards; independent reviewers may objectively accept every risk tier through a typed result, while deterministic Tusker handlers own integration and closure.
- Tags are projections; typed frontmatter is source of truth.
- Obsidian Bases and dashboards are generated views, not canonical state.
- Browser-backed ChatGPT work is a runner result source, not a direct state writer.
- Waves are first-class V7 batch records; membership is canonical on `kind: wave`, task `wave:` is a reconcile-maintained back-pointer, and wave `status` is derived from member task closure.
- Multi-unit planning and implementation default to a requirements-traceable `tusker.delivery-plan/v2` DAG even when the user does not name Tusker or a DAG. The agent asks for outcomes, acceptance, important tests, constraints, non-goals, and genuine decisions; Tusker owns IDs, dependency syntax, waves, runners, workspaces, retries, and frontiers. Context, plan, review, and held import remain inert. One human-attributable `tusker delivery start` action revalidates the exact plan/context and may arm only its resulting wave after project, daemon, runner, workspace, and integration preflight; it never enables infrastructure, release, spending, or unrelated work.
- Wave completion and execution authorization are separate. Completion remains derived from member closure; authorization is disarmed, armed, paused, or a derived stale projection. Read-only whole-wave preflight explains every blocker, including external dependencies and integration-base/runner/skill compatibility. Arm atomically binds authorization to the exact material spec, task, proof-policy, gate-authority, and dependency fingerprint while promoting only held members; that consent is the explicit human dispatch authorization for critical-risk members. Pause, stale authorization, and disarm stop future claims and retries without releasing or forging outcomes for live workers.
- An armed wave is durable desired state, not a one-shot queue entry. Every daemon poll reconstructs its runnable frontier from canonical tasks, dependency hardness, authorization fingerprint, and runtime leases; proof-green soft edges flow through review, while exhausted machine work and genuine human gates contain only their hard downstream closures and independent branches continue.
- Armed-wave modifying work is isolated and attributable, shares the wave concurrency ceiling across implementation and review, and reaches the serialized integration lane only through the deterministic completion handler after a typed review result. The reviewer never merges, lands, closes, or moves refs. Merged-state validation is reusable only for the same source, command, and toolchain fingerprint; red optimistic batches bisect so green members still land.
- Wave delivery uses one `tusker.wave-brief/v1` projection across CLI JSON and Serve/Mac surfaces. Its stable order is Outcome, See it, Landed, Rework/parked, Human action, Documentation; it exposes implementation, proof, review, landing, and documentation independently and links compact artifacts to acceptance IDs plus durable evidence records.
- Merge landing is wave-scoped: `tusker wave create` cuts `integration/W-####`, wave task worktrees branch from that integration branch as `task/<TASK-ID>`, `tusker land` serializes batch merges through a gated staging worktree, and completed waves land to the configured default branch as one merge commit.
- Terminal task state is monotone under merge and reconcile. A task in `done`, `cancelled`, or `superseded` may leave terminal state only through an explicit Tusker control operation that mints a fresh `state_rev`; stale branch content or stale object-rev repair must fail with a CAS conflict instead of certifying a non-terminal rewind.
- Project registration quarantine is a loader property: entry points that scan registered projects use the shared loader, which records failed enabled registrations as `health: error` with `last_error` and continues loading unrelated healthy projects.
- Canonical CLI writes are the primary state-change channel and notify the resident daemon through one common post-write boundary using the registered runtime project identity. Notifications debounce and reconcile one project; project-registry mutations trigger one explicit registry refresh. Timed reconciliation is only a bounded correctness/recovery fallback.
- Reconciliation cadence is independent per enabled project and resets on CLI mutation, Serve attention, or manual refresh: live runtime work stays at the 5-second safety floor, actively viewed projects use 20-second attention, then idle projects back off through 60 seconds, 5 minutes, 10 minutes, and 30 minutes. Daemon restart begins hot. The scheduler wakes at the nearest due time instead of scanning every project on a global tick.
- Daemon project polls load only operational records under `.tusker/work/**`. The shared mtime/size cache makes unchanged due polls stat-only with zero Markdown reads or YAML parses; full-vault knowledge/document loading remains exclusive to surfaces that require it.
- Project registration and dispatch authorization are separate. A registered enabled project may be observed with `automation.enabled: false`; fresh V7 initialization defaults automation off.
- The canonical reusable Agent Skills package lives at `skills/tusker/`, with `skill` retained only as a compatibility symlink for existing source-linked installs. Its two-field `SKILL.md` is a compact trigger-complete router to terminal PLAN, WORK, OPERATE, or rare one-hop guides. `assets/compatibility.yaml` and `tusker capabilities --json` own binary/workflow/factory/source/materialization compatibility; generated installs carry deterministic compatibility, canonical-payload, and installed-payload provenance. `.tusker/SKILL.md` remains a separate project-knowledge router.
- `tusker setup doctor` is the read-only onboarding diagnostic for registered vault/workflow drift, binary and generated-skill provenance, and offline ChatGPT handoff configuration. `setup repair` changes only deterministic local state, is idempotent, and never invents project routing, credentials, or browser workflow updates.
- Local Codex automation additionally requires `tusker capabilities --json` to advertise both `acp setup` and `codex_acp`. A missing capability means the installed binary predates the source contract; update the binary instead of substituting a legacy runner. `tusker acp doctor` verifies the sealed adapter separately from workflow configuration and authentication.
- `tusker delivery rollout doctor` reports core, interactive, automation, authorization, runtime, and optional-integration health independently; optional adapter drift cannot quarantine core planning or interactive work. Repair is idempotent and explicitly scoped to `core` (default), `automation`, `service`, or `integrations`; service repair may repair definitions but never starts/loads the service, and no scope enables automation, arms work, changes credentials, invokes a provider, moves refs, releases, or spends.
- Repository validation is serialized across linked worktrees by one validation gate. Makefile Go build, vet, and test phases default to two Go scheduler threads and one package/test lane; the `cmd/tusker` TestMain also acquires the gate so raw focused tests cannot overlap a broad shared-state suite. Helper test re-execs inherit the held lease.
- Owned paths are enforced at claim time across CLI and daemon claims under a cross-process claim lock. Healthy intersecting leases are reason-fully refused; expired holders are taken over only after process liveness fails, with an audit event.
- Successful run submission requires a structured end state. The harness, not model prose, owns branch, HEAD, worktree, and dirty facts; attempts and the generated stream board expose that record.
- `.tusker/dashboards/streams.md` and `tusker streams --json` are projections of runtime leases and recent successful attempts. Stale heartbeats are never presented as live lanes.
- Passing gates are reusable only by the exact tracked-tree hash, command, and feature profile. Scheduled batch gates run unattended, write that ledger on green, and create or update repair tasks on red without blocking a merge.
- Collision-prone namespace linting is opt-in repository policy. Integrator tasks are a typed work kind and exclusively claim configured shared namespaces; their packet composes dependency end states and a file-overlap audit.

## Invariants

- A ready/rework task must have concrete acceptance and verification proof.
- Placeholder acceptance must block dispatch.
- A task in canonical terminal/backlog state must not retain a non-terminal runtime row; the invariant circuit is last-resort containment for rows the close/status/reconcile retirement sweep failed to reach.
- A discarded task must retain its ID and history, record actor/time/reason metadata, and have no open gates. Any removed dependency edge must be attributable to an explicit discard resolution.
- A run at its attempt cap parks before any new attempt is created. `attempt_count_within_caps` is a corruption/operator-surgery sentinel, not a normal dispatch control path.
- Raw CLI output belongs in runtime scratch/logs, not task markdown.
- `tusker automation plan <task> --json` is the canonical pre-dispatch explanation.
- `tusker daemon run` and `tusker automation dispatch` fail closed when invoked from a detected Codex/Claude or dispatched-worker environment; a model session may inspect plans/status and manage records, but cannot recursively create model workers.
- `tusker xcode doctor` classifies generated Xcode build-state failures; when it reports `likely_infrastructure`, agents must record proof as blocked by infrastructure and do not claim code validation from the failed Xcode build.
- High and critical risk closeout requires stronger objective proof, not implicit human acceptance. Only explicit gates reserve human authority.
- Delivery briefs classify only valid open human-owned gates as Human action. Agent-capable review, test, log, benchmark, and artifact inspection failures remain in Rework/parked with the first actionable failure; raw logs, transcripts, and usage totals are not delivery artifacts.
- Legacy V5/V6 docs, publication manifests, site export state, and checked-in event history are not default read paths.
- Targeting a quarantined project may fail loudly, but unrelated quarantined registrations must not fatal daemon run/resume, status/list, automation, all-project listing, or serve paths.

## Verification

Use focused proof first:

```bash
go test ./cmd/tusker -run <test-name> -count=1
```

Use broad proof before closing cross-cutting changes:

```bash
make test
```
