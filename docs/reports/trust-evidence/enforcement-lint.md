# Enforcement and lint evidence

Status: focused enforcement validation passed; advisory lint remains a separate
cross-entry check.

Hard enforcement now rejects unknown `proof_required` values at task creation
and validation through the proof lane's authoritative
`v7KnownProofRequirement` predicate. Evidence validation also rejects malformed
present category/facts, source revision, and artifact identity fields; accepted
copied artifacts require a current fingerprint. This blocks false proof and
integrity defects without turning prose quality into a release gate.

The existing top-layer lint remains scoped and advisory by default. It becomes
an error only when a demanding task is marked ready. Read-only discovery does
not consume warning debt, and a routine change does not require a blanket repo
lint or an extra human approval.

The focused enforcement tests passed on local macOS arm64:

```sh
scripts/with-validation-lock.sh -- go test ./cmd/tusker -run '^(TestTrustEnforcementLintRejectsUnknownProofRequirement|TestTrustEnforcementLintRejectsMalformedEvidenceIdentity)$' -count=1 -v
```

Result: PASS (1.099s).

Remaining A4 evidence is the existing valid routine change with unrelated
warning debt; it must be reported from an actual cross-entry execution rather
than a wrapper test.
