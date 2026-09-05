---
title: "FLW-T-0016 review closeout evidence"
status: "focused regression passed"
---

# Review closeout evidence

Review snapshots include the current proof report. `TestTrustReviewCloseout`
captures that snapshot, replaces a fingerprinted artifact, and verifies that
the proof fingerprint changes. The prior review therefore cannot authorize
close after a meaningful artifact change.

The existing review result protocol still supplies the broader two paths:
implement → review → edit → close, and reject → rework → review → close. It
binds separate implementation and reviewer attempts, task/work/source
revisions, proof and gate fingerprints, and all acceptance IDs for a pass.

The native interactive route is `tusker work review <task-id> --by
reviewer:<name>`. It creates a real hand-owned review lane attempt and binds
its parent to the completed implementation attempt. The packet contains the
exact proof and gate fingerprints used by `review submit`, plus the completed
implementation workspace material fingerprint. Its workspace is the same
implementation workspace, avoiding a fresh checkout that would hide dirty
source. The recorded scope covers declared owned paths, generated outputs,
knowledge nodes, and local spec references; tracked and untracked changes in
that scope stale review, while unrelated parallel-task files do not. Mutable
Tusker records are excluded. Review submission and close recheck the immutable
scope. Submission also rejects
same-actor implementation/review provenance. Focused lifecycle regressions are
staged as `TestWorkSessionInteractiveReviewReceiptBindsImplementer` and
`TestWorkSessionInteractiveReviewRejectsMaterialDrift`,
`TestWorkSessionInteractiveReviewRejectsTrackedMaterialDrift`,
`TestWorkSessionInteractiveReviewRejectsMaterialScopeDrift`, and
`TestWorkSessionInteractiveReviewMaterialScopeIgnoresUnrelatedAndControlWrites`;
grouped Go validation is pending shared source-coherence repairs, so this report
does not claim that the new regressions passed yet.

Revision: `03201019308fbc533e6aeace9f8c612e8b2237aa` plus the dirty proof patch
Host: local `Darwin arm64`

Executed:

```sh
GOMAXPROCS=2 scripts/with-validation-lock.sh -- \
  go test -timeout=5m -p=1 -parallel=1 ./cmd/tusker \
  -run '^TestTrustReviewCloseout$' -count=1 -v
```

Result: PASS, `ok tusker/cmd/tusker 1.636s`.

Material SHA-256: `v7_proof_cmd.go`
`c896e273808db204232cec77fd55184951a871605c46984f4a27258a3f31d989`;
`trust_proof_contract_test.go`
`2bf380ed0a35f2bb6e2e7bda7e8110d7eba47c6b7964a26c7bfdd1a557279624`.

This source check does not create a human receipt, claim a human decision, or
replace installed-runtime acceptance.
