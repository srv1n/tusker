---
title: "Everyday workflow documentation report"
subject: skills-docs-workflows-report
part_of: tasks-and-proof
status: report
---

# Everyday workflow documentation report

This report records the documentation work for `FLW-T-0032`. The edited guides
explain preparation, execution, waiting, proof, review, and completion as
separate actions.

## Acceptance map

| Acceptance | Result | Evidence |
| --- | --- | --- |
| A1 | met | `factory-intake.md` links to `delivery-and-waves.md`; the three guides lead with reader outcomes and separate prepare, start, wait, review, and close. |
| A2 | met | `tasks-and-proof.md` and `proof-and-closeout.md` author command rows as `pending` and assign `pass` or `fail` to the executor. |
| A3 | met | The guides retain exact states, identifiers, authority limits, evidence rules, unknown states, and the distinction between interactive work and automation. |
| A4 | partially met | The normal and refusal routes below were checked against help, source, and a read-only readiness probe. Independent review remains unrecorded. |

## Before and after

Before:

> Use `tusker verify add`. Store the exact command or manual check. Store
> `pass` or `fail`.

After:

> Use `tusker verify add` to store the exact command and link it to acceptance
> row IDs. A command row starts as `pending`.

The old text gave the task author an outcome that the public command refuses.
The new text separates authoring from execution and tells the reader what to do
after a refusal.

## Command and source comparison

Inspection date: 2026-09-11. These are source/help observations, not a live
coordination qualification.

| Route | Command or source | Expected | Observed | Result |
| --- | --- | --- | --- | --- |
| Normal | `tusker verify --help` | Command rows start pending; the executor records pass or fail. | Help states that rule and lists `pending`, `blocked`, `skipped`, and `waived` for `verify add`. | pass |
| Normal | `cmd/tusker/v7_verification_execution.go` and `cmd/tusker/v7_verification_execution_test.go` | A qualified executor runs a pending command and records its result. | Source checks reviewer authority; focused tests cover pending execution and stored outcomes. | pass |
| Refusal | `tusker verify add ... --result pass` compared with `cmd/tusker/v7_proof_cmd.go` | Public authoring refuses an executor-owned result. | Help omits `pass` and `fail`; source rejects both public values without mutating the task. | pass |
| Refusal | `tusker work readiness FLW-T-0032 --lane execute --json` | A malformed readiness call is rejected without changing state. | The installed binary returned `MISSING_ARG --id`; no task or runtime state changed. | fail |
| Refusal | `tusker work readiness --id FLW-T-0032 --lane execute --json` | A non-ready task reports its blocker and remedy. | The installed binary reported `task_not_ready`, reason `Task status is backlog.`, and remedy `Move the task to ready or rework before starting interactive work.` No state changed. | pass |

The malformed refusal row is retained because a failed check is not a pass.

## Verification

| Check | Host | Result | Notes |
| --- | --- | --- | --- |
| `tusker docs check --json` | local workspace | pass | `valid: true`, 40 documents, no issues. Structural check only. |
| `go test ./cmd/tusker -run 'TestTrustCliGuideUsesExecutorRecordedCommandProof\|TestAcceptExecutesCommandRows\|TestReviewRequestAllowsPendingCommandRows\|TestVerifyAddCannotForgeCommandExecutionResult' -count=1` | local workspace | pass | Four focused tests passed in one package. |
| `git diff --check -- docs/system/factory-intake.md docs/system/tasks-and-proof.md docs/system/proof-and-closeout.md` | local workspace | pass | No whitespace errors in tracked owned documents. |

## Meaning review

Self-review found no claim that preparation starts execution, waiting means
completion, submission means acceptance, or a structural check grants review
authority. An independent reviewer still needs to follow the prepare-work and
record-command-proof examples against the named binary or an isolated fixture.

## Wave-guide wording for `ACO-T-0007`

Consider adding one outcome-led sentence near "Plan phases": "Import prepares
held work; only a confirmed start and current wave authorization make it
eligible for unattended execution." This report does not edit
`docs/system/delivery-and-waves.md`.

## Unqualified behavior

No live daemon, wave, provider, agent-message route, active fixture, or
architect continuation was exercised. The report makes no shipped-runtime
claim for those paths. No application build was run for this prose-only change.
