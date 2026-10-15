---
title: "Proof and closeout"
subject: proof-and-closeout
part_of: overview
status: canonical
describes: [cmd/tusker/v7_proof_cmd.go, cmd/tusker/lifecycle_cmd.go, cmd/tusker/work_recovery.go, cmd/tusker/v7_closeout_cmd.go, cmd/tusker/v7_close_authority.go, cmd/tusker/review_result.go, internal/v7policy/close.go]
read_when: "Choosing evidence, checking acceptance coverage, or closing a task."
skip_when: "Planning dependencies or choosing a provider."
---

# Proof and closeout

Use proof to show which acceptance outcomes the result meets. A check result,
review, and completed task are different things.

## Proof modes

New tasks use inline proof by default. A critical-risk task uses audit proof by
default. The supported modes are `none`, `inline`, `card`, `artifact`, and
`audit`.

Inline proof stores check results on the task. The other evidence-bearing modes
can add evidence files. The default evidence budgets are zero for inline, one
for card, three for artifact, and five for audit.

## Author command proof as pending

Use `tusker edit` to store the exact command and link it to acceptance
row IDs. A command row starts as `pending`:

```sh
tusker edit APP-T-0001 \
  --covers A1,A2 \
  --check "command: go test ./cmd/tusker -run TestFocused -count=1" \
  --result pending \
  --note "Focused task behavior."
```

Expected result: Tusker records the row without running the command. During a
valid pass review, the verification executor runs pending command rows in the
bound implementation workspace and records `pass` or `fail`.

`tusker edit --verify` also accepts `blocked`, `skipped`, and `waived` when those states
are true. A blocked row requires `--blocked-by`. None of these states counts as
a passing command. A public caller cannot author `pass` or `fail`; Tusker
refuses the row instead of trusting a claimed result.

Raw logs belong in `.tusker/scratch/<TASK-ID>/`. Scratch is temporary. Move a
durable artifact to an evidence record before close.

Screenshot, video, and trace requirements need matching evidence types. A
filename extension cannot turn a test result into visual evidence. The wave
brief displays the evidence type actually recorded, not the artifact type
promised by the task.

An artifact contract is complete only when current accepted evidence has the
contract's matching type, covers each promised acceptance row, and retains a
readable copied artifact with its recorded fingerprint. If the task records a
source revision, the evidence records the same revision. A replaced artifact,
an external link, or old evidence without an identity stays visible but cannot
complete the contract.

Use `--proof-category visual|performance|backend|migration` with compact
`--proof-facts key=value,...` only when that category is required. Visual
evidence records a before/after pair or `baseline=new_ui`; performance records
both values and matching workloads; backend records an observable and a
negative case; migration records preservation, interruption, and recovery.
These fields establish structure and provenance. They do not judge image
quality or substitute for an explicit acceptance decision.

## Review

Review answers three plain questions: does the result meet every acceptance
row, does the proof support those outcomes, and are all gates clear? A valid
review is independent of the implementation session and is bound to the exact
task, work, implementation, proof, gate, and material revisions.

The valid review results are pass, changes requested, and blocked. A later
change makes a result stale when its bound facts change, including a changed
artifact fingerprint or a changed proof snapshot. The implementation attempt
and reviewer attempt remain separate authorities.

For a live interactive review, start the review lane:

```sh
tusker review APP-T-0001 --by reviewer:agent
```

Expected result: the returned packet carries the review attempt, the
linked completed implementation attempt and actor, current proof and gate
fingerprints, and a material fingerprint of the implementation workspace. Its
workspace is that exact implementation workspace, rather than a fresh `HEAD`
checkout that would omit an uncommitted diff. The fingerprint covers tracked
and untracked files in the task's declared owned paths, generated outputs,
knowledge nodes, and repository-local spec references. The stored scope is part
of the end-state identity and cannot be narrowed later. Tusker's mutable
control-plane records are excluded. The route and close consumer recheck that
scope against the implementation workspace, so later relevant dirty edits stale
the receipt while unrelated parallel-task changes do not. Daemon execution and
proposal harvest use the same server-derived scope and reject a missing scope;
whole-project gate-ledger checks retain their separate full-tree semantics. Use
the packet's `next` command to submit the receipt. The path rejects a reviewer
who is the
implementation-session actor. It also refuses a stale task, proof, gate,
implementation, or material fingerprint. Use the packet's reported next
command after fixing the named problem. A structural check does not establish
human authority or subjective quality.

