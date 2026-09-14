# W-0025 completed-work review

Independent reviewer: Sol, medium reasoning. Scope: reported completed
FLW-T-0043, FLW-T-0044, FLW-T-0045 and FLW-T-0047, against
`.tusker/specs/direct-wave-authoring.md` and the implementation handoff.
This is a bounded source review of the current shared dirty checkout, not
attribution of every change to Devin. Remaining 0050/0048/0046/0049 behavior
is outside the completion claim. No product source or lifecycle records were
changed by this review. No production migration, provider or daemon was run.

## Verdict

Three tickets need correction before acceptance. The contact implementation
supports registration and truthful capability reporting; actual delivery to
pre-existing external conversations remains unsupported/unqualified.

| Ticket | Disposition |
|---|---|
| 0043 | Changes required: completed-contract mutation, dependency race, retry identity and event failure semantics. |
| 0044 | Changes required: omitted inconsistent wave membership; event publication semantics. |
| 0045 | Changes required at shared update/completion boundary: changed non-strict completed contracts still appear completed. Start locking/revalidation itself has useful coverage. |
| 0047 | Registration/protocol implementation present. No demonstrated external send/reply capability for Codex, Claude Code or Devin. This is not an accepted end-to-end messaging result. |

## Findings

### P1 — Changed completed tasks retain old completion

Location: `cmd/tusker/direct_authoring_cmd.go:125` and `:206`;
`cmd/tusker/direct_wave_authority.go:380`.

Task update accepts a new body and recomputes the contract fingerprint while
preserving terminal status and existing proof state. Wave review labels any
done task completed. Updating a completed non-strict task with its current
revision therefore leaves new unverified instructions marked Completed; task
Start refuses it as terminal. Use controlled amendment/rework semantics and
invalidate applicable completion/proof rather than merely recalculating a hash.
Do not conflate changing the durable contract with evidence that an immutable
attempt snapshot was overwritten; the confirmed problem is false completion.

Missing regression: complete a non-strict task, update its body/acceptance,
then inspect review and attempt Start. New material must not inherit completion.

### P1 — Dependency validation races with other task updates

Location: `cmd/tusker/direct_authoring_cmd.go:184` through `:207`;
`cmd/tusker/commands_v7.go:4470`.

Dependency graph validation happens before the material lock is acquired by
saveV7DocumentCAS. Per-document CAS protects each edited task, not the graph
read earlier. Concurrent A-depends-on-B and B-depends-on-A updates can both
validate the previous acyclic graph and commit different files successfully.
Hold the common material lock across graph read, validation and commit, using
the existing under-lock write authority.

Missing regression: deterministically interleave both graph edits and prove
one refuses without leaving a cycle. This finding is established by source
ordering; no new race reproduction was executed during this review.

### P1 — Migration inventory omits inconsistent wave membership

Location: `cmd/tusker/direct_wave_migration.go:424` through `:447`.

Wave conversion gathers tasks from wave.members. Standalone conversion skips
every legacy task with a nonempty wave field. A task referencing a missing
wave, or absent from its referenced wave's members, falls through both passes.
It receives no disposition or blocker. This is an inventory completeness bug,
not evidence of actual data deletion in this review; it makes later cutover
unsafe. Account for every legacy task exactly once, refusing ambiguous or
inconsistent associations explicitly.

Missing regressions: missing referenced wave; mismatched forward/reverse
membership; one task listed in competing waves. Every task must appear in a
conversion or explicit blocked disposition.

### P2 — Retry mappings depend on mutable member ordering

Location: `cmd/tusker/direct_authoring_cmd.go:637` through `:660`.

Identical request replay pairs original temporary task keys with the current
wave.members order, then action keys with current gate ordering. Membership
edits/reordering can return incorrect IDs for the original keys. Use immutable
operation receipt mappings, or verify identity and report drift. No maintained
delivery-plan object is needed to preserve a mutation receipt.

Missing regression: create a batch, alter member/gate ordering or remove a
member, retry the original request and verify original identity or refusal.

### P2 — Events are outside the claimed atomic transaction

Location: `cmd/tusker/direct_authoring_cmd.go:779` through `:794`;
`cmd/tusker/direct_wave_migration.go:243`.

Record publication commits before events are emitted individually. An event
write failure leaves durable records and partial events while the command
returns failure. The report claims no partial event graph. Include the required
events in the existing transaction or make post-commit event recovery durable
and explicit; returning ordinary failure after partial publication is not the
documented guarantee. Prefer reusing existing transactional mechanisms.

Missing regression: fail event publication after records commit, then retry or
restart and verify a truthful result with complete, nonduplicated event history.

## Verification and limitations

Fresh check run by the primary reviewer:

```sh
rtk proxy go test ./cmd/tusker -run '^(TestDirectWaveAuthoring|TestDirectWaveMigration|TestDirectWaveAuthority|TestDirectStartAuthority|TestExternalArchitectRouting)' -count=1
```

PASS, package execution 36.898s. The matching source contains 58 top-level test
functions: authoring 9, migration 19, authority/start 21, contacts 9. The command
emitted a package result, not individual test timing. Existing handoff counts
are older and differ. Initial sandbox execution could not access Go's cache;
the approved cache-enabled rerun passed.

Tests cover meaningful negative scenarios including rollback, stale material,
ownership, route failures, and protocol generation checks. Passing those tests
does not cover the five findings above. No new regression tests or fixes were
written. The handoff's nine directive/daemon failures and fingerprint failure
were not rerun or causally attributed; their supposed baseline origin remains
unverified. No full-suite, installed UI, live provider, or real-vault migration
qualification was performed.

## Assessing the Devin trial

This trial produced substantial implementation and useful focused tests. It
also needs correctness repairs at state, graph and migration boundaries before
the reported completion can be accepted. The shared tree and stabilization
history do not establish which agent introduced each defect.

The supplied reports contain no measured Devin token usage, monetary cost,
end-to-end implementation time or human supervision time. Test runtime is not
implementation latency. Consequently this review cannot calculate cost savings
or prove that Devin is more efficient than another model.

A reasonable next experiment is bounded Standard-tier work with an independent
review gate. Record worker usage/cost, wall time, review and repair cost, human
intervention, and whether acceptance passed. Compare total cost per accepted
task and elapsed time to accepted completion. Cheap first-pass output that
requires expensive correction can erase the saving; unattended work can still
be worthwhile even when slow. One mixed-worktree sample is insufficient for a
general model ranking. Have the implementation owner repair these findings
before building more critical work on the affected boundaries.
