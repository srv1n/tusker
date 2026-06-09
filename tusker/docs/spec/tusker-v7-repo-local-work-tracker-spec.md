# Tusker V7 Technical Specification: Repo-Local Work Tracker for Agent-Native Software Development

**Status:** Draft for implementation  
**Audience:** Tusker engineering team, agent-orchestration implementers, CLI authors  
**Primary decision:** Tusker remains the canonical work tracker and lives in the same repository as code and knowledge.  
**Secondary decision:** live execution state must be branch-safe. Do not let agents mutate canonical task state from arbitrary implementation branches.

---

## 0. Executive Summary

Tusker V7 should be a **repo-local, markdown-backed, branch-safe work control plane** for humans and coding agents.

The previous design trend—fat markdown tasks containing requirements, status, logs, handoff notes, evidence, and agent scratchpads—will not scale. The V7 design separates durable work objects from runtime events:

```text
Task       = executable work contract
Gate       = blocker / approval / human action / proof requirement / decision
Epic       = roadmap initiative
Domain     = durable repository knowledge
Decision   = durable choice and rationale
Evidence   = proof artifact or proof summary
Attempt    = one agent run against a task
Event      = append-only lifecycle fact
Lease      = temporary claim that an agent/human/CI is currently acting
Packet     = generated context bundle for agent/reviewer/human
```

Tusker should stay inside the repository because that is the point: a clone of the repo should bring the code, docs, work history, decisions, and proof surface together. But Tusker must stop treating the task note as the database. The database is a small set of typed markdown records plus append-only event files. The task note is the human-readable contract.

The core architecture is:

```text
same repository
├── code
├── docs / knowledge / skills
├── tusker work objects
└── tusker generated views / evidence / events

but with branch guards:
├── durable work definitions can be edited in normal branches
├── live state transitions happen only through Tusker control operations
├── implementation branches cannot directly mutate protected state fields
└── runtime leases/attempts are isolated from ordinary feature diffs
```

The punchline:

```text
Same repo, not uncontrolled same-branch state mutation.
```

---

## 1. Why This Exists

The engineering workflow is changing:

```text
old bottleneck: humans writing code
new bottleneck: humans defining intent, acceptance, confidence, review, and coordination
```

Coding agents can write a large fraction of product code, tests, docs, and tooling, but they still need structured, durable, discoverable context. Humans should spend most of their time on:

```text
intent
acceptance criteria
risk policy
gates
product judgment
architecture boundaries
evidence review
harness improvement
```

Tusker exists to be the repo-local system of record for that work.

It is not Jira.  
It is not Confluence.  
It is not an agent transcript graveyard.  
It is not a generic Kanban board.

Tusker is a canonical repository-native work tracker designed for:

```text
human-readable markdown
agent-readable structure
small vertical tasks
first-class gates
evidence-based close
branch-safe orchestration
Obsidian-compatible dashboards
future sync/server support
```

---

## 2. Non-Negotiable Design Principles

### 2.1 Repo-local is the default

All durable project work records must live in the source repository unless explicitly configured otherwise.

Reason:

```text
A clone of the repo should carry the code, docs, decisions, tasks, gates, evidence summaries, and agent instructions needed to understand and continue the project.
```

This is especially important for solo developers, open-source projects, and small teams where an external work tracker adds friction.

### 2.2 Worktrees/clones are execution workspaces, not the work tracker

Tusker should support agents running in:

```text
main checkout
git worktree
fresh clone
container clone
remote sandbox
cloud workspace
```

But the work tracking model must not depend on one particular execution workspace type.

A worktree is just one way to isolate code changes. It is not the canonical storage model.

### 2.3 Tasks are contracts, not logs

A task should answer:

```text
What are we trying to make true?
What is accepted as done?
What proof is required?
Who or what moves next?
What gates block it?
```

A task should not contain:

```text
raw command output
full agent transcript
long work logs
repeated status transition history
giant review packets
implementation diary
stale handoff sludge
```

Those belong in evidence, attempts, or events.

### 2.4 Gates are first-class records

Do not model "human must log in", "CI must run cargo", "human must approve paid provider probe", or "product must decide OAuth vs API key" as ordinary implementation tasks.

These are gates:

```text
auth gate
env gate
CI gate
verification gate
signoff gate
decision gate
quota gate
external-service gate
manual hold gate
```

The UI may call them "Human Actions" when the owner is a human, but the schema should call them `gate`.

### 2.5 Obsidian compatibility matters, but Obsidian must not become the database design

Markdown and flat YAML frontmatter are required.

Nested YAML should be avoided for anything humans need to query in Obsidian Bases.

Generated dashboard notes should exist because Obsidian is not a transaction engine.

### 2.6 Protected state must be mechanically guarded

Agents will do whatever the system permits. If state bloat and merge conflicts are possible, they will happen.

Tusker must enforce:

```text
no raw logs in task bodies
no protected state edits from implementation branches
no done task with open blocking gate
no task close without required evidence
no open task without a next action projection
no blocked task without explicit gate/dependency/hold
```

### 2.7 Future sync must be possible

Do not build a bespoke markdown shape that cannot later move to:

```text
Git-backed sync
S3/R2 object store with compare-and-swap
server mode
Yjs/CRDT editor surface
mobile editing
small-team collaboration
```

Every object must have stable identity, revision metadata, and deterministic validation.

---

## 3. Recommended Repository Layout

V7 should use a visible `tusker/` directory for canonical human-readable work records. Avoid hiding primary records under `.tusker/` because Obsidian and humans should see them.

