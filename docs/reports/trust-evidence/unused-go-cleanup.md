# Unused Go cleanup

Date: 2026-09-05

Removed the retired `active_spend_monotonic` sentinel compatibility path.
It had no default configuration or evaluator, and its only Go caller was a
test asserting that obsolete workflow configuration is silently ignored. A
workflow that still names it now follows the existing unknown-check failure
path instead of being silently accepted.

`strict_v2_proof_authority/v1` remains: delivery import and start currently
validate it, even though it is unavailable. `RouteFactoryIntake` is already
absent from the current tree.

Validation pending the shared source-coherence window: `go test ./cmd/tusker
-run 'TestSentinel' -count=1`.
