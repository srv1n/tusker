---
title: "Native human-receipt test-contract cleanup"
status: "source migrated; Go validation pending shared source coherence"
---

# Native human-receipt test-contract cleanup

## Replaced assumptions

- A raw `--by human:*` actor could satisfy a human-owned gate.
- A Serve gate request with evidence could complete a human action.
- `TestTrustStateIntegrity` was a distinct test, rather than a selector wrapper.

## Current invariants

- Human-owned satisfy, waive, and obsolete transitions require a native signed receipt bound to the current gate material and action.
- Positive legacy fixtures call `gateV7TransitionWithTrustedHumanReceiptForTest`, which only reaches the production post-verifier seam. `TestTrustHumanReceipts` owns signature, challenge, stale, replay, forged, and HTTP-submit verification.
- Raw direct, CLI, and Serve human actors remain refused with `HUMAN_CONTROL_RECEIPT_REQUIRED`; the Serve projection test then verifies the trusted transition produces the actual satisfied gate state.
- The nine state checks are selected directly; their historical aggregate evidence is retained separately.

## Pending grouped check

```sh
go test ./cmd/tusker -run '^(TestTrustHumanReceipts|TestV7TaskGateEvidenceAttemptReconcileFlow|TestV7GateControlEagerlyReconcilesTaskProjectionAndDashboards|TestV7CloseoutFingerprintInvalidatesGateChange|TestV7VerificationGateCanSatisfyManualProofRequirement|TestCrossScopeGateOrdering|TestServeHumanActionContractAndReviewProjection|TestServeSnapshotCacheReusesBuildAndInvalidatesPerProject|TestServeSnapshotIdenticalRefreshEmitsNoProjectionEvent|TestV7TargetedReconcileRepairsOnlySelectedTask|TestV7ReconcileDryRunEnumeratesInvalidStateRevsWithoutWrites|TestV7ReconcileRepairsStaleObjectStateRevAndEmitsEvent|TestV7SaveCASRejectsOnDiskBodyEditWithStaleStateRev|TestV7DocumentCASConcurrentProcessesAllowExactlyOneWriter|TestV7DocumentCASAtomicFailuresPreserveOriginal|TestV7DocumentCASParentDirectorySyncFailureIsReportedAfterRename)$' -count=1
```

Not run here: root held all Go builds and tests until the shared source window is coherent.