```text
repo/
├── AGENTS.md
├── tusker.yaml
├── docs/
│   ├── architecture.md
│   ├── reliability.md
│   ├── security.md
│   ├── qa-plan.md
│   └── agent-reviewers.md
│
├── skills/
│   ├── project/
│   │   ├── SKILL.md
│   │   ├── local-dev.md
│   │   ├── testing.md
│   │   └── pr-lifecycle.md
│   └── tusker/
│       ├── SKILL.md
│       └── commands.md
│
├── tusker/
│   ├── README.md
│   ├── projects/
│   │   └── default.md
│   │
│   ├── work/
│   │   ├── epics/
│   │   │   └── HSP.md
│   │   ├── tasks/
│   │   │   └── HSP-T-0042.md
│   │   ├── gates/
│   │   │   └── HSP-G-0003.md
│   │   ├── decisions/
│   │   │   └── HSP-D-0001.md
│   │   ├── inbox/
│   │   │   └── proposed-followup-2026-05-13.md
│   │   └── archive/
│   │
│   ├── knowledge/
│   │   └── domains/
│   │       └── providers/
│   │           ├── INDEX.md
│   │           ├── CANON.md
│   │           ├── runbooks/
│   │           └── decisions/
│   │
│   ├── evidence/
│   │   └── HSP-T-0042/
│   │       ├── HSP-T-0042-E-0001.md
│   │       ├── provider-ready.png
│   │       └── smoke-video.mov
│   │
│   ├── events/
│   │   └── 2026/
│   │       └── 05/
│   │           └── HSP-T-0042--20260513T050001Z--01HXYZ.json
│   │
│   ├── attempts/
│   │   └── HSP-T-0042/
│   │       └── HSP-T-0042-A-0001.md
│   │
│   ├── dashboards/
│   │   ├── human-actions.md
│   │   ├── agent-ready.md
│   │   ├── review-queue.md
│   │   └── recently-done.md
│   │
│   └── _generated/
│       ├── indexes/
│       │   ├── tasks.json
│       │   ├── gates.json
│       │   └── dashboard.json
│       ├── packets/
│       │   ├── HSP-T-0042.agent.md
│       │   └── HSP-T-0042.reviewer.md
│       └── bases/
│           ├── agent-ready.base
│           └── human-actions.base
```

### 3.1 Git-ignore policy

Recommended `.gitignore` additions:

```gitignore
# Local runtime state; not canonical
.tusker-local/
.tusker-runtime/

# Large raw logs unless explicitly promoted to evidence
tusker/evidence/**/raw-*.log
tusker/evidence/**/raw-*.txt

# Generated packet cache can be rebuilt
tusker/_generated/packets/*.tmp
```

Do **not** gitignore canonical markdown objects:

```text
tusker/work/**
tusker/knowledge/**
tusker/evidence/**/*.md
tusker/events/**/*.json
tusker/attempts/**/*.md
tusker/dashboards/**
```

Large binary evidence files may be committed, Git LFS-backed, or stored externally depending on project policy.

---

## 4. Branch-Safe Storage Model

The core problem with repo-local work tracking is not Git itself. Git is fine. The problem is allowing every agent branch to mutate the same canonical task status fields.

V7 solves this with a split between durable object content and protected state mutation.

### 4.1 Branch classes

Tusker recognizes branch classes:

```text
control branch
  Usually main, trunk, master, or a configured branch.
  Can mutate canonical state.

implementation branch
  Feature/work branch for code changes.
  Can add evidence and proposals.
  Cannot directly mutate protected lifecycle fields.

state branch, optional
  Same Git repository, separate branch used for leases/runtime events in multi-agent mode.
  Not required for solo v1.

generated branch, optional
  For publishing dashboards or external synced views.
```

Configuration:

```yaml
# tusker.yaml
schema: tusker.config/v1
project_id: tusker
default_branch: main

branches:
  control:
    - main
    - trunk
  implementation_patterns:
    - "task/*"
    - "agent/*"
    - "feature/*"
  state_branch: tusker/state
```

### 4.2 Protected fields

These fields must not be changed in ordinary implementation branches unless the change is made through a Tusker control operation:

```text
schema
kind
id
project
status
readiness
next_owner
next_source
next_ref
next_action
accepted_by
accepted_at
closed_at
superseded_by
state_rev
state_updated_at
state_updated_by
```

Editable from implementation branches:

```text
title
intent body
acceptance body
non-goals body
domains
knowledge links
docs/canon updates
new tasks in inbox
evidence records under unique IDs
attempt summaries
decision proposals
```

### 4.3 State transition rule

A task lifecycle transition must be applied through:

```sh
tusker status <task-id> <new-status>
tusker close <task-id>
tusker gate satisfy <gate-id>
tusker gate waive <gate-id>
tusker reconcile
```

These commands must enforce:

```text
current branch is a configured control branch
OR mutation mode is explicitly configured for single-user local mode
OR a configured remote state backend accepts the mutation
```

If an agent tries to close a task from a feature branch:

```text
error: protected Tusker state cannot be mutated from branch agent/HSP-T-0042.
Use:
  tusker handoff HSP-T-0042 --summary ...
  tusker evidence add ...
  tusker propose close HSP-T-0042
```

### 4.4 Same repo, safe agents

The workflow becomes:

```text
1. Human creates task/gate in repo.
2. Agent claims task through Tusker.
3. Agent runs in worktree/clone/container.
4. Agent modifies code/docs and adds evidence/attempt summary.
5. Agent opens PR.
6. Reviewers inspect acceptance + evidence.
7. After merge or acceptance, Tusker updates canonical task state on control branch.
```

This preserves the property you want:

```text
After the work lands, the repository contains the code, docs, task, gates, evidence summaries, and decision history together.
```

But it avoids this disaster:

```text
agent branch A edits HSP-T-0042 status
agent branch B edits HSP-T-0042 status
human edits HSP-T-0042 status on phone
merge conflict soup
```

### 4.5 Event-per-file, not append-to-one-log

Do not append every event to one giant `events.jsonl` file. That creates guaranteed merge conflicts.

Use one event file per event:

```text
tusker/events/YYYY/MM/<object-id>--<timestamp>--<ulid>.json
```

Example:

```json
{
  "schema": "tusker.event/v1",
  "id": "01J0H3EKW5Z7N4JG6CN4M9EV64",
  "project": "tusker",
  "object": "HSP-T-0042",
  "object_kind": "task",
  "event_kind": "status_changed",
  "actor": "human:sarav",
  "at": "2026-05-13T05:00:01Z",
  "from": {
    "status": "review"
  },
  "to": {
    "status": "done"
  },
  "reason": "Outcome accepted with required evidence attached.",
  "evidence": [
    "HSP-T-0042-E-0001",
    "HSP-T-0042-E-0002"
  ]
}
```

This makes merges mostly conflict-free.

---

## 5. Object Model

### 5.1 Object kinds

```text
project
epic
task
gate
decision
domain
evidence
attempt
event
packet
dashboard
```

### 5.2 Identity rules

IDs must be stable and human-readable.

```text
Epic:      HSP
Task:      HSP-T-0042
Gate:      HSP-G-0003
Decision:  HSP-D-0001
Evidence:  HSP-T-0042-E-0001
Attempt:   HSP-T-0042-A-0001
Event:     ULID or UUIDv7
Domain:    providers
```

