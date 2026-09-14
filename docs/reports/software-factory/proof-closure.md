# W-0027 proof closure

Date: 2026-09-14

This report records the narrow software-factory acceptance extension for
FLW-T-0052. It closes the proof, review, and completion authority gaps without
adding a new proof store, scheduler, delivery plan, or mandatory front-matter
requirement.

## Production verification receipts

The unproduced `tusker.proof-contract/v1`, `tusker.proof-results/v1`, and
strict-lineage projection was removed. The canonical task `## Verification`
ledger is the only proof path.

The shared command executor writes a `tusker.verification-receipt/v1` token
into the row notes. It binds the current task contract fingerprint, work
revision, source revision, exact scoped material fingerprint, row fingerprint,
exit code, and bounded-output digest. `computeV7ProofReport`, accept, close,
direct-wave review, and background Start all reject a passed command row whose
receipt is absent or no longer matches the current task contract/work/source
or a freshly re-hashed scoped source tree. A task-level proof waiver cannot
make an executable pending, failed, waived, missing, or stale row authoritative.

Filtered Go commands using `-run` or `-run=` additionally require positive
match evidence from the actual captured verbose or JSON output. Exit zero with
no `=== RUN` or JSON run event is recorded as failure with `match_count=0`.
Ordinary non-test commands do not invent a match count.

Material amendments conservatively reset prior passed or waived verification
rows to pending while retaining the historical note. The next successful gate
execution appends a new current receipt. The external-review close path cannot
promote a pending command row; only the shared executor may do that.

## Structured reviewer findings

Authoritative v3 `changes_requested` results use the existing `findings` list,
with each entry encoded as one JSON object using
`schema: tusker.reviewer-finding/v1`:

```json
{"schema":"tusker.reviewer-finding/v1","id":"F-001","kind":"blocking","acceptance":["A1"],"evidence":["receipt-1"],"consequence":"acceptance is unproven","closure_condition":"repair the implementation and re-run A1","repair_scope":"material","material_fingerprint":"sha256:<current-material>"}
```

Blocking findings require a stable `id`, at least one `acceptance` reference,
at least one `evidence` reference, `consequence`, `closure_condition`, a
`repair_scope`, and the reviewed `material_fingerprint`. `repair_scope` is
`material` when source/implementation/artifact repair is required and `proof`
when only verification or the task ledger must be refreshed. Omitted scope on
legacy findings defaults to `material`. The persisted v3 consumer rechecks
that the finding fingerprint equals the result's exact material. A v3 result
with only advisory findings cannot request rework; advisory findings never
block by themselves. Existing v1/v2 records remain readable audit transport.

Producer: `tusker review submit` / the existing worker proposal transport.
Consumer: `normalizeReviewResult`, `SaveReviewResult`, and the existing
completion reactor hand-back. The generated reviewer-findings section carries
the exact JSON record, so an implementer can reference its stable ID during
repair and a later independent review can bind closure to the exact current
material. Proof-scoped findings may use the same implementation material after
the proof/task ledger is refreshed.

## Exact review material and closure

Every authoritative `tusker.review-result/v3` row, including `pass`,
`changes_requested`, and `blocked`, carries an exact `material_fingerprint`.
The current workspace implementation emits the lower-case 64-hex tree hash;
the persisted validator also accepts the existing `sha256:<64-hex>` identity
form. `review submit` computes the identity from the linked implementation
attempt, `SaveReviewResult` persists and fingerprints it, and the completion
reactor recomputes it from the durable execute/review attempt binding before
using the result. Worker proposals are first normalized as authority-less v2
transport; the daemon then binds the exact attempt material, upgrades the
result to v3, and performs the final closure/finding validation. A missing,
malformed, stale, or mismatched identity is a repair condition, not a
completion result. Hand-run reviews retain the existing independent-actor
check; the authoritative dispatched lane additionally binds both exact worker
policies.

When a later independent review returns `pass`, its `closed_findings` list
contains `tusker.reviewer-finding-closure/v1` objects:

```json
{"schema":"tusker.reviewer-finding-closure/v1","id":"F-001","closure_condition":"repair the implementation and re-run A1","evidence":["repair-receipt"],"material_fingerprint":"sha256:<current-material>"}
```

The producer is `tusker review submit --closure <JSON>` (or the existing
worker proposal transport). The existing `ReviewResult` transaction and
`SaveReviewResult` retain the attestation; no second closure store is added.
The completion reactor reads prior immutable v3 results for the same task and
work revision, requires every prior blocking ID and closure condition to be
explicitly attested, and requires a later independent review attempt. The
closure fingerprint must equal the current pass result's exact material. A
material-scoped finding additionally requires changed material; a proof-scoped
finding may close on the same material after its proof/task ledger is
refreshed. Unknown IDs, omitted IDs, self-closure, stale closure material,
material-scoped same-material review, or condition drift all remain blocked.

## Focused evidence

The focused regression suite covers the real executor's zero-match failure,
current receipt identity, stale-contract rejection, authoritative CLI
submission and persistence, worker-proposal closure binding, exact material
drift at completion, equal-timestamp durable finding closure, and durable
independent finding closure. The deterministic completion matrix also
exercises real hand-back/close transactions and legacy v1/v2 review-result
compatibility. The required command is:

```text
GOCACHE=/private/tmp/tusker-w27-proof-gocache \
TUSKER_VALIDATION_LOCK_DIR=/private/tmp/tusker-w27-proof-locks \
go test ./cmd/tusker -run '^TestSoftwareFactoryProof' -count=1 -v
```

This is source-level proof only. It does not claim an installed Tusker binary,
live provider, daemon dispatch, external integration, or human acceptance.
