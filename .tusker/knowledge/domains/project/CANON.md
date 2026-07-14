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
updated_at: "2026-07-14T15:45:24Z"
state_rev: "sha256:7f205dbf3c917f14cf53b070a6338bea2529bdf579b76ad3dc38b8fe31097a10"
---

# Project Canon

## Current Truth

- V7 is the only product surface in this repository.
- Durable task status never uses `active`.
- Dispatchable task states are `ready` and `rework`.
- Runtime activity is represented by run leases, attempts, sessions, and workspaces.
- Execution has four disjoint roles: a human-opened interactive session executes directly with its own tools; the independently running resident daemon is the only background dispatcher; an implementation worker handles exactly one injected task/attempt; and an independent read-only reviewer verifies one handoff. Interactive sessions and dispatched workers never launch nested model runners or foreground daemons.
- Creating or grooming tasks is inert. Only a registered project with automation enabled, a resident daemon, and a ready/rework task with satisfied dependencies can produce a background implementation process.
- Direct interactive work does not require daemon enablement or a daemon lifecycle claim. It inspects any live automated owner before taking over the same tracked task, but unrelated disabled-daemon state never blocks implementation.
- Canonical task state owns runtime liveness: when a task is closed, cancelled, superseded, or moved back to backlog, every non-terminal runtime row for that task is retired with an actor/reason audit stamp.
- Abandoned work uses the dedicated discard ceremony: `cancelled` is a durable tombstone, active views hide it by default, open gates become obsolete, and task/attempt/evidence/event history remains addressable. Physical task deletion is not a normal lifecycle operation.
- Discard never weakens downstream contracts silently. Active dependents require an explicit choice to detach the discarded prerequisite or discard the complete downstream dependency closure.
- Every attempt-creating path uses the shared attempt-cap guard before dispatch. Fresh dispatch, failure retry, continuation retry, reclaim replacement, and redriven retry-queued dispatch all count against the active redrive window; reclaim-caused replacements are not free attempts.
- The resident daemon claims before spawning a worker. Dispatched workers verify injected ownership but never claim again; the runner harness owns session attachment, heartbeats, and normalized runtime outcomes, while the worker owns implementation, proof, review request, or a concrete blocker/gate.
- Runner exit classification is tracker-aware on every outcome write path, including daemon status observation and wrapper direct-store recording: exit code 0 with tracker state still in `ready` or `rework` is attempt outcome `early_exit`, not clean completion, and counts against the no-progress continuation cap.
- Codex exec owns the inner agent loop for the local Codex lane: Tusker launches one `codex exec --json` process per attempt, ingests `thread.started` and `turn.*` JSONL events, resumes later attempts with `codex exec resume <session-id>` when safe, and treats `max_turns` and budget as process governors.
- Automated review is independent, read-only, one review per implementation handoff, and capped at three review cycles per task before operator intervention.
- The resolved Codex runner policy is enforced with real `codex exec` CLI arguments. Full-access plus approval-free execution uses the CLI bypass flag; sandboxed profiles use the matching `--sandbox` mode. Tusker-specific environment variables are diagnostic metadata, not enforcement.
- Codex exec event-stream silence is not evidence of runner death while raw JSONL shows a command execution started and not completed; the heartbeat watchdog uses `codex.turn_timeout_ms` as the in-flight command cap and reserves the shorter idle heartbeat reap for true silence.
- Human-owned gates set `agent_action: stop_until_human_response` and `readiness: waiting_on_human`.
- Human gates are reserved for human capability/authority, genuinely unresolved contract intent, or explicitly subjective final-artifact acceptance. Approved task/spec decisions are already accepted; agents must not turn their implementation into new human approval questions. Risk controls proof depth, reviewer strength, and landing safeguards; independent reviewers may close objective work at every risk tier.
- Tags are projections; typed frontmatter is source of truth.
- Obsidian Bases and dashboards are generated views, not canonical state.
- Browser-backed ChatGPT work is a runner result source, not a direct state writer.
- Waves are first-class V7 batch records; membership is canonical on `kind: wave`, task `wave:` is a reconcile-maintained back-pointer, and wave `status` is derived from member task closure.
- Delivery plans are versioned proposals with an explicit stable scope and source-keyed tasks. Tusker validates their spec refs, acceptance/proof, artifacts, and dependency DAG, then atomically writes held task contracts and one wave with final IDs; plan/import remain inert and never authorize or dispatch work.
- Wave completion and execution authorization are separate. Completion remains derived from member closure; authorization is disarmed, armed, paused, or a derived stale projection. Read-only whole-wave preflight explains every blocker, including external dependencies and integration-base/runner/skill compatibility. Arm atomically binds authorization to the exact material spec, task, proof-policy, gate-authority, and dependency fingerprint while promoting only held members; that consent is the explicit human dispatch authorization for critical-risk members. Pause, stale authorization, and disarm stop future claims and retries without releasing or forging outcomes for live workers.
- An armed wave is durable desired state, not a one-shot queue entry. Every daemon poll reconstructs its runnable frontier from canonical tasks, dependency hardness, authorization fingerprint, and runtime leases; proof-green soft edges flow through review, while exhausted machine work and genuine human gates contain only their hard downstream closures and independent branches continue.
- Armed-wave modifying work is isolated and attributable, shares the wave concurrency ceiling across implementation and review, and lands through the serialized integration lane before reviewer close. Merged-state validation is reusable only for the same source, command, and toolchain fingerprint; red optimistic batches bisect so green members still land.
- Wave delivery uses one `tusker.wave-brief/v1` projection across CLI JSON and Serve/Mac surfaces. Its stable order is Outcome, See it, Landed, Rework/parked, Human action, Documentation; it exposes implementation, proof, review, landing, and documentation independently and links compact artifacts to acceptance IDs plus durable evidence records.
- Merge landing is wave-scoped: `tusker wave create` cuts `integration/W-####`, wave task worktrees branch from that integration branch as `task/<TASK-ID>`, `tusker land` serializes batch merges through a gated staging worktree, and completed waves land to the configured default branch as one merge commit.
- Terminal task state is monotone under merge and reconcile. A task in `done`, `cancelled`, or `superseded` may leave terminal state only through an explicit Tusker control operation that mints a fresh `state_rev`; stale branch content or stale object-rev repair must fail with a CAS conflict instead of certifying a non-terminal rewind.
- Project registration quarantine is a loader property: entry points that scan registered projects use the shared loader, which records failed enabled registrations as `health: error` with `last_error` and continues loading unrelated healthy projects.
- `tusker setup doctor` is the read-only onboarding diagnostic for registered vault/workflow drift, binary and generated-skill provenance, and offline ChatGPT handoff configuration. `setup repair` changes only deterministic local state, is idempotent, and never invents project routing, credentials, or browser workflow updates.
- `tusker delivery rollout doctor|repair` aggregates that same setup doctor once across registered projects, diagnoses the managed daemon once globally, and quarantines incompatible registrations without stopping compatible siblings. Repair is idempotent and allowlisted to generated Tusker skills, supported workflow/runner policy, objective close defaults, registration metadata, and the managed service; it never rewrites project knowledge, task contracts, secrets, repo instructions, handoff routing, or unrelated configuration.
- Repository validation is serialized across linked worktrees by one validation gate. Makefile Go build, vet, and test phases default to two Go scheduler threads and one package/test lane; the `cmd/tusker` TestMain also acquires the gate so raw focused tests cannot overlap a broad shared-state suite. Helper test re-execs inherit the held lease.

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