Path must match ID:

```text
tusker/work/tasks/HSP-T-0042.md
tusker/work/gates/HSP-G-0003.md
tusker/evidence/HSP-T-0042/HSP-T-0042-E-0001.md
```

### 5.3 Revision rules

Every canonical object should carry a `state_rev`.

```yaml
state_rev: "sha256:..."
```

`state_rev` is computed from canonical normalized content, excluding generated fields if needed. Save operations must use compare-and-swap semantics:

```text
load object
compute current rev
prepare mutation with base rev
before write, re-read object
if current rev != base rev, fail with conflict
write
emit event
```

This prepares Tusker for future S3/R2/server sync.

---

## 6. Task Schema

### 6.1 Frontmatter

```yaml
---
schema: tusker.task/v7
kind: task
id: HSP-T-0042
project: tusker
title: Add direct OpenAI provider smoke harness
epic: HSP

status: ready
readiness: blocked_by_gate

priority: p1
risk: medium
size: m

next_owner: human:sarav
next_source: gate
next_ref: HSP-G-0003
next_action: "Complete OpenAI OAuth so provider discovery can be smoke-tested."

domains:
  - providers
  - auth

gates:
  - HSP-G-0003
  - HSP-G-0004

dependencies: []

evidence_required:
  - automated_test
  - screenshot
  - smoke_summary

created_at: 2026-05-13T05:00:00Z
created_by: human:sarav
updated_at: 2026-05-13T05:00:00Z
updated_by: human:sarav

state_rev: "sha256:..."
---
```

### 6.2 Body template

```md
# HSP-T-0042 · Add direct OpenAI provider smoke harness

## Intent

Add a smoke harness that proves provider readiness behavior without requiring humans to inspect raw logs.

## Acceptance

| ID | Outcome | Proof |
|---|---|---|
| A1 | Missing credentials produce a clear setup error, not a panic. | Focused automated test |
| A2 | Live probe cannot run unless explicit paid-provider approval is present. | Gate HSP-G-0004 plus automated guard test |
| A3 | Provider readiness evidence is compact. | Evidence packet includes summary and screenshot |

## Non-goals

- Do not run paid provider probes by default.
- Do not store secrets in Tusker, logs, screenshots, or chat transcripts.

## Verification

- `go test ./cmd/tusker -run TestOpenAIProviderSmoke -count=1`
- Live smoke only after HSP-G-0004 is satisfied.

## Evidence

Pending.

## Knowledge delta

None expected unless provider setup behavior changes.
```

### 6.3 Status enum

Use these lifecycle statuses:

```text
idea
backlog
ready
review
rework
done
cancelled
superseded
```

Do not make `active` a durable task status. Active/running is runtime state derived from leases and attempts.

Why:

```text
active changes frequently
active is a claim, not the task's durable lifecycle state
committing active from agent branches creates merge churn
```

### 6.4 Readiness enum

Readiness is computed/materialized:

```text
ready
blocked_by_gate
blocked_by_dependency
waiting_on_review
waiting_on_ci
held
done
cancelled
superseded
```

`readiness` should be recomputed by `tusker reconcile`.

### 6.5 Next action projection

`next_owner`, `next_source`, `next_ref`, and `next_action` are a projection.

Source of truth:

```text
open blocking gates
dependencies
lifecycle status
review policy
task's own next action
```

Projection algorithm:

```text
if status in done,cancelled,superseded:
    next_owner = none
    next_source = status
    next_ref = ""
    next_action = ""

else if open blocking gate exists:
    gate = highest_priority_open_blocking_gate(task)
    next_owner = gate.owner
    next_source = gate
    next_ref = gate.id
    next_action = gate.action

else if unresolved dependency exists:
    dep = highest_priority_dependency(task)
    next_owner = blocked_dependency
    next_source = dependency
    next_ref = dep.id
    next_action = "Wait for dependency <dep.id> to reach done."

else if status == review:
    next_owner = owner_required_by_risk_policy(task)
    next_source = review_policy
    next_ref = ""
    next_action = "Review evidence and close or return to rework."

else:
    next_owner = agent
    next_source = task
    next_ref = task.id
    next_action = task-authored next action or generated action
```

### 6.6 Task validation

Hard failures:

```text
task file path does not match id
missing schema/kind/id/project/title/status/risk/priority
status not in enum
risk not in enum
open task missing next_owner/next_action
done task has open blocking gates
done task missing accepted_by/accepted_at
done task missing required evidence
readiness is inconsistent with open gates/dependencies
raw command output in body
"Work Log" section in body
"Execution Diary" section in body
body > configured hard line limit
protected fields modified from non-control branch
```

Warnings:

```text
body > configured warning line limit
acceptance criteria missing proof column
verification section missing exact command/proof
knowledge delta too long
too many domains
large evidence artifact not LFS/external-backed
```

Recommended defaults:

```yaml
validation:
  task_body_warn_lines: 120
  task_body_fail_lines: 220
  frontmatter_warn_lines: 60
```

---

## 7. Gate Schema

### 7.1 Why gates are not human tasks

A human-owned task produces a deliverable.

A gate unlocks, approves, verifies, or decides something.

```text
Founder writes GTM copy                -> human-owned task
Human completes OpenAI OAuth           -> gate
Human approves paid provider probe     -> gate
CI runs cargo verifier                 -> gate
Product chooses OAuth vs API key only  -> decision gate
Manual release approval                -> signoff gate
```

The dashboard may label human-owned gates as "Human Actions", but the schema should remain `gate`.

### 7.2 Frontmatter

```yaml
---
schema: tusker.gate/v1
kind: gate
id: HSP-G-0003
project: tusker
title: Complete OpenAI OAuth for provider smoke tests

gate_kind: auth
status: open
owner: human:sarav
priority: p1
blocking: true

blocks:
  - HSP-T-0042

action: "Complete OpenAI OAuth in the desktop app."
verification: "Provider active-models endpoint returns at least one OpenAI model with status=ready."

created_at: 2026-05-13T05:00:00Z
created_by: agent:codex
updated_at: 2026-05-13T05:00:00Z
updated_by: agent:codex

state_rev: "sha256:..."
---
```

### 7.3 Body template

