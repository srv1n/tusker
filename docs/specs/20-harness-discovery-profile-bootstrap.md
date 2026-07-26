---
capsule:
  what: "Bootstrap Tusker's provider-neutral harness catalog, semantic runner profiles, V2 authoring path, and low-tier dogfood proof."
  use_when:
    - "Implementing the smallest safe slice needed before Tusker can orchestrate its own follow-on work."
  skip_when:
    - "Enabling automation, starting a daemon, arming a wave, dispatching paid work, releasing, or promoting main."
---

# Harness discovery and profile bootstrap

Status: implementation contract
Date: 2026-07-26

## Product outcome

A freshly initialized or existing Tusker repository can discover the model and
effort combinations exposed by installed local harnesses, generate a small set
of editable semantic runner profiles, preview how a task would be routed, and
author a valid V2 delivery DAG without hand-writing Tusker internals.

This slice makes the factory *ready to opt into*. It does not opt a repository
into automation, start a daemon, arm a wave, dispatch a model, move a ref,
release, spend, or configure credentials.

## Requirements

| ID | Outcome |
| --- | --- |
| R1 | `tusker runner catalog --json` returns a provider-neutral, machine-readable capability catalog with harness, model, supported effort, service-tier (when known), visibility, default, discovery source, confidence, command version, and observation time. |
| R2 | Codex discovery uses the installed CLI's live catalog when available and has an explicit bundled/offline fallback; Claude reports only documented aliases/efforts as declared capabilities because its CLI has no model-list API. The output never pretends both sources have equal certainty. |
| R3 | Fresh initialization and an explicit reconcile/preview command can produce a small semantic role set instead of a model × effort Cartesian explosion. Existing explicit profiles are preserved. Automation remains disabled before and after. |
| R4 | Runner validation accepts the effort levels actually exposed by supported harnesses, and routing can use a model-neutral task complexity (`routine`, `standard`, `complex`, `frontier`) without embedding a provider model in task contracts. |
| R5 | `tusker delivery plan` emits `tusker.delivery-plan/v2`, source keys, requirement placeholders, acceptance, verification, artifacts, owned paths, and dependency examples that pass the V2 parser once filled. Installed Tusker guidance tells planning agents to use that path by default for multi-unit work. |
| R6 | A read-only route preview explains the selected semantic role/profile and exact precedence without claiming or dispatching a task. |
| R7 | A disposable repository proves initialization, catalog inspection, profile generation, V2 DAG doctor/review/dry-run, and route preview with automation still off. Any actual resident-daemon launch remains a separate human/operator action. |

## Product decisions

### Capability catalog

- The catalog is a runtime, machine-local observation. It is not committed to a
  project and is never treated as permanent provider truth.
- Codex is `live` when the installed CLI returns its catalog. An explicitly
  requested bundled fallback is `bundled`, with lower freshness.
- Claude is `declared` until its CLI exposes a machine-readable catalog.
- Discovery failures are represented per harness. One missing harness does not
  erase healthy results from another.
- Catalog inspection is read-only and must not authenticate, install, mutate
  configuration, or launch a model session.

### Semantic profiles

Generate only these roles when the underlying capability exists:

1. `planner`
2. `execute-fast`
3. `execute-standard`
4. `execute-complex`
5. `execute-frontier`
6. `review-independent`
7. `repair-complex`

Profiles are project policy, not provider inventory. The generator chooses
balanced defaults from discovered capabilities, records the chosen harness,
model, and effort in ordinary editable `tusker.yaml`, and preserves user-owned
profiles on reconcile. It must not automatically choose the cheapest model for
all work or the frontier model for all work.

The first dogfood proof deliberately selects lower-tier execution defaults.
Escalation to a frontier profile is explicit policy and not exercised here.

### Routing

Task contracts may declare `complexity: routine|standard|complex|frontier`.
Routing precedence remains explicit task profile override, project routing
rule, lane role, then project default. Complexity maps to a semantic role; the
role maps to a configured profile. Preview must show every step and source.

### V2 authoring

Planning agents write one V2 delivery plan with source-key dependencies. Tusker
allocates final task and wave identities on inert import. Epics describe
outcomes, waves bind execution authority, and the dependency graph unlocks the
next frontier. A lone task is simply a wave of one.

## Acceptance boundaries

- No command in this work enables project automation, enables a registry
  project, arms a wave, starts/installs a daemon, dispatches a worker, changes
  release/promotion policy, moves `main`, pushes, spends, or configures secrets.
- Existing `tusker.yaml` profiles and routing rules are preserved unless the
  user explicitly requests replacement.
- Generated defaults are deterministic for the same capability snapshot and
  explain why each model/effort was selected.
- The CLI contract is the source of truth. Serve UI consumes it in a subsequent
  V2 delivery plan dogfooded through this bootstrap.

## Verification

Focused Go tests cover catalog parsing/fallback, missing harness isolation,
profile generation/preservation, effort and complexity validation, routing
precedence/preview, V2 scaffold shape, and automation-off invariants.

