---
name: tusker
description: "Operate Tusker repo-local task contracts, proof, review, human gates, waves, and agent orchestration. Use when a repository contains .tusker, the user names a Tusker task, or work must be planned, executed, reviewed, or closed through Tusker."
license: "LICENSE"
compatibility: "Requires the tusker CLI and a repository-local Tusker V7 vault."
metadata:
  wave_authorization_schema: "tusker.wave-authorization/v1"
  workflow_version: "1"
  tracker_schema_version: "7"
  factory_intake_contract_schema: "tusker.factory-intake-contract/v1"
  factory_intake_contract_version: "1.1.0"
  factory_intake_contract_fingerprint: "sha256:0704d5ee907d738c496512b5ae948e96590a7b732c4ab774bee1de1429b5b13c"
---

# Tusker Operator Skill

## When To Use

Use this skill when a repository contains a Tusker vault or a task asks you to create, pick up, dispatch, prove, review, close, or explain Tusker work.

## Execution Modes

### Factory intake and delivery DAGs

Route work by semantic scope, even when the user never says “Tusker,” “DAG,”
“task,” “wave,” or “daemon.” Read-only evaluation stays read-only. One genuinely
bounded implementation outcome may use the singleton/direct path. Planning,
decomposition, unattended delivery, or implementation with multiple
independently provable outcomes **must** be authored as a versioned
`tusker.delivery-plan/v2` DAG; never hand-create an arbitrary task series.
Author each V2 plan with an explicit stable scope. Use source-keyed tasks and
gates; source keys are the only caller-supplied identities. Tusker owns the final records
and allocates durable epic, task, gate, wave, revision, and event identities during import.

Ask the user for product facts only: desired outcomes, observable acceptance,
important tests and failure cases, constraints, priorities, non-goals, and
genuine unresolved authority or subjective decisions. Tusker and the agent own
epic/task/gate IDs, dependency syntax, waves, frontiers, workspaces, runners,
proof modes, retries, review cycles, and integration mechanics.

A delivery epic groups the product outcome. A wave is a separate,
fingerprint-bound execution authorization over exact tasks, gates,
dependencies, and context; an epic is never executable authority. Build the
bounded context, validate, and render the product review:

```bash
tusker delivery context --spec <SPEC> --scope <STABLE-SCOPE> --json
tusker delivery doctor --plan <PLAN.yaml> --json
tusker delivery review --plan <PLAN.yaml>
```

Plan creation, context, review, and dry-run are read-only. Import may create or
reconcile held records. All of them are inert: they never dispatch, register or
enable a project, start or install a daemon, arm work, move a Git ref, satisfy a
gate, authorize release, or authorize paid model work.

For unattended delivery, the product review emits one exact Start action. Run
only that fingerprint-bound action after the human explicitly chooses it:

```bash
tusker delivery start --plan <PLAN.yaml> \
  --confirm <PLAN-FINGERPRINT> \
  --by human:<name>
```

Start revalidates the plan, bounded context, task/gate/dependency contracts,
runner/workspace policy, integration base, project opt-in, and resident-daemon
preflight. It atomically reconciles held records and arms only the resulting
exact wave. It does not grant missing infrastructure or broader authority.
Automation remains separately opt-in per project, new projects use
`automation.dispatch_scope: armed_waves`, and a stale, paused, or disarmed wave
cannot produce new daemon claims.

### Interactive work

A Codex or Claude session opened directly by the user implements the requested
work itself. Use Tusker for task contracts, packets, dependencies, proof,
gates, review, and lifecycle state. When a task ID is available, inspect it
with `tusker show <TASK-ID> --capsule` or `tusker packet <TASK-ID> --for agent`,
then read only the routed project-skill/domain files before implementation.

Before modifying a tracked task, atomically open the canonical interactive work
session:

```bash
tusker work start <TASK-ID> --by agent:<name> --source codex
```

Use the returned packet/workspace/revision exactly. A healthy existing owner,
dependency/gate blocker, stale task revision, or unsafe workspace is a
coordination refusal, not permission to edit around Tusker. Finish through
`tusker work submit`, `tusker work fail`, or `tusker work release` with the same
owner and revision. This work session works while project automation is
disabled; it does not require daemon enablement or a daemon lifecycle claim,
and never launches a model.

Never start `tusker daemon run`, invoke `tusker automation dispatch`, or launch
nested `codex exec`/`claude -p` workers from this session. Creating, grooming,
or updating a ready task is inert. Background execution belongs only to an
independently running resident daemon for projects with automation enabled.
The interactive session may inspect project settings; it changes automation,
release, spending, or daemon authority only when the user explicitly requests
that exact action. It implements the current coding request with its own tools.
The interactive work session is ownership bookkeeping, not daemon enablement
or permission to launch a nested worker.

### Automated work

Tusker automation is opt-in per project. Interactive agents may create tasks,
inspect `tusker automation plan`, and manage the project's automation setting,
but they never invoke dispatch or start the daemon. An independently managed
resident daemon is the sole process allowed to turn eligible plans into
background workers. `tusker automation plan` is read-only and does not
authorize dispatch.