```md
# HSP-G-0003 · Complete OpenAI OAuth for provider smoke tests

## Action

Complete OpenAI OAuth in the desktop app.

## Steps

1. Open the desktop app.
2. Go to Settings → Providers → OpenAI.
3. Click **Connect OpenAI**.
4. Complete browser OAuth.
5. Return to the app.
6. Confirm OpenAI shows as connected.

## Verification

Run:

```sh
curl http://localhost:PORT/v1/app-services/providers/active-models
```

Expected:

```text
At least one OpenAI model appears with status=ready.
```

## Secret policy

Do not paste OAuth tokens or API keys into Tusker, task notes, logs, screenshots, or chat transcripts.

## Unblocks

- [[HSP-T-0042]]
```

### 7.4 Gate kind enum

```text
auth
env
setup
dev_host
ci
verification
signoff
decision
quota
external_service
manual_hold
security
release
```

### 7.5 Gate status enum

```text
open
satisfied
waived
obsolete
```

### 7.6 Gate validation

Hard failures:

```text
missing owner
missing action
missing verification for blocking gate
blocking gate with empty blocks list
status not in enum
gate_kind not in enum
satisfied gate missing satisfied_by/satisfied_at
waived gate missing waived_by/waived_at/waive_reason
protected fields modified from non-control branch
```

Warnings:

```text
owner is human but body lacks steps
external_service gate lacks official documentation link or setup notes
auth/env gate body lacks secret policy
verification gate lacks exact command/manual proof
```

---

## 8. Epic Schema

### 8.1 Frontmatter

```yaml
---
schema: tusker.epic/v7
kind: epic
id: HSP
project: tusker
title: First-class harness provider setup
status: active
owner: human:sarav
priority: p1

domains:
  - providers
  - auth
  - runtime

created_at: 2026-05-13T05:00:00Z
updated_at: 2026-05-13T05:00:00Z
state_rev: "sha256:..."
---
```

### 8.2 Body template

```md
# HSP · First-class harness provider setup

## Thesis

Make provider setup and smoke verification reliable enough that agents can run harness work without human terminal babysitting.

## Success criteria

- [ ] Agents can detect configured providers.
- [ ] Live provider probes never run without explicit approval.
- [ ] Provider setup failures produce actionable instructions.
- [ ] Evidence packets are compact enough for human review.

## Current decision

Support OAuth and API-key flows, but live paid-provider probes require explicit approval.

## Open gates

<!-- tusker:generated open-gates -->

## Active work

<!-- tusker:generated active-work -->

## Recently completed

<!-- tusker:generated recently-completed -->
```

### 8.3 Epic rules

Epics must not contain:

```text
full task history
agent transcripts
raw logs
implementation diaries
gigantic generated tables not inside managed blocks
```

Generated tables should be inside managed blocks and rebuildable.

---

## 9. Evidence Schema

### 9.1 Evidence frontmatter

```yaml
---
schema: tusker.evidence/v1
kind: evidence
id: HSP-T-0042-E-0001
project: tusker
task: HSP-T-0042
epic: HSP

evidence_kind: automated_test
status: accepted
covers:
  - HSP-T-0042:A1
  - HSP-T-0042:A2

artifact_paths:
  - tusker/evidence/HSP-T-0042/test-summary.txt

created_by: agent:codex
created_at: 2026-05-13T05:00:00Z
accepted_by: reviewer:agent
accepted_at: 2026-05-13T05:15:00Z
state_rev: "sha256:..."
---
```

### 9.2 Evidence body

```md
# HSP-T-0042-E-0001 · Provider smoke automated test evidence

## Summary

Focused provider smoke tests passed after adding explicit no-credential and live-gate behavior.

## Commands

```sh
go test ./cmd/tusker -run TestOpenAIProviderSmoke -count=1
```

## Result

Pass.

## Covers

- HSP-T-0042 A1
- HSP-T-0042 A2

## Artifact links

- [[test-summary.txt]]
```

### 9.3 Evidence kinds

```text
automated_test
unit_test
integration_test
e2e_test
screenshot
video
trace
log_excerpt
manual_smoke
ci_run
review_packet
security_review
accessibility_review
performance_profile
release_smoke
```

### 9.4 Evidence rule

The task body may link to evidence, but it must not paste raw evidence.

Good:

```md
## Evidence

- [[HSP-T-0042-E-0001]] automated test summary
- [[HSP-T-0042-E-0002]] provider-ready screenshot
```

Bad:

```md
## Verification log

<800 lines of go test output>
```

---

## 10. Attempt Schema

An attempt is one agent run against one task.

Attempt summaries may be committed. Raw transcripts should be local, external, or explicitly promoted.

### 10.1 Attempt frontmatter

```yaml
---
schema: tusker.attempt/v1
kind: attempt
id: HSP-T-0042-A-0001
project: tusker
task: HSP-T-0042
runner: codex
agent_model: gpt-5.3-codex
workspace_kind: git_worktree
workspace_path: "../.tusker-worktrees/HSP-T-0042"
branch: "agent/HSP-T-0042"
status: handoff

started_at: 2026-05-13T05:00:00Z
ended_at: 2026-05-13T06:10:00Z

pr_url: ""
evidence:
  - HSP-T-0042-E-0001

state_rev: "sha256:..."
---
```

### 10.2 Attempt body

```md
# HSP-T-0042-A-0001 · Agent attempt summary

## Outcome

Implemented the deterministic no-credential provider smoke test and live-probe approval guard.

## Changed areas

- `cmd/tusker/...`
- `docs/providers/...`

## Verification

- Focused provider smoke tests passed.
- Broader CLI test suite not run because local Go toolchain unavailable.

## Handoff

Needs human/CI to run full suite and approve live provider probe.

## Follow-ups proposed

- Add provider setup runbook for Twitter/X OAuth.
```

### 10.3 Attempt rules

Attempts are allowed to mention limitations honestly.

Raw chain-of-thought must not be stored.

Raw command logs should be attached as artifacts only when useful.

---

## 11. Decision Schema

Decisions capture durable choices. They are not tasks.

```yaml
---
schema: tusker.decision/v1
kind: decision
id: HSP-D-0001
project: tusker
epic: HSP
title: Use repo-local Tusker work records with branch-safe state mutation
status: accepted
decided_by: human:sarav
decided_at: 2026-05-13T05:00:00Z
supersedes: []
state_rev: "sha256:..."
---
```

Body:

