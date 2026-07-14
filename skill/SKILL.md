---
name: tusker
description: "Operate Tusker repo-local task contracts, proof, review, human gates, and agent orchestration."
---

# Tusker Operator Skill

## When To Use

Use this skill when a repository contains a Tusker vault or a task asks you to create, pick up, dispatch, prove, review, close, or explain Tusker work.

## Execution Modes

### Spec-to-wave planning

Models may propose a versioned delivery plan with an explicit stable scope,
source-keyed tasks, acceptance,
exact verification, dependencies, artifacts, owned paths, runner/concurrency
hints, and knowledge nodes. Tusker owns the final records: use `tusker delivery
import --plan <path> --dry-run` to validate the graph and mapping, then import it
atomically. Planning and import create held work and never dispatch, promote, or
authorize execution.

### Interactive work

A Codex or Claude session opened directly by the user implements the requested
work itself. Use Tusker for task contracts, packets, dependencies, proof,
gates, review, and lifecycle state. When a task ID is available, inspect it
with `tusker show <TASK-ID> --capsule` or `tusker packet <TASK-ID> --for agent`,
then read only the routed project-skill/domain files before implementation.

Never start `tusker daemon run`, invoke `tusker automation dispatch`, or launch
nested `codex exec`/`claude -p` workers from this session. Creating, grooming,
or updating a ready task is inert. Background execution belongs only to an
independently running resident daemon for projects with automation enabled.
The interactive session may inspect or change task/project settings, but it
implements the current user's coding request with its own tools.

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
normalized runtime outcomes. The worker owns task implementation, proof, and
the product-level terminal action: request review when acceptance is proven,
or record one concrete blocker/human gate when it cannot proceed. It must not
manually replace the harness's claim, heartbeat, submit, or failure records.

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
close objectively proven work at every tier. Explicit human gates remain binding
for capability, external authority, unresolved intent, and contractually
subjective acceptance.

## Hard Rules

- A task is a contract, not a chat log.
- `active` is never a durable V7 task status.
- Runtime activity lives in runs, leases, sessions, attempts, and workspaces.
- Human gates stop the agent. Do not keep validating around them.
- Proof must map to acceptance. A vague summary is not proof.
- Abandoned tasks are discarded with `tusker discard`, never physically deleted or moved to `cancelled` by raw status mutation. Inspect the downstream impact first and explicitly detach or discard dependents.
- When Xcode fails from generated build-state corruption, run `tusker xcode doctor`; if it reports `likely_infrastructure`, do not claim code validation from that failed build.
- Explainer packets help humans understand and participate; they do not satisfy proof by themselves.
- Raw logs do not belong in task markdown.
- Tags are generated projections; typed frontmatter is source of truth.


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
| Existing repo setup/onboarding | `references/REPO_ONBOARDING.md` |
| Human gates and closeout | `references/CLOSEOUT_PROTOCOL.md` |
| Proof modes and evidence | `references/RISK_AND_EVIDENCE.md` |
| Xcode generated build-state failures | `references/XCODE_BUILD_STATE.md` |
| Obsidian/Bases projections | `references/OBSIDIAN_BASES.md` |

## Default Agent Loop

```text
interactive: task context → routed domain canon → scoped code search → edit → exact verification → proof → optional explainer → review/closeout
automation (explicit only): plan → packet → dispatch/daemon → configured runner
dispatched worker: claimed run → packet → routed domain canon → edit → exact verification → normalized proof/outcome
```

Do not replace this with broad repo scans unless the task explicitly requires discovery.
