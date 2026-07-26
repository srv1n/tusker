---
capsule:
  what: "Binding implementation contract for opt-in, model-free scheduled staging and full-green promotion, with human-facing briefs and bounded red-night triage."
  use_when:
    - "Work changes departure scheduling, wave promotion, singleton delivery, cross-project build arbitration, red-gate repair, release profiles, or delivery briefs."
  skip_when:
    - "The task changes an unrelated runner, tracker CRUD, or a repository that has not opted into scheduled promotion."
---

# Opt-in Scheduled Promotion

Status: accepted for task planning
Date: 2026-07-25
Supersedes as implementation direction: `docs/design/scheduled-integration-merge-trains-proposal.md`

## 1. Outcome

Tusker may continuously integrate objectively reviewed task work into an
isolated delivery branch and promote that delivery branch to the configured
default branch on a fixed schedule. Ordinary staging, gating, and promotion
are deterministic and model-free. A paid model may be dispatched only for an
ambiguous red-gate diagnosis after deterministic classification cannot assign
the failure to infrastructure, a known flaky test, or an isolated task.

The capability is opt-in per repository. Importing tasks, installing Tusker,
registering a project, enabling ordinary task automation, or running Serve
must not enable scheduled staging, promotion, model spend, or release.

## 2. Product invariants

### 2.1 Default off

When scheduled-promotion configuration is absent, malformed, or explicitly
disabled, Tusker preserves existing behavior:

- no timetable is evaluated;
- no Git fetch is initiated for departure planning;
- no departure record is created;
- no task is wrapped in an implicit delivery unit;
- no integration or default-branch ref moves;
- no paid model is launched for promotion work;
- no release command runs;
- the UI presents the feature as off, not as pending or degraded.

Configuration exposes monotone permission modes:

| Mode | Observe | Stage reviewed work | Promote default branch | Release |
| --- | --- | --- | --- | --- |
| `disabled` | No | No | No | No |
| `shadow` | Yes | No | No | No |
| `stage` | Yes | Yes | No | No |
| `promote` | Yes | Yes | Yes, after the configured full gate | No, unless separately authorized |

Release is independently opt-in through a named release profile. Selecting
`promote` never implies release permission. Model-assisted red triage is also
independently opt-in and defaults off.

### 2.2 No cadence gap

An existing repository-specific conductor remains authoritative while Tusker
runs in `shadow` mode. Cutover occurs only after representative departure
decisions match on candidate revisions, reasons, gate requirements, intended
main movement, and release eligibility. Elapsed time alone is not parity
evidence.

The shadow matrix must include:

- no cargo;
- eligible cargo;
- an operator hold;
- offline or failed fetch;
- main changed since the last successfully gated revision;
- daemon restart around a window;
- duplicate trigger;
- red gate;
- successful promotion.

### 2.3 Invisible singleton delivery

A finished standalone task reaches the configured staging and promotion path
without the operator creating, naming, adding, or arming a wave. Tusker may
represent this internally as a one-member wave or delivery unit, but normal
product surfaces say what will happen ("staged", "scheduled for promotion",
"landed") rather than requiring wave mechanics.

Automatic wrapping does not authorize new implementation work. It inherits the
task's existing execution provenance and the repository's explicit scheduled
promotion policy. Existing explicit multi-task waves keep their current
authorization and fingerprint rules.

### 2.4 Green default branch

Staging is where velocity accumulates; the default branch is where trust
accumulates. In `promote` mode, the default branch moves only after the exact
candidate revision passes the configured full promotion gate. A focused
staging gate must never be reported as full promotion proof.

The promotion operation binds:

- project and departure identity;
- scheduled window;
- task IDs and task `state_rev` values;
- task source SHAs;
- wave authorization fingerprint where applicable;
- integration base and candidate SHA;
- gate command, profile, toolchain, and tree fingerprint;
- expected default-branch SHA.

Every fact is revalidated immediately before the ref update. Drift causes a
recompute or a named refusal, never an optimistic push.

### 2.5 The human surface is a product deliverable

The morning surface is one screen in product language with three primary
lists:

1. **Landed last night**
2. **Blocked or repairing**
3. **Needs your decision**