```md
# HSP-D-0001 · Use repo-local Tusker work records with branch-safe state mutation

## Decision

Tusker work records live in the same Git repository as code and knowledge. Runtime status mutation is guarded so implementation branches cannot directly mutate protected state fields.

## Context

Repo-local records make work, docs, evidence, and code travel together. Uncontrolled branch-local status mutation creates merge conflicts and stale dashboards.

## Consequences

- Humans can use Obsidian over the repository.
- Agents can read work records from the repo.
- State changes must go through Tusker commands.
- Runtime leases may use a local store, Git state branch, or future remote CAS store.
```

---

## 12. Domain Knowledge Schema

Domains are persistent areas of truth. They are not epics.

```text
Domain = durable subject area
Epic   = temporary initiative
Task   = executable slice
Gate   = blocker/proof/decision
```

Example:

```text
Domain: providers
Epic: HSP · First-class harness provider setup
Task: HSP-T-0042 · Add direct OpenAI provider smoke harness
Gate: HSP-G-0003 · Complete OpenAI OAuth
```

Domain layout:

```text
tusker/knowledge/domains/providers/
├── INDEX.md
├── CANON.md
├── runbooks/
│   └── openai-oauth.md
├── decisions/
└── sources/
```

`INDEX.md` should answer:

```text
What is this domain?
When should an agent read this?
What files are canonical?
What runbooks exist?
What invariants matter?
```

In V7 frontmatter, `source_of_truth` means repo-local canonical input paths for
that domain or knowledge node. It is not a legacy docs publication freshness
contract. `canonical_files` means the small files an agent should read first,
usually `INDEX.md` and `CANON.md`; future schemas may rename these to
`source_paths` and `canon_files`.

`CANON.md` should answer:

```text
What is currently true?
What are the stable interfaces?
What are the product/architecture constraints?
What is stale or deprecated?
```

---

## 13. Event Schema

### 13.1 Event frontmatter is not needed

Use JSON for events.

```json
{
  "schema": "tusker.event/v1",
  "id": "01J0H3EKW5Z7N4JG6CN4M9EV64",
  "project": "tusker",
  "object": "HSP-T-0042",
  "object_kind": "task",
  "event_kind": "gate_added",
  "actor": "agent:codex",
  "at": "2026-05-13T05:00:01Z",
  "payload": {
    "gate": "HSP-G-0003"
  }
}
```

### 13.2 Event kinds

```text
created
updated
status_changed
gate_added
gate_satisfied
gate_waived
gate_obsoleted
claimed
claim_released
attempt_started
attempt_handoff
attempt_failed
evidence_added
review_requested
review_passed
review_failed
closed
reopened
superseded
cancelled
decision_accepted
```

### 13.3 Event rules

Events are append-only.

Do not edit past events except for explicit redaction.

If redaction is required, write:

```text
redaction event
redacted replacement event
```

Secrets must never be stored in events.

---

## 14. Lease Model

A lease is a temporary claim. It prevents duplicate execution.

Leases are runtime state, not durable work contracts.

### 14.1 Lease fields

```json
{
  "schema": "tusker.lease/v1",
  "id": "01J0H3...",
  "project": "tusker",
  "task": "HSP-T-0042",
  "owner": "agent:codex:runner-003",
  "workspace": "../.tusker-worktrees/HSP-T-0042",
  "branch": "agent/HSP-T-0042",
  "status": "active",
  "claimed_at": "2026-05-13T05:00:00Z",
  "expires_at": "2026-05-13T07:00:00Z",
  "heartbeat_at": "2026-05-13T05:15:00Z"
}
```

### 14.2 Lease backends

V7 should support these in order:

```text
local sqlite/file backend
  for solo use

git state branch backend
  for small teams without a server

remote CAS backend
  future S3/R2/API server sync
```

### 14.3 Git state branch backend

Use branch:

```text
tusker/state
```

Store:

```text
leases/<task-id>.json
runtime/runs/<attempt-id>.json
scheduler/index.json
```

Update protocol:

```text
git fetch origin tusker/state
git checkout --detach origin/tusker/state into temp state workdir
apply CAS update
commit
git push origin HEAD:tusker/state --force-with-lease=<old-sha>
```

If push fails, pull latest state and retry or surface conflict.

This keeps live execution coordination in the same Git repository, but out of normal source PRs.

---

## 15. Agent Workspace Policy

Tusker must not mandate worktrees.

Supported execution workspace kinds:

```text
same_checkout
git_worktree
fresh_clone
container_clone
remote_workspace
```

### 15.1 Recommended default for solo local use

```text
git worktree
```

Why:

```text
fast
cheap disk usage
standard Git semantics
easy to inspect
works well with local app instances when the repo supports per-worktree ports/state
```

### 15.2 Recommended default for CI/cloud agents

```text
fresh clone or container clone
```

Why:

```text
stronger cleanup boundary
less coupling to developer checkout
simpler sandboxing
reproducible boot
```

### 15.3 Tusker rule

The workspace kind is an orchestration setting, not a storage model.

```yaml
orchestration:
  workspace:
    kind: git_worktree
    root: "../.tusker-worktrees"
    branch_pattern: "agent/{{task_id}}"
```

or:

```yaml
orchestration:
  workspace:
    kind: fresh_clone
    root: "/tmp/tusker-workspaces"
    remote: "git@github.com:org/repo.git"
```

---

## 16. State Machine

### 16.1 Lifecycle

```text
idea
  ↓
backlog
  ↓
ready
  ↓
review ── fail ──> rework
  ↓               │
done <────────────┘

cancelled
superseded
```

A task may be `ready` while blocked by gates. This is not a contradiction.

```yaml
status: ready
readiness: blocked_by_gate
next_owner: human:sarav
next_ref: HSP-G-0003
```

### 16.2 Runtime state

Runtime state is derived from leases/attempts:

```text
idle
claimed
running
stalled
handoff
failed
```

This is not the same as task lifecycle.

### 16.3 Close rule

Close means:

```text
the outcome is accepted against acceptance criteria,
with required evidence attached,
and no blocking gates remain open.
```

Not:

```text
agent says done
PR opened
tests passed once
code merged without review
```

### 16.4 Risk-based close policy

```yaml
close_policy:
  low:
    required_acceptor: reviewer_agent
    required_evidence:
      - automated_test
  medium:
    required_acceptor: reviewer_agent
    required_evidence:
      - automated_test
      - evidence_packet
  high:
    required_acceptor: human
    required_evidence:
      - automated_test
      - human_review
  critical:
    required_acceptor: human
    required_gates:
      - release
      - security
```