### Dispatched workers

When `TUSKER_ATTEMPT_ID` is present, follow the claimed-run protocol, work
only the claimed task, and do not spawn another runner or daemon.

### Dispatched reviewers

A dispatched review attempt is read-only with respect to implementation files,
Git refs, and task lifecycle state. Inspect the exact injected task, source,
work revision, proof, gates, and acceptance contract, then submit exactly one
attempt-bound result with the command supplied in the review packet:

```bash
tusker review submit <TASK-ID> \
  --attempt <ATTEMPT-ID> \
  --task-rev <TASK-REV> \
  --source-sha <SOURCE-SHA> \
  --work-rev <WORK-REV> \
  --proof-fingerprint <PROOF-FINGERPRINT> \
  --gate-fingerprint <GATE-FINGERPRINT> \
  --verdict pass|changes_requested|blocked \
  --covers <ACCEPTANCE-IDS> \
  --summary "<BOUNDED-SUMMARY>"
```

Use `--finding` for each actionable `changes_requested` finding and
`--blocker machine|infrastructure|human` for `blocked`. A human blocker requires
a real open human-owned gate. Reviewer prose and process exit are not
acceptance.

Never merge, land, close, move refs, change task status, satisfy gates, or edit
implementation files from the review lane. The deterministic control plane
consumes a valid typed result. If the installed CLI or packet does not support
the typed command, report the stale contract and stop; do not fall back to
legacy reviewer choreography.

## Claimed-run protocol

This protocol applies only to workers launched by the resident daemon. A
directly opened interactive Codex or Claude session does not become a daemon
worker, does not claim a daemon lifecycle run, and does not require project
automation to be enabled. It should inspect an existing live run before taking
over the same tracked task; a conflict is a coordination fact, not permission
to launch another worker or a reason unrelated direct work must stop.

The daemon atomically claims the task before it creates the worker process. A
dispatched worker must verify that its injected task, attempt, workspace, and
packet match; it must not claim again:

```bash
tusker packet <TASK-ID> --for agent
tusker runs inspect <TASK-ID> --json
```

The runner harness owns session attachment, heartbeats, process monitoring, and
normalized runtime outcomes. The worker owns task implementation and the
smallest objective proof mapped to acceptance. It requests review when that
proof is ready, or records one concrete blocker/human gate when it cannot
proceed. It never merges, lands, closes, moves refs, or schedules successors,
and it must not manually replace the harness's claim, heartbeat, submit, or
failure records.

Abrupt termination is handled later by heartbeat expiry and safe reclaim; do
not forge a terminal result. The durable task remains `ready` or `rework`
while its lease projects live execution. Never write `active` or
`in_progress` into task frontmatter.

For broad, high-risk, or agent-heavy work where the human needs a mental model before review, generate an understanding packet:

```bash
tusker packet <TASK-ID> --for explainer
```

Read the explainer before the raw diff when the user asks for understanding, but do not treat it as proof or approval.

## Human Approval Boundary

Reserve human gates for work that requires human capability, authority, or
explicitly subjective acceptance:

- credentials, secrets, OAuth/login, account ownership, billing, or inaccessible environments/devices;
- security, privacy, legal, production-release, or destructive external authority;
- a genuine contradiction or missing product decision that the approved task/spec does not resolve;
- final human acceptance of screenshots, recordings, UX feel, brand quality, or other end artifacts when the contract explicitly requires subjective signoff.

Everything already decided by the task, acceptance criteria, governing spec, or
linked decision is already approved. Implement it without asking the human to
approve it again. In particular, removal, migration, naming, compatibility, and
mapping choices stated by the contract are not fresh decisions.

Before creating a human gate, answer all four:

1. What exact fact, capability, authority, or subjective judgment is missing?
2. Why can no implementing or independent reviewer agent provide it?
3. Where does the approved contract leave it unresolved or explicitly require human acceptance?
4. What exact human action and evidence clears the gate?

If question 3 points to a decision the contract already made, do not create the
gate. Continue the work or send an unmet acceptance item to the reviewer/rework
lane. Agents own objective inspection of diffs, code, tests, logs, screenshots,
recordings, and artifacts. A human may receive those artifacts for explicitly
subjective final acceptance, not as a substitute for agent verification.

Risk changes proof depth, reviewer strength, and landing safeguards; risk alone
does not justify a human gate or human close policy. Independent reviewers may
objectively accept work at every tier through a typed result; the deterministic
control plane performs landing and closure. Explicit human gates remain binding
for capability, external authority, unresolved intent, and contractually
subjective acceptance.

## Proof Economics (Gates Over Records)

Building and re-verifying are cheap; context and human attention are not. Every
process artifact must either gate a decision (accept, land, review, dispatch)
or preserve a human decision and its why. Regenerable history passes neither
test — do not write it.

- Proof is the smallest set of verification rows that covers the acceptance
  contract. For a small task, one command row is a complete proof. Never pad
  proof to look thorough.
