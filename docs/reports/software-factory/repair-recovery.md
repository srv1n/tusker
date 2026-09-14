# W-0027 repair and recovery

Date: 2026-09-14

This report records the bounded FLW-T-0053 repair path. It reuses the existing
external-loop event ledger, runtime settings, run/attempt state, and supervisor
decision table. It adds no scheduler, proof store, provider adapter, or task
front matter.

## Durable transitions and counters

External-loop admission now performs effective-cap resolution, event lookup,
counter calculation, cap enforcement, and event insertion in one runtime-store
transaction. The effective caps and the first admission timestamp are
persisted under the existing `daemon_settings` authority, keyed by project and
task record. A later restart uses those caps and the original wall-clock start
unless an explicit command override is supplied. A partial CLI override merges
only the named positive fields; unmentioned persisted caps cannot be silently
relaxed. The persisted `WallClockTimeoutHours` is measured from that first
admission, including after a controller restart.

The idempotency identity is the stable transition tuple
`stage/action/job_id/attempt_id/work_revision/material_fingerprint`; human reason
text is payload/context and does not create a second continuation. A changed
revision or material fingerprint therefore requires a fresh admission instead
of inheriting an old provider result. The transaction projects counters before
insertion, so concurrent admissions cannot spend the same repair slot.

## Recovery boundaries

Collection is an admission checkpoint, not proof that the requested effect was
acknowledged. On restart, terminal collection actions remain handled, while a
collected apply/review continuation is eligible for reconciliation until the
run/attempt path records the effect. A collected close is retried until the
canonical task close effect is visible. This removes the previous blanket job
suppression that could lose a patch, targeted review, or close after a crash.

Cap exhaustion or another explicit external-loop block records one durable
`stop_for_human` supervisor decision correlated to the external event ID. The
decision identity is deterministic over the transition facts, so replay repairs
an absent decision without creating duplicate requests. The existing scheduler
continues to evaluate unrelated eligible work independently.
Caller-supplied contract or route blockers take the same escalation path while
the event payload retains the requested incomplete action for operator repair.

The durable fault points are explicit: an event may commit before the run
acknowledges its effect, a repair event may commit before continuation dispatch,
and an escalation event may commit before the supervisor decision acknowledgement.
The first two reconcile only from a released terminal runtime checkpoint; the
third replays the deterministic decision upsert. No live daemon/provider fault
injection was run. Admission and caps are keyed by project and task record, so
unrelated scheduler work is not stopped by this path; a separate unrelated-task
live control was not claimed as evidence.

Typed blocking review findings are retained in the existing event payload and
their stable IDs are included in the next external repair launch context. This
lets the repair worker target the finding and lets a later review bind closure
to the exact repaired material supplied by FLW-T-0052.

Structured external findings are canonical JSON, validated against the existing
typed finding contract; an object that cannot be parsed is a progression
blocker. A pre-claim dispatch checkpoint preserves the newly published runner
and work revision while retaining the prior lease/outcome snapshot, so a
restart can claim the intent once without duplicating the attempt.

Codex Cloud start writes its provider task identity to the existing durable
event ledger before returning. If the daemon dies before copying that result
into the run and attempt rows, recovery adopts the exact task ID in one
lease-fenced transaction; a missing, malformed, conflicting, or unpersistable
checkpoint fails closed and never redispatches the provider operation. A second
recovery pass is idempotent and proceeds only to reconcile that same task.

External review packets are accepted only as the authoritative
`tusker.review-result/v3` DTO. The collector normalizes structured findings and
rejects missing or malformed authority fields, pass results carrying findings,
and advisory-only `changes_requested` results. Before an action is admitted it
binds task, attempt, source, policy, declared scope, and the exact workspace
material through the existing `reviewImplementationParent` and
`reviewAttemptMaterialFingerprint` authorities. The event payload carries the
same exact material identity; stale or arbitrary provider material cannot drive
repair or close. Provider-supplied risk metadata is ignored; auto-close falls
back to the canonical task risk.

The daemon's review caller only suppresses a collected close after the event is
successful, matches the current work revision and workspace material, and the
canonical task close effect is visible (`done`, `cancelled`, `superseded`, or a
close timestamp). Historical, failed, or prior-material close admissions remain
eligible for review/reconciliation.

## Focused evidence

```text
GOCACHE=/private/tmp/tusker-w27-repair-gocache \
GOTMPDIR=/private/tmp/tusker-w27-repair-gotmp \
TUSKER_VALIDATION_LOCK_DIR=/private/tmp/tusker-w27-repair-locks \
go test ./cmd/tusker -run '^TestSoftwareFactoryRepair' -count=1 -v
```

PASS: fifteen top-level tests (twenty including the close-suppression subcases)
covering persisted effective caps and reason-independent identity, concurrent
repair-cap admission, restart reconciliation of unacknowledged apply/close
effects, deterministic escalation IDs, typed finding identity propagation,
unacknowledged escalation checkpointing and action variants,
revision/material amendment admission, canonical structured-finding parsing
with malformed-input blocking, v3 external-review authority and exact
workspace-material binding through the collector/controller, durable wall-clock
timeout and first-admission timestamp, daemon caller close suppression,
empty-provider-identity admission and legacy replay refusal, and pre-claim
intent restart/retry exactly once. The Codex Cloud provider-dispatch
crash-window case adopts one durable task ID and proves a restarted controller
does not call Start again. The first-admission case also
checks that a partial override merges with configured caps without replacing
unmentioned fields.

An earlier twelve-test selection passed under the race detector. The final
fifteen-test race rerun is pending: concurrent strict-proof edits left
`accept_cmd.go` calling `v7VerificationReceiptRequirementMissing` with its old
signature, so `go test -race` stopped at compile-time vet before this selection.

The focused source suite is not installed-binary, live-provider, daemon
dispatch, external-architect delivery, or human-acceptance evidence. The
independent routing and review-result selections used for compatibility checks
pass; the broader external-loop regression selection is not a clean PASS at
this source snapshot: five selected tests fail. The first reports `json ok:
expected true, got false`; a daemon selection separately exposes the fixture's
stored task-contract drift, and the remaining failures report the configured
fallback runner instead of `chatgpt-browser` or the same setup drift. These
are configuration/fixture issues outside this repair path; no live provider or
resident daemon was launched.

The affected selection was:

```text
GOCACHE=/private/tmp/tusker-w27-repair-gocache \
GOTMPDIR=/private/tmp/tusker-w27-repair-gotmp \
TUSKER_VALIDATION_LOCK_DIR=/private/tmp/tusker-w27-repair-locks \
go test ./cmd/tusker -run 'Test(Automation.*External|Daemon.*External|DispatchExternal)' -count=1 -v
```

First actionable failure: `TestAutomationAdvanceExternalCollectsRecordsPolicyAndIsIdempotent`
reported `json ok: expected true, got false`. The remaining four failures were
the same fixture/setup drift (including an explicit stored contract blocker in
the daemon collect test or configured fallback runner); the close and
concurrent-lease checks passed. No live daemon, provider, installation, or
live-vault evidence was claimed.