---

## 17. CLI Specification

### 17.1 Object creation

```sh
tusker new epic HSP --title "First-class harness provider setup"
tusker new task --epic HSP --title "Add direct OpenAI provider smoke harness"
tusker new gate --blocks HSP-T-0042 --kind auth --owner human:sarav \
  --action "Provision staging OAuth credentials." \
  --verification "Provider ready endpoint returned OpenAI model." \
  --why-agent-cannot "Human account access is required."
tusker new decision --epic HSP --title "Use repo-local branch-safe work tracker"
```

### 17.2 Gate operations

```sh
tusker gate list --open
tusker gate list --owner human:sarav
tusker gate satisfy HSP-G-0003 --evidence "Provider ready endpoint returned OpenAI model."
tusker gate waive HSP-G-0004 --reason "Live smoke deferred to release candidate."
tusker gate obsolete HSP-G-0007 --reason "Task superseded."
```

### 17.3 Runtime operations

```sh
tusker claim HSP-T-0042 --owner agent:codex
tusker heartbeat HSP-T-0042
tusker release HSP-T-0042
tusker attempt start HSP-T-0042
tusker attempt handoff HSP-T-0042 --summary ./summary.md
```

### 17.4 Evidence

```sh
tusker evidence add HSP-T-0042 \
  --kind automated_test \
  --covers A1,A2 \
  --summary "Focused provider smoke tests passed."

tusker evidence add HSP-T-0042 \
  --kind screenshot \
  --path ./provider-ready.png \
  --covers A3
```

### 17.5 Packets

```sh
tusker packet HSP-T-0042 --for agent
tusker packet HSP-T-0042 --for reviewer
tusker brief HSP-T-0042
tusker brief --owner human:sarav
```

### 17.6 Dashboard

```sh
tusker dashboard build
tusker dashboard open human-actions
tusker next --owner agent
tusker next --owner human:sarav
tusker next --owner reviewer
```

### 17.7 Validation and reconciliation

```sh
tusker validate
tusker validate --branch-policy
tusker reconcile
tusker compact HSP-T-0042
tusker migrate v7
```

---

## 18. Generated Views

### 18.1 Human brief

Generated by:

```sh
tusker brief HSP-T-0042
```

Output:

```text
HSP-T-0042 · Add direct OpenAI provider smoke harness
Status: ready
Readiness: blocked_by_gate
Next owner: human:sarav
Next action: Complete OpenAI OAuth.
Open gates: HSP-G-0003, HSP-G-0004
Acceptance: 3 items
Evidence required: automated_test, screenshot, smoke_summary
```

### 18.2 Agent packet

Generated by:

```sh
tusker packet HSP-T-0042 --for agent
```

Packet should include:

```text
task contract
open gates summary
dependencies
relevant domain indexes
selected canon snippets
testing commands
evidence requirements
branch policy
close policy
```

Packet should exclude:

```text
raw history
unrelated tasks
giant logs
all docs by default
all previous attempts by default
```

### 18.3 Reviewer packet

Generated by:

```sh
tusker packet HSP-T-0042 --for reviewer
```

Packet should include:

```text
intent
acceptance table
proof required per acceptance item
evidence links
diff summary if PR exists
risk policy
reviewer personas
known gates/waivers
```

### 18.4 Obsidian dashboard notes

Generated markdown dashboards:

```text
tusker/dashboards/human-actions.md
tusker/dashboards/agent-ready.md
tusker/dashboards/review-queue.md
tusker/dashboards/ci-waiting.md
```

Example:

```md
# Human Actions

<!-- tusker:generated:start human-actions -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| [[HSP-G-0003]] | human:sarav | [[HSP-T-0042]] | Complete OpenAI OAuth. |

<!-- tusker:generated:end -->
```

For Obsidian Bases, generate `.base` files if supported, but do not depend on Bases for correctness.

---

## 19. Branch Guard Implementation

### 19.1 Git diff guard

`validate --branch-policy` should inspect:

```sh
git merge-base HEAD origin/main
git diff --name-only <merge-base>...HEAD
git diff <merge-base>...HEAD -- tusker/work/tasks tusker/work/gates
```

Detect protected field changes.

Pseudo-code:

```go
func ValidateProtectedFieldChanges(baseRef, headRef string) error {
    changed := GitChangedFiles(baseRef, headRef, "tusker/work")
    for _, file := range changed {
        if !IsTuskerObject(file) {
            continue
        }
        before := ParseObjectAt(baseRef, file)
        after := ParseObjectAt(headRef, file)

        diff := DiffFrontmatter(before, after)
        for _, field := range diff.ChangedFields {
            if ProtectedField(field) && !AllowedStateChangeContext() {
                return error("protected field changed outside control branch")
            }
        }
    }
    return nil
}
```

### 19.2 Pre-commit hook

Optional hook:

```sh
tusker validate --staged --branch-policy
```

### 19.3 CI rule

Required for team mode:

```text
Pull requests from implementation branches must not change protected Tusker state fields.
```

Exception:

```text
label: tusker-state-change
reviewer: human maintainer
```

Use sparingly.

---

## 20. Merge/Reconcile Strategy

### 20.1 Conflict-minimizing file strategy

Use many small files:

```text
one task per file
one gate per file
one decision per file
one evidence record per file
one attempt per file
one event per file
```

Avoid:

```text
one giant tasks.json
one append-only events.jsonl
one mega epic file with all tasks
one markdown task with all logs
```

### 20.2 Reconcile command

`tusker reconcile` must:

```text
load all tasks/gates/events/evidence
recompute readiness
recompute next action projection
detect stale leases
detect done tasks with open gates
materialize generated dashboards
update generated managed blocks
write events for projection changes if needed
```

### 20.3 Merge conflict policy

If two branches edit the same task contract body:

```text
ordinary Git conflict; human/agent resolves
```

If two branches add distinct evidence files:

```text
no conflict
```

If two branches mutate status:

```text
should be blocked by branch guard before merge
```

If two branches create same new ID:

```text
validation fails; one branch must re-ID
```

### 20.4 ID allocation

Use CLI-managed counters or ULID suffixes.

For human-readable IDs, maintain:

```text
tusker/work/epics/HSP.md
```

with:

```yaml
next_task_number: 43
next_gate_number: 5
```

