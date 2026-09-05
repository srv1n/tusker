---
title: "FLW-T-0007 state integrity evidence"
status: "historical aggregate passed; direct selector pending"
---

# State integrity evidence

Revision: `03201019308fbc533e6aeace9f8c612e8b2237aa` + dirty state-integrity patch
Host: local `Darwin arm64`

Historical material SHA-256 at the aggregate passing run:

| Path | SHA-256 |
| --- | --- |
| `cmd/tusker/commands_v7.go` | `0fd7b8034b001e7411e55d8f2691d4dbb40686d0c0258450d14e7a96c643c8f6` |
| `cmd/tusker/serve_command.go` | `e34ff3c2efe9e686d28e4870a3ba0856540199264bf812bf37b4100d05a6d41e` |
| `cmd/tusker/v7_document_write_test.go` | `612506dba94abd928cb641e51a59792c0ff88b08c3ef673721713c68ab91f17f` |
| `cmd/tusker/trust_state_integrity_test.go` | `50108c1eace94d3859ca5c3fe789ef7f6e437fb9139257b4a438c2aa3c7d5d63` (deleted pure aggregate) |

## Reproduction and repair

The audit names six failing state/snapshot tests (two snapshot and four state
revision cases), despite the task packet calling them seven. The required
adversarial interleaving is covered separately by the existing real-process
CAS regression, and interrupted writes by injected atomic-write failures.

Before the repair, the six audit-named tests failed: a state revision omitted
the Markdown body, so stale body edits appeared current; snapshot invalidation
discarded the cached entry instead of rebuilding it, suppressing both the
second build count and changed-projection event.

`v7StateRev` now covers frontmatter and canonical body. CAS and reconciliation
therefore operate on the exact file content read under the document lock.
Snapshot rebuild consumes its current invalidation before building, leaving a
new invalidation visible as a retry signal.

## Historical aggregate proof

```sh
GOMAXPROCS=2 scripts/with-validation-lock.sh -- \
  go test -timeout=5m -p=1 -parallel=1 ./cmd/tusker \
  -run '^TestTrustStateIntegrity$' -count=1 -v
```

Result: PASS, `ok tusker/cmd/tusker 4.650s`.

That historical aggregate executed:

- snapshot cache invalidation and changed-only projection events;
- targeted reconciliation isolation, stable dry-run scan, and stale
  task/attempt repair events;
- stale body CAS refusal;
- competing real-process CAS writers, with exactly one committed writer; and
- injected write/sync/close/rename interruption failures preserving the last
  committed document; and
- a post-rename parent-directory-sync failure followed by an idempotent retry
  with exact canonical bytes and no leaked temporary files.

The baseline failure log is `.tusker/scratch/FLW-T-0007/baseline-seven.log`.
The passing focused log is `.tusker/scratch/FLW-T-0007/trust-state-integrity-recovery.log`.

## Current direct selector

PENDING: the wrapper is deleted and the shared Go validation window is held.

```sh
GOMAXPROCS=2 scripts/with-validation-lock.sh -- \
  go test -timeout=5m -p=1 -parallel=1 ./cmd/tusker \
  -run '^(TestServeSnapshotCacheReusesBuildAndInvalidatesPerProject|TestServeSnapshotIdenticalRefreshEmitsNoProjectionEvent|TestV7TargetedReconcileRepairsOnlySelectedTask|TestV7ReconcileDryRunEnumeratesInvalidStateRevsWithoutWrites|TestV7ReconcileRepairsStaleObjectStateRevAndEmitsEvent|TestV7SaveCASRejectsOnDiskBodyEditWithStaleStateRev|TestV7DocumentCASConcurrentProcessesAllowExactlyOneWriter|TestV7DocumentCASAtomicFailuresPreserveOriginal|TestV7DocumentCASParentDirectorySyncFailureIsReportedAfterRename)$' -count=1 -v
```

## Acceptance status

Focused automated evidence covers A1, A2, and A3 for the tested mutations.
It does not establish installed-runtime or human acceptance; neither was run.