It reuses `tusker.wave-brief/v1`, logbook, gate, release, and runtime facts
rather than creating another narrative record system. Artifact links and
technical detail remain available by drill-down.

Only three scheduled-promotion outcomes may produce a push notification:

- promotion red;
- release failed;
- spending or paid-launch hold activated.

Notifications are deduplicated per durable outcome and deep-link to the
relevant project brief. Green, empty, held-by-choice, and ordinary staging
departures remain quiet.

### 2.6 Smart red-night handling

Red promotion evidence is captured once as a structured failure packet:

- exact candidate SHA and tree hash;
- gate commands, profile, toolchain, host, and runner;
- complete structured defect set plus bounded actionable excerpts;
- last matching green gate;
- batch-bisection or task-isolation result;
- candidate tasks and touched paths;
- exact reproduction command;
- runtime artifact references;
- timeout, disk, OOM, network, runner-loss, and known-flake signals.

Deterministic classification runs first:

1. known infrastructure failure → retry or infrastructure repair;
2. known flake → apply the configured flake policy and rerun;
3. isolated task failure → return that task to rework;
4. ambiguous integration failure → optionally dispatch paid model triage.

Model triage runs only when explicitly enabled. It receives the structured
packet, attaches to a real Tusker repair/integrator task, never receives
secrets, and returns a structured classification, affected-task set, likely
root cause, reproduction, and repair acceptance contract.

### 2.7 Release is a separate authority boundary

Scheduled promotion does not execute arbitrary deploy shell strings. A release
uses a named profile that binds:

- an exact successfully promoted revision;
- authorized environment;
- credential provider;
- deterministic runner;
- idempotency key;
- rollback reference;
- explicit release hold and authorization policy;
- durable result.

Secrets are resolved inside the release runner and never embedded in task
records, departure records, logs, prompts, notifications, or UI payloads.

## 3. Architecture

```text
due window
  -> opt-in and hold check
  -> deterministic candidate snapshot
  -> no cargo: compact skip
  -> stage eligible reviewed work
  -> full promotion gate on frozen candidate
  -> green: CAS promote default branch
  -> optional authorized release profile
  -> red: structured failure packet
           -> deterministic classification
           -> optional paid triage only if ambiguous
```

### 3.1 Durable departure run

A departure is runtime work, not a fake task attempt. The runtime store owns a
first-class departure record with a uniqueness constraint over project,
departure policy, and scheduled window. It is restart-safe and idempotent.

Suggested states:

```text
due -> evaluating -> skipped|blocked|staging -> gating
    -> promoted -> releasing -> passed
    -> failed|repairing
```

State transitions record exact input and output revisions. Reconciliation
resumes safe incomplete states and refuses ambiguous ones.

### 3.2 Scheduler residence

The resident daemon owns the timetable. A second scheduler process is not
introduced. The scheduler wakes at the nearest due departure, coexists with
adaptive project reconciliation, and never scans opted-out repositories.

Misfire defaults:

- shadow/staging departure: `skip`;
- full promotion: `coalesce` at most one missed window when the current
  candidate has not been successfully gated;
- never replay every missed window.

### 3.3 Shared resources

Cross-project contention uses atomic runtime resource leases, not unrelated
per-repository lock files. A resource lease has an owner, purpose, heartbeat,
expiry, and recovery path. Example resources include local Cargo, Xcode,
ephemeral gate capacity, and a release environment.

### 3.4 Existing machinery to reuse

Implementation extends rather than duplicates:

- clock windows and unattended batch gates;
- `tusker land` staging worktrees, batch bisection, and landing audit;
- gate preflight and tree/command/profile ledger reuse;
- wave authorization and integration branches;
- wave brief and logbook projections;
- runtime attempts, leases, runner profiles, and escalations;
- Serve project operations and settings surfaces.

## 4. Operator surface

CLI target shape:

```text
tusker departure check --project <id> [--departure <id>] --json
tusker departure status --project <id> --json
tusker departure history --project <id> --limit <n> --json
tusker departure hold [--project <id>] [--release-only] --reason <text>
tusker departure resume [--project <id>] [--release-only] --by <actor>
```