But this can conflict under parallel creation. Safer options:

```text
Option A: allocate IDs only on control branch
Option B: use timestamp/ULID draft IDs in inbox
Option C: reserve ID blocks per agent
```

Recommended default:

```text
Tasks created by humans/control branch use sequential IDs.
Tasks proposed by agents use inbox proposal files until accepted.
```

---

## 21. Sync Roadmap

### 21.1 V1: Local Git-backed repo

```text
canonical records in repo
manual git sync
Obsidian edits markdown
Tusker CLI validates/reconciles
```

This is enough for solo use.

### 21.2 V2: Small team Git mode

```text
canonical records in repo
leases/runtime state in tusker/state branch
branch policy enforced in CI
dashboard generated on main
```

This supports small teams and open source.

### 21.3 V3: Remote object store

Use S3/R2/server with CAS:

```go
type Store interface {
    GetObject(ctx context.Context, id ObjectID) (Object, Rev, error)
    PutObjectCAS(ctx context.Context, id ObjectID, base Rev, next Object) (Rev, error)
    ListObjects(ctx context.Context, q Query) ([]ObjectRef, error)
    AppendEvent(ctx context.Context, ev Event) error
    GetEvents(ctx context.Context, scope EventScope) ([]Event, error)
}
```

Objects remain serializable to markdown.

### 21.4 V4: Collaborative editor

Use CRDT/Yjs only for live editing UX.

Do not make CRDT the canonical source of truth before the object model is stable.

---

## 22. Go Implementation Plan

### 22.1 Package layout

```text
cmd/tusker/
  main.go

internal/model/
  task.go
  gate.go
  epic.go
  evidence.go
  event.go
  attempt.go
  enums.go

internal/markdown/
  parse.go
  frontmatter.go
  sections.go
  render.go

internal/store/
  store.go
  filesystem.go
  gitstate.go
  cas.go

internal/gitx/
  branch.go
  diff.go
  worktree.go
  protected_fields.go

internal/validate/
  validator.go
  task_rules.go
  gate_rules.go
  evidence_rules.go
  branch_policy.go

internal/reconcile/
  readiness.go
  next_action.go
  materialize.go

internal/dashboard/
  build.go
  markdown_tables.go
  bases.go

internal/packet/
  agent.go
  reviewer.go
  human_brief.go
  context_selector.go

internal/runtime/
  lease.go
  attempt.go
  heartbeat.go
  workspace.go

internal/ids/
  ids.go
  ulid.go
  counters.go

internal/config/
  config.go
```

### 22.2 Core interfaces

```go
type ObjectKind string
type ObjectID string
type Rev string

type Object interface {
    Kind() ObjectKind
    ID() ObjectID
    Rev() Rev
    Validate() []Diagnostic
}

type Store interface {
    Load(ctx context.Context, id ObjectID) (Object, error)
    SaveCAS(ctx context.Context, obj Object, base Rev) (Rev, error)
    List(ctx context.Context, q Query) ([]ObjectRef, error)
    AppendEvent(ctx context.Context, ev Event) error
}

type RuntimeStore interface {
    Claim(ctx context.Context, taskID ObjectID, owner string, ttl time.Duration) (Lease, error)
    Heartbeat(ctx context.Context, leaseID string) error
    Release(ctx context.Context, leaseID string) error
    ListLeases(ctx context.Context, q LeaseQuery) ([]Lease, error)
}
```

### 22.3 Markdown parser requirements

Must preserve:

```text
body sections
comments
managed generated blocks
frontmatter ordering
human formatting where reasonable
```

Must normalize:

```text
timestamps
enum values
list fields
path separators
```

### 22.4 Diagnostics

```go
type Severity string

const (
    Error Severity = "error"
    Warning Severity = "warning"
    Info Severity = "info"
)

type Diagnostic struct {
    Severity Severity
    Code     string
    File     string
    Line     int
    Message  string
    Fix      *FixSuggestion
}
```

Example codes:

```text
TASK_MISSING_NEXT_ACTION
TASK_BODY_TOO_LONG
TASK_RAW_LOG_IN_BODY
GATE_MISSING_VERIFICATION
DONE_TASK_OPEN_GATE
PROTECTED_FIELD_CHANGED
EVIDENCE_REQUIRED_MISSING
READINESS_STALE
ID_PATH_MISMATCH
```

---

## 23. Migration From Current Tusker

### 23.1 Migration goals

```text
shorten tasks
extract gates
extract evidence/logs
remove work logs from task body
materialize dashboards
protect branch state
```

### 23.2 Migration commands

```sh
tusker migrate v7 --dry-run
tusker migrate v7 --write
tusker migrate gates --from-blocked-reason --write
tusker compact --all --archive-logs --write
tusker reconcile --write
```

### 23.3 Migration algorithm

For every existing task:

```text
1. Parse frontmatter and body.
2. Detect blocker phrases:
   - user must
   - human must
   - CI must
   - cannot run because
   - OAuth required
   - env var required
   - dev host required
   - signoff required
3. Create gate records.
4. Replace blocked status with readiness derived from gate.
5. Move Work Log to attempt summary or archive.
6. Move raw Verification Log to evidence artifact.
7. Preserve concise Verification section.
8. Generate Evidence links.
9. Remove stale handoff blocks.
10. Recompute next action projection.
```

### 23.4 Compact rules

Before:

```md
## Work Log
- 2026-05-13: Ran many commands...
- ...
```

After:

```md
## Evidence

- [[HSP-T-0042-E-0001]] focused test evidence

## Knowledge delta

Provider setup behavior unchanged.
```

Moved to:

```text
tusker/attempts/HSP-T-0042/HSP-T-0042-A-0001.md
tusker/evidence/HSP-T-0042/HSP-T-0042-E-0001.md
```

---

## 24. Security and Secret Policy

Tusker records must never contain:

```text
API keys
OAuth tokens
session cookies
private keys
passwords
raw PII
production secrets
```

Validators should scan for common patterns and fail hard.

Gate records involving credentials must include a secret policy section.

Screenshots must be checked or redacted before acceptance when they may contain secrets.

---

## 25. Open Source / Small Team Mode

For open-source projects:

```text
canonical work records stay in repo
contributors can propose task/gate changes by PR
maintainers control state transitions
agents can produce PRs with evidence
CI enforces branch policy
```

Recommended GitHub integration later:

```text
Tusker task -> optional GitHub Issue projection
Tusker gate -> checklist/comment/label projection
Tusker evidence -> PR comment/artifact projection
Tusker status -> issue label/state projection
```

But Tusker remains canonical.

Do not make GitHub Issues canonical by default if the product thesis is repo-local work memory.

---

## 26. Final Design Decision

Tusker should use a repo-local storage layout, but with a stricter state model than normal markdown.

Final recommendation:

```text
1. Keep Tusker canonical records inside the source repository.
2. Use real markdown records for tasks, gates, epics, decisions, evidence, and attempts.
3. Make gates first-class.
4. Make active/running runtime state derived from leases, not durable task status.
5. Use event-per-file, not one giant event log.
6. Guard protected state fields from implementation branches.
7. Allow agents to add evidence and proposals from branches.
8. Apply close/status transitions only through Tusker control commands.
9. Generate Obsidian dashboards and agent/reviewer packets.
10. Keep future sync possible via stable IDs, CAS revs, and small independent objects.
```

This gives you the thing you actually want:

```text
git clone repo
→ see code
→ see docs
→ see tasks
→ see gates
→ see decisions
→ see evidence summaries
→ run Tusker
→ agents can continue work
```

without turning every agent branch into a markdown merge grenade.

---

## 27. Implementation Cut Plan

### Phase 1: V7 schema and validator

Build:

```text
task/gate/epic/evidence schemas
flat frontmatter parser
validation rules
body-size limits
raw-log detector
protected-field diff checker
```

Exit:

```text
tusker validate catches current bloat and unsafe state changes.
```

### Phase 2: Gates and next action projection

Build:

```text
new gate
gate satisfy/waive/obsolete
reconcile readiness
next_owner projection
human actions dashboard
```

Exit:

```text
human gates are visible and stop polluting task backlog.
```

### Phase 3: Evidence and attempts

Build:

```text
evidence add
attempt start/handoff
task evidence links
close evidence enforcement
```

Exit:

```text
task close requires proof, but proof does not bloat task body.
```

### Phase 4: Branch guards

Build:

```text
control branch config
protected field validation
pre-commit optional hook
CI branch policy
feature-branch refusal for status mutations
```

Exit:

```text
parallel agents cannot create task status merge hell.
```

### Phase 5: Packets and dashboards

Build:

```text
brief
packet --for agent
packet --for reviewer
dashboard build
Obsidian-friendly generated tables/Bases
```

Exit:

```text
humans scan; agents execute; reviewers verify.
```

### Phase 6: Runtime leases

Build:

```text
local lease backend
claim/heartbeat/release
active run dashboard
stale lease detection
```

Exit:

```text
one task is not accidentally run by five agents.
```

### Phase 7: Git state branch / future sync

Build:

```text
tusker/state backend
CAS object abstraction
remote-store interface
```

Exit:

```text
small teams can coordinate without moving off repo-local markdown.
```

---

## 28. The Hard Rules to Put in the Agent Skill

Add this to the Tusker operator skill:

```md
# Tusker V7 Agent Rules

You may read all Tusker work records.

You may add:
- evidence records
- attempt summaries
- inbox proposals
- documentation updates
- domain knowledge updates
- decision proposals

You must not directly edit protected task/gate state fields from an implementation branch:
- status
- readiness
- next_owner
- next_action
- accepted_by
- accepted_at
- closed_at
- state_rev

Finish contract:
1. Add evidence covering the acceptance items.
2. Add/update the attempt summary.
3. Open or update PR when the workflow uses PRs.
4. Prefer `tusker finish <task-id> --summary "<what changed and where proof lives>"`.
5. If using lower-level commands, run `tusker attempt handoff <task-id>` or the repo's handoff command.
6. If implementation is complete and no blocker remains, request review:
   - control branch or explicit local mode: `tusker status <task-id> review --reason "Ready for independent review."`
   - implementation branch: `tusker propose status <task-id> --status review --reason "Ready for independent review."`
7. If blocked on human input, credentials, external setup, or another dependency, create a blocking gate when policy permits (`tusker new gate --blocks <task-id> ...`) or propose one (`tusker propose create_gate <task-id> ...`). Include owner, action, verification, and why the agent cannot do it. For contradictory or unusable specs, use a human-owned decision gate with the agent's suggested resolution.
8. Never leave completed implementation work in `ready`, `active`, or `rework` with only an attempt handoff. Handoff is attempt state; task review state is the review queue.
9. Do not mark the task done unless running on a configured control branch and close policy permits it.

Never paste raw logs into task bodies.
Never add Work Log sections.
Never store secrets.
```

---

## 29. Appendix: Minimal `tusker.yaml`

```yaml
schema: tusker.config/v1
project_id: tusker
project_name: Tusker

storage:
  root: tusker
  generated_root: tusker/_generated
  evidence_root: tusker/evidence
  events_root: tusker/events
  attempts_root: tusker/attempts

branches:
  default_branch: main
  control:
    - main
  state_branch: tusker/state
  implementation_patterns:
    - "agent/*"
    - "task/*"
    - "feature/*"

validation:
  task_body_warn_lines: 120
  task_body_fail_lines: 220
  require_acceptance_proof: true
  forbid_work_log_section: true
  forbid_raw_logs_in_task: true
  protect_state_fields: true

runtime:
  lease_backend: local
  lease_ttl_minutes: 120

orchestration:
  workspace:
    kind: git_worktree
    root: "../.tusker-worktrees"
    branch_pattern: "agent/{{task_id}}"

close_policy:
  low:
    required_acceptor: reviewer_agent
  medium:
    required_acceptor: reviewer_agent
  high:
    required_acceptor: human
  critical:
    required_acceptor: human
```

---

## 30. Appendix: Acceptance Criteria Table Format

Preferred:

```md
## Acceptance

| ID | Outcome | Proof |
|---|---|---|
| A1 | User can connect provider through OAuth. | Screenshot + provider readiness endpoint |
| A2 | Missing credentials show actionable setup error. | Automated test |
| A3 | Live paid provider probe requires approval. | Gate + automated guard test |
```

Acceptable for tiny tasks:

```md
## Acceptance

- [ ] A1: Missing credentials show actionable setup error. Proof: focused automated test.
- [ ] A2: Docs updated. Proof: link to changed runbook.
```

Rejected:

```md
## Acceptance

- [ ] Works
- [ ] Tests pass
```

That is not acceptance criteria. That is vibes in a trench coat.