The disposable-repository proof records:

1. automation off before initialization/reconcile;
2. discovered catalog provenance;
3. generated semantic profiles;
4. a four-frontier V2 DAG (`hello` and `goodbye` in parallel, router, docs and
   end-to-end proof in parallel, final integration gate);
5. doctor and import dry-run success, plus a complete product review whose
   Start boundary is correctly blocked because automation remains off;
6. route previews selecting lower-tier execution profiles; and
7. automation still off, no daemon/service, no runs, and no ref movement.

## Accepted production feedback for the next factory wave

The following requirements came from a real merge-train incident. They are
accepted inputs to the next V2 delivery plan, but they do **not** widen this
bootstrap implementation. This cut establishes capability discovery, profiles,
V2 authoring, route preview, and automation-off dogfood. The next wave consumes
those contracts to harden factory operations.

### F1 — Typed human break-glass close

An objective independent-review gate remains the normal close path. A broken
review runner must not make a named human authority powerless, though. Tusker
needs a distinct break-glass close—not a generic `--force` flag—with:

- a locally accountable human actor and explicit authority;
- exact task ID, work revision, implementation revision, and gate/proof
  fingerprints;
- the review requirement being overridden, a concrete incident reason, and an
  optional expiry on the authorization;
- a permanent append-only audit receipt visible in task, wave, digest, and
  Serve surfaces; and
- explicit downstream/integration risk projection so the override cannot
  masquerade as an independent reviewer pass.

Dispatched implementation and review workers cannot mint or consume this
authority by forging an actor string. Break-glass closes one exact revision; it
does not enable automation, move a ref, release, or create standing permission.

### F2 — Runner health before claim

Tusker must resolve and health-check the exact configured runner executable,
effective PATH, version, permissions, and required command shape before it
creates a claim or attempt. A configured executable that is absent from the
daemon's environment is an infrastructure blocker, not worker no-progress.

Allowed alternate executable paths must be explicit project or machine policy;
Tusker may explain a discovered alternative but must not silently switch the
authorized executable. Plan, queue, runs, wave brief, and Serve must say
`infrastructure_blocked`, name the failed executable/PATH source, and give one
operator action.

### F3 — Installed Tusker capability manifest

Orchestrators must query the installed binary instead of trusting docs or a
dispatch sheet. Add a read-only `tusker capabilities --json` contract covering:

- binary version and build fingerprint;
- supported commands/subcommands and relevant flags;
- supported task, delivery-plan, review, completion, and receipt schemas;
- runner harness adapters and capability-catalog schema;
- enabled/compiled optional capabilities; and
- deprecations or replacements for absent commands.

Documentation and skills may describe intent; the installed capability
manifest is authoritative for what an orchestrator may invoke.

### F4 — Canonical task registration across worktrees

A task created on a worker branch cannot remain invisible to the control plane
that must board it. Task admission from any linked worktree must be mediated by
one canonical control writer—resident daemon/shared control checkout when
automation is active, or the explicit interactive control path when it is not.

Tusker must either write the task registration through to that canonical
control surface atomically or refuse before creating a branch-local task
record. Sweeping arbitrary worktree Markdown is diagnostic fallback only; it
must not silently choose between conflicting task identities. Pending
registration and its remedy are first-class state.

### F5 — First-class boarding census and atomic boarding receipt

The resident daemon, not a launchd schedule, owns departure decisions. launchd
may keep the resident daemon alive, but repository privacy permissions and
departure cadence belong to the daemon/control plane.

Tusker needs a stable boarding census JSON projection (preferably wave-scoped)
that reports, per candidate: exact task binding, clean/frozen revision, proof,
review, gates, blockers, claim health, and boarding readiness. The deterministic
integration handler must atomically bind the accepted task transition to the
exact merge commit in one boarding/completion receipt. Conductors and UIs
consume this contract instead of reimplementing readiness by scraping.

### F6 — Waiting, parked, and stalled are different states

A parked attempt is not made healthy by emitting heartbeats forever. Tusker
must distinguish:

- a live worker with a renewable lease and heartbeat;
- an intentional wait with `next_wake_at`, wake source, and bounded deadline;
- an infrastructure-blocked terminal attempt with an operator remedy; and
- no-progress/stalled work that has exceeded its progress deadline.

The resident daemon watches those deadlines. An overdue wait or missing live
heartbeat becomes a loud typed escalation in runs, queue, wave brief, digest,
and Serve. A terminal parked attempt cannot impersonate a healthy waiting
process.

### Next-wave priority

The follow-on V2 plan should order the work:

1. installed capabilities plus runner pre-claim health (`F2`, `F3`);
2. typed run/wait/escalation state (`F6`);
3. canonical task registration (`F4`);
4. boarding census and atomic receipt (`F5`); and
5. human break-glass close (`F1`) with independent security review.

That order removes invisible infrastructure failures first, then fixes control
plane liveness and registration before changing close authority.
