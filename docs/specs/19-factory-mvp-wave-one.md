---
capsule:
  what: "Plans one inert MVP import and one separately approved, one-task shadow/staging dogfood observation."
  use_when:
    - "Preparing or reviewing the first opt-in daemon dogfood wave and its exact execution boundary."
  skip_when:
    - "Enabling automation, arming a wave, starting a daemon, promoting a ref, releasing, or spending."
---

# Factory MVP wave one

Status: proposed delivery contract
Date: 2026-07-26

## Outcome

Produce one durable operations report from the first opt-in daemon dogfood
run. Plan authoring, doctor, and import dry-run are inert. They neither enable
the project nor create execution authority. A later run may occur only after
separate explicit human approval has enabled the authorized project, armed
`W-0004`, and started the managed daemon. The daemon then dispatches exactly
`ORC-T-0050` once with the configured `implementation-terra` profile (Terra,
medium effort) for a shadow/staging-only observation.

## Requirements

| ID | Outcome |
| --- | --- |
| R1 | The worker-owned report proves the landed operations projection and focused E2E checks remain green during one authorized Terra-medium implementation attempt, and records the configured bounded reviewer policy as an expected later boundary. |
| R2 | The worker-owned report captures the approved project enablement, `W-0004` arm, managed-daemon start, one-task dispatch, and configured reviewer policy alongside read-only operations, automation, daemon, and wave state. |
| R3 | The report proves no unrelated claim/project, `all_eligible` widening, default-ref movement, provider credential use, release, or extra implementation/triage/release/provider attempt occurred. |

## Constraints and non-goals

- Exactly one low-risk task; concurrency is one; the implementation runner is
  Terra at medium effort. Normal configured bounded reviewer attempt(s) remain
  enabled and are not a second implementation attempt. The worker report
  records their configured bounded policy, not a reviewer result that has not
  happened yet.
- The run is shadow/staging only. The legacy conductor remains authoritative.
  During the worker's report/check window, both `main` and
  `integration/W-0004` must remain unchanged. A coordinator's separate
  post-run cutover check may observe one normal staging-completion advance of
  `integration/W-0004`; that is runbook success evidence, never a task
  prerequisite or self-attestation. `main` must not move.
- This plan has no dependencies or human gates. Separate human approvals are
  prerequisites for one project enablement, one `W-0004` arm, and one daemon
  start; the task observes those facts and does not perform them.
- Binary installation is a separate pre-run approval and is never task work.
  Use the repository-built `go run ./cmd/tusker ...` command surface while the
  installed binary is stale.
- No provider credential, default-ref promotion, release, spend, broad
  `all_eligible` dispatch, additional project, extra implementation/triage/
  release/provider attempt, or runtime feature change is in scope.

## Acceptance and verification

The delivery task must create `docs/reports/factory-mvp-first-run.md` with the
exact command results, pre/post active-run and ref snapshots, the approved
event tuple, and all read-only preflight surfaces. It must refuse to proceed
unless a fresh inert dry-run still maps
`capture-first-run-readiness -> ORC-T-0050` and `W-0004`.

```sh
go test ./cmd/tusker -run '^TestFactoryOperationsProjection$' -count=1
go test ./cmd/tusker -run '^TestFactoryExecutionControl$' -count=1
```

The report must record repository-built output from `setup doctor`, strict
skill doctor, `validate`, projects list, factory operations, automation
status/queue, daemon/service status, and `W-0004` brief/preflight. The report
must capture the preflight JSON and independently assert `ok:true`; a JSON-mode
command exit alone is not proof. A known
pre-existing red `validate` result is a recorded blocker, not false green proof
or permission to repair/widen scope. The active-run snapshots must prove only
the approved project and `ORC-T-0050` were claimed and distinguish its Terra
implementation attempt from the configured reviewer policy; the worker refuses cleanly
if `integration/W-0004` is absent and its ref snapshots prove both it and
`main` did not move during worker work. A separate coordinator post-run check,
not this task, may later record one normal `integration/W-0004` staging advance.

## Coordinator post-run check

After the worker has submitted and normal review/staging has occurred, a
coordinator performs one read-only check. It records the actual reviewer
attempt count, verdict, and completion state, then records the staging result
and any normal `integration/W-0004` advance while confirming `main` remains
unchanged. This check does not edit the reviewed task artifact
`docs/reports/factory-mvp-first-run.md`, does not create another attempt, and
does not enable, arm, start, release, promote, spend, or configure a provider.