A pass must cover every acceptance ID exactly and have eligible objective
proof with no open gates. `changes_requested` needs an actionable finding.
`blocked` needs a machine, infrastructure, or human blocker; a human blocker
also needs an open human-owned gate. Recording any verdict does not merge,
land, move a ref, or close the task.

## Recording finished work

`tusker done` is the one way to record finished work. When the caller holds the
task's live work session, `done` submits it. Work done any other way
(parallel agents with one central gate, a different harness, a teammate, or
hand edits) is recorded after it is committed, without a claim:

```sh
tusker done APP-T-0001 APP-T-0002 --by agent:<name>
tusker done --wave W-0001 --by agent:<name>
tusker done --since <git-ref> --by agent:<name>
```

`--since` selects open tasks whose `owned_paths` changed between the ref and
`HEAD`, so the agent that did the work needs no trailer or Tusker integration.
For each task, Tusker reruns the command rows in the registered checkout. It
reuses receipts that still match the material. It then runs the
`adopt_completed` transition, which records the implementation as
"external/unknown". Dependencies are adopted before their dependents.

Every task receives its own contract-bound receipt. Identical pending shell
commands execute for each task: they can read tracker state, ignored files, or
external inputs that a repository hash does not cover. Existing material-bound
receipts retain their normal validity checks; arbitrary command observations
are not cached across tasks.

The actor decides how far a task moves:

| Actor | Passing task moves to |
| --- | --- |
| `human:` or `operator:` | `done`, unless the contract requires `independent_review` |
| Agent inside an armed wave | `done`, unless the contract requires `independent_review` |
| Any other agent | `review`; a person or an independent review closes it |

An agent never closes through `done`, because Tusker observed the checks
but no one accepted the result. A dependency in `review` lets a dependent
reach `review`; close still requires every dependency to be `done`. Human
gates and open blocking gates still refuse. A person running `tusker done` on
a task already in review closes it after the close checks below.

Each refusal names one fix. The usual causes are a failing check, a drifted
contract (`tusker edit <ID> --body-file <path> --by <actor>`), a live
claim on the task, or a malformed Verification row. A `|` inside a Check cell
must be written `\|`; otherwise the table splits the command. Readiness also
reports such rows as blockers.

Quiet interactive leases are reclaimable by a subsequent claim or `done` once
24 hours have passed since lease expiry and the recorded process is gone.
Live or uncertain process identities retain their ownership fence. Reclaim
releases ownership without deleting checkout files or scheduling a retry.
Before that deadline, an operator can release an abandoned session explicitly:

```sh
tusker runs retire APP-T-0001 --release --break-glass --by human:<name> --reason 'release abandoned session'
```

## Close checks

Close only after the current result has complete acceptance coverage, eligible
proof, clear gates, a valid review, and an acceptable repository state. The
default close policy needs a reviewer or a person. Risk alone does not add a
gate or evidence kind. Project configuration can add exact evidence or gate
rules.

Expected result: `tusker done APP-T-0001` marks the current reviewed result
done. If any required fact is missing or stale, close refuses and names the
remaining action. Fix that fact and retry. Do not treat a successful command,
an old review, or a submitted work session as completion.

When only a person can finish the task, closeout writes a bounded checkpoint
and sets `stop_until_human_response`. It does not invent approval.

## Code sources

- `cmd/tusker/v7_proof_cmd.go`
- `cmd/tusker/review_result.go`
- `cmd/tusker/v7_closeout_cmd.go`
- `cmd/tusker/v7_close_authority.go`
- `internal/v7policy/close.go`