- Do not write progress logs, unchanged-state updates, narrative evidence, or
  transcripts. No gate reads them; every future context pays to skip them.
- When a guard refuses with a remedy that involves no decision (open an
  attempt, use a proposal), apply the remedy and continue; report it in one
  line. Do not re-litigate the guard.
- If a status is stale, suspect the mechanism before the operator: fix or flag
  the transition that should have set it, do not add reminder ceremony.

## Hard Rules

- A task is a contract, not a chat log.
- Use the Tusker CLI for canonical lifecycle, proof, gate, wave, and evidence mutations. Do not hand-edit control fields when a CLI command exists.
- Successful CLI mutations notify the resident daemon for targeted project reconciliation. Timed reconciliation is an adaptive safety net for raw external edits and recovery, not the primary state-change channel.
- `active` is never a durable V7 task status.
- Runtime activity lives in runs, leases, sessions, attempts, and workspaces.
- Human gates stop the agent. Do not keep validating around them.
- Proof must map to acceptance. A vague summary is not proof.
- A dispatched reviewer only submits a typed review result. It never edits
  implementation, merges, lands, closes, moves refs, or changes task/gate
  lifecycle state.
- Abandoned tasks are discarded with `tusker discard`, never physically deleted or moved to `cancelled` by raw status mutation. Inspect the downstream impact first and explicitly detach or discard dependents.
- When Xcode fails from generated build-state corruption, run `tusker xcode doctor`; if it reports `likely_infrastructure`, do not claim code validation from that failed build.
- Explainer packets help humans understand and participate; they do not satisfy proof by themselves.
- Raw logs do not belong in task markdown.
- Tags are generated projections; typed frontmatter is source of truth.
- Work spanning more than one lane, worktree, or branch follows
  `references/INTEGRATION_MERGE.md`: lanes open a canonical work session before
  work, shared scarce
  resources (migration numbers, lockfiles, generated files) belong to a single
  integrator, and full-suite gates run as unattended batch, never inside a lane.
- Gate-tier proof runs in harvest mode (no fail-fast) behind cheap preflight
  checks, and never repeats on an unchanged tree. Serial
  fix-recompile-rediscover loops are a defect in slow-compile ecosystems, not
  diligence.


## Hard Stop Rule

When Tusker records `agent_action: stop_until_human_response` or `readiness: waiting_on_human`, stop execution. Do not re-run the same validations, spawn subagents, or invent a workaround. Check:

```bash
tusker closeout status <TASK-ID> --json
tusker proof status <TASK-ID>
```

Revalidation while waiting on human is noise unless the human changed the gate, task, proof, or code.

## Skill Boundary

Treat repo `AGENTS.md` / `CLAUDE.md` as bootstrap pointers only. The installed Tusker operator skill owns tracker mechanics. The repo `.tusker/SKILL.md` owns project knowledge routing.

Keep canonical skills separate from project memory. Generic skill improvements patch canonical skill source. Project facts go into repo-local config/state/profile files such as `.tusker/**`, `.chatgpt-handoff.json`, or `.chatgpt-handoff/profile.md`. Generated installs under `.agents/skills/**` and `.claude/skills/**` are sync/bundle outputs; do not hand-edit them unless they are symlinks to the canonical source.

## Final Response Shape For Human Wait

When machine work is complete but a human gate remains, answer with exactly what is blocked, what the human must do, and the Tusker task/gate id to resume from. Do not claim completion.

## Read Next

| Need | File |
|---|---|
| CLI commands | `references/COMMANDS.md` |
| Repo skill/source policy | `references/REPO_CONTRACT.md` |
| Lifecycle and statuses | `references/WORKFLOW.md` |
| Automation, runners, browser workers, fanout | `references/ORCHESTRATION.md` |
| Multi-lane work, merges, integrator role, slow-compile proof tiers | `references/INTEGRATION_MERGE.md` |
| Existing repo setup/onboarding | `references/REPO_ONBOARDING.md` |
| Human gates and closeout | `references/CLOSEOUT_PROTOCOL.md` |
| Proof modes and evidence | `references/RISK_AND_EVIDENCE.md` |
| Xcode generated build-state failures | `references/XCODE_BUILD_STATE.md` |
| Obsidian/Bases projections | `references/OBSIDIAN_BASES.md` |

Read only the one row needed for the current request. References are terminal
operator resources: do not recursively load sibling references unless the
selected file explicitly identifies one as required for the current operation.

## Default Agent Loop

```text
read-only: bounded context → evidence-backed answer
multi-unit intake: requirements → V2 delivery DAG → doctor → product review → held import
interactive tracked work: work start → packet/canon → edit → exact proof → work submit/fail/release
automation (explicit only): fingerprint-bound Start → resident daemon → claimed implementation → typed review → deterministic integration/close/successor wake
dispatched worker: verify injected claim → packet/canon → edit → exact proof → review handoff
dispatched reviewer: immutable inspection → one typed verdict only
```

Do not replace this with broad repo scans unless the task explicitly requires discovery.
