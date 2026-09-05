# Preflight evidence

Status: focused contract validation passed.

`status ready` now calls the same dispatch-blocker policy used by `next` and
daemon dispatch, before it writes. A placeholder contract therefore receives
the same actionable refusal on all three public paths. A task held only by a
dependency remains structurally valid but is not pickable. Delivery start keeps
its existing mutation-time fingerprint recheck and leaves records unchanged on
a stale confirmation.

Executed on local macOS arm64 at `03201019`:

```sh
scripts/with-validation-lock.sh -- go test ./cmd/tusker -run '^(TestDeliveryImportJSONReportsAllPreflightIssues|TestDeliveryStart)$' -count=1 -v
```

Result: PASS (20.278s after a 38s validation-lock wait).

This covers frozen plans, material races, stale confirmation, and rollback.
The table-driven public walkthrough also passed on local macOS arm64:

```sh
scripts/with-validation-lock.sh -- go test ./cmd/tusker -run '^TestTrustPreflight$' -count=1 -v
```

Result: PASS (4.708s). It exercises status-ready, next, daemon dispatch,
dependency waiting, and stale delivery start without a write from a read-only
refusal.