`check` is read-only and model-free. It explains the resolved configuration,
candidate revisions, blockers, required resources, gate/release intent, and
whether execution is impossible because the feature is disabled.

Serve exposes:

- current opt-in mode and provenance;
- next scheduled departure;
- current hold and exact clearing action;
- last successfully gated/promoted/released revision;
- the morning brief;
- recent actionable departure outcomes;
- explicit controls that never enable release as a side effect of enabling
  shadow, staging, or promotion.

## 5. Agent guidance

The canonical Tusker skill and project canon must teach agents:

- scheduled promotion is daemon-owned and opt-in;
- interactive agents never start a daemon or scheduled departure;
- implementation and reviewer lanes never push the default branch;
- standalone tasks receive an implicit delivery unit automatically;
- `shadow` never mutates Git, tasks, gates, or external systems;
- staging proof and promotion proof are distinct;
- ordinary promotion is deterministic and model-free;
- red evidence is classified before paid triage;
- model triage works a real task and cannot deploy;
- release profiles own credentials and external authority;
- no task, skill install, project registration, or automation setting silently
  opts a repository into scheduled promotion.

## 6. Rollout

The executable DAG, authorization boundary, copy-paste agent prompts, and
operator launch controls are in
`docs/runbooks/execute-opt-in-scheduled-promotion-wave.md`.

1. Land the default-off policy and runtime schema.
2. Run the planner and morning brief in shadow mode.
3. Prove representative parity while the existing conductor remains active.
4. Enable deterministic staging.
5. Enable gated promotion after restart, duplicate-trigger, and red-gate
   failure injection pass.
6. Disable the old conductor only after Tusker is authoritative; retain it for
   one rollback window.
7. Add optional model triage and release profiles after deterministic
   promotion is stable.
8. Exercise multi-project resource arbitration before a second heavy project
   enables promotion.

## 7. Non-goals

- Scheduled promotion is not enabled during this delivery wave.
- Importing this plan does not arm or dispatch its tasks.
- This work does not migrate the rzn repository; it provides the generic
  shadow and cutover mechanisms required for a separate rzn-local migration
  task.
- Exact currency enforcement is not claimed until runners provide trustworthy
  billable usage. Launch and attempt quotas remain the honest hard limits.
- Generic scheduled promotion does not support standing self-merge permission.
  Any exception is one-shot, revision-bound, actor-bound, and expiring.

## 8. Accepted production operability requirements

A production merge-train incident exposed six control-plane gaps after the
original delivery wave was imported. They are binding requirements for a
follow-on V2 wave, not retroactive additions to `W-0001`, and do not opt any
repository into automation or promotion.

Five are factory-wide dependencies surfaced by scheduled promotion. They must
land as shared Tusker contracts rather than local train-only workarounds. The
boarding census and boarding receipt are scheduled-promotion contracts.

### 8.1 Typed human break-glass close

Independent review remains the normal objective close path. A broken reviewer
runner must not make a named human authority powerless. Tusker needs a
distinct break-glass close—not a generic `--force` flag—that requires:

- a locally accountable human actor with explicit authority;
- exact task ID, task state revision, implementation revision, and applicable
  gate/proof fingerprints;
- the exact review requirement being overridden, a concrete incident reason,
  and an optional authorization expiry;
- a permanent append-only receipt visible from task, wave, digest, and Serve;
  and
- an explicit downstream/integration risk projection so the override cannot
  masquerade as an independent-review pass.

Dispatched implementation and review workers cannot mint or consume this
authority by supplying an actor string. The authorization closes one exact
revision. It does not enable automation, move a ref, release, or create a
standing permission.

### 8.2 Runner health before claim

Tusker resolves and health-checks the exact configured runner executable,
effective PATH, version, permissions, and required command shape before it
creates a claim or attempt. A missing executable in the daemon environment is
an infrastructure blocker, not worker no-progress.

Alternate executable locations are explicit project or machine policy. Tusker
may explain a discovered alternative, but cannot silently replace the
authorized executable. Plan, queue, runs, wave brief, and Serve report
`infrastructure_blocked`, the failed executable/PATH source, and one bounded
operator remedy.

### 8.3 Installed Tusker capability manifest

Orchestrators query the installed binary instead of trusting documentation or
a dispatch sheet. A read-only `tusker capabilities --json` contract reports:

- binary version and build fingerprint;
- supported commands, subcommands, and relevant flags;
- supported task, delivery-plan, review, completion, and receipt schemas;
- runner adapters and capability-catalog schema;
- enabled or compiled optional capabilities; and
- deprecations or replacements for unavailable commands.

Documentation and skills describe intent. The installed capability manifest
is authoritative for what an orchestrator may invoke.

### 8.4 Canonical task registration across worktrees

A task created on a worker branch cannot remain invisible to the control plane
that must board it. Task admission from any linked worktree is mediated by one
canonical control writer: the resident daemon/shared control checkout when
automation is active, or the explicit interactive control path when it is not.

Tusker either writes the registration through to that canonical surface
atomically or refuses before creating a branch-local task record. Sweeping
arbitrary worktree Markdown is diagnostic fallback only and never silently
chooses between conflicting task identities. Pending registration and its
remedy are first-class state.

### 8.5 First-class boarding census and atomic boarding receipt

The resident daemon owns departure decisions. A host service manager may keep
the daemon alive, but does not implement repository scheduling; this avoids
platform privacy constraints such as macOS TCC turning a healthy schedule into
an invisible no-op.

Tusker exposes a stable, preferably wave-scoped boarding census JSON
projection. For each candidate it reports exact task binding, clean/frozen
revision, proof, review, gates, blockers, claim health, and readiness. The
deterministic integration handler atomically binds the accepted task
transition to the exact merge commit in one boarding/completion receipt.
Conductors and UIs consume this contract instead of reimplementing readiness
by scraping.

### 8.6 Waiting, parked, and stalled are different states

A parked attempt is not made healthy by emitting heartbeats forever. Tusker
distinguishes:

- a live worker with a renewable lease and heartbeat;
- an intentional wait with `next_wake_at`, wake source, and bounded deadline;
- an infrastructure-blocked terminal attempt with an operator remedy; and
- no-progress/stalled work that exceeded its progress deadline.

The resident daemon watches those deadlines. An overdue wait or missing live
heartbeat becomes a loud typed escalation in runs, queue, wave brief, digest,
and Serve. A terminal parked attempt cannot impersonate a healthy waiting
process.

### 8.7 Follow-on dependency order

The next V2 wave should land the work in this order:

1. installed capabilities plus runner pre-claim health (`8.2`, `8.3`);
2. typed run, wait, and escalation state (`8.6`);
3. canonical task registration (`8.4`);
4. boarding census and atomic receipt (`8.5`); and
5. human break-glass close (`8.1`) with independent security review.

This order removes invisible infrastructure failures first, then fixes
control-plane liveness and registration before changing close authority.

## Work streams

<!-- tusker:delivery-import:edb3afd7d44a6061:begin -->

- `[[ORC-T-0026]]` implements delivery source `actionable-notifications`.
- `[[ORC-T-0018]]` implements delivery source `daemon-timetable`.
- `[[ORC-T-0015]]` implements delivery source `departure-runtime-store`.
- `[[ORC-T-0020]]` implements delivery source `gated-staging-promotion`.
- `[[ORC-T-0019]]` implements delivery source `global-resource-leases`.
- `[[ORC-T-0017]]` implements delivery source `implicit-singleton`.
- `[[ORC-T-0024]]` implements delivery source `morning-brief-api`.
- `[[ORC-T-0014]]` implements delivery source `opt-in-policy`.
- `[[ORC-T-0022]]` implements delivery source `optional-model-triage`.
- `[[ORC-T-0021]]` implements delivery source `red-failure-packet`.
- `[[ORC-T-0023]]` implements delivery source `release-profiles`.
- `[[ORC-T-0025]]` implements delivery source `serve-ui`.
- `[[ORC-T-0028]]` implements delivery source `shadow-cutover-e2e`.
- `[[ORC-T-0016]]` implements delivery source `shadow-planner-cli`.
- `[[ORC-T-0027]]` implements delivery source `skill-agent-guidance`.

- `[[W-0001]]` is the imported delivery wave.

<!-- tusker:delivery-import:edb3afd7d44a6061:end -->
