---
title: "Decision log: scratch retention session, 2026-08-04"
subject: scratch-retention-grill
keywords: [scratch, retention, gc, disk, cleanup]
part_of: scratch-retention
status: canonical
created: 2026-08-04
decides_for: ".tusker/specs/scratch-retention.md"
read_when: "You want the why behind the scratch retention decisions, including the audit numbers that triggered them."
skip_when: "You only need the locked decisions — read [[scratch-retention]] instead."
---

# Scratch retention — session log, 2026-08-04

This session started as an operator complaint, not a planned grill: "the
Tusker scratch folder in every project just seems to blow up in size — a
couple of days of working and it's close to a gig. It has to be of some value
to us, but it can't grow infinitely."

## Evidence gathered before any decisions

- This repo's `.tusker` is 4.3M total — Tusker's own bookkeeping is not the
  problem.
- `song_rendition_lab_studio_ready/.tusker` is 962M, of which scratch is
  956M: 288 WAV files, dominated by four directories (two task-keyed, two
  hand-named), including three full copies of 44M guitar renders under
  `rejected_*` folders. Oldest entries were 9 days old.
- Code map (Opus investigation): no TTL, size cap, rotation, or GC exists for
  any `.tusker` subdirectory. The only scratch deletion in the codebase is
  `removeTaskPlanFile`, which removes just `PLAN.md`; the daemon's automated
  close never calls even that. Evidence attachment copies files, validation
  forbids scratch as a durable artifact path, and the gate ledger ignores
  scratch — so deletion cannot break proof, except `link-only` evidence,
  which self-labels non-durable.

## Decisions

**Q: Reap task scratch at close, or keep it through wave-boundary review?**
Recommended: reap at close, since evidence is copied and review works from
evidence, not scratch; offered the operator the chance to push back given
their batch-review-at-wave-boundaries flow. Operator approved the design as
proposed ("yes please spec and tusker tasks"). Locked: delete
`scratch/<TASK-ID>/` on every close route; link-only evidence into own
scratch defers close-time reaping (decision 3 in the spec).

**Q: Close-time hook alone, or also an age-based sweep?**
Recommended: both — the audit showed the biggest offenders were hand-named
directories (`SINE-STRAND-FULLMAP`, `orig-piano`) a close hook would never
touch. Operator approved. Locked: `tusker gc` sweeps any top-level scratch
entry older than the TTL, task-keyed or not, dry-run by default.

**Q: TTL and doctor budget defaults?**
Recommended 14 days and 200M; operator accepted the proposal wholesale
without adjusting either, so both locked as configurable defaults rather
than re-asked.

**Q: Daemon-scheduled GC now?**
Recommended deferring; manual gc plus the doctor warning covers current
usage. Deferred.

**Behavioral fix noted:** agents park gigabytes in scratch because nothing
tells them it expires. The retention contract goes into the installed skill
text and signs so promotion-to-evidence becomes the habit, not hoarding.

## External review, 2026-08-05

The first implementation was sent for external review and came back
`request_changes` at critical risk, with three blocker findings — all
reproduced against the real code before any fix was written:

1. **The sweep did not prove its target was a Tusker vault.** `resolveVaultPath`
   accepts an explicit `--vault` with no validation, and discovery treats any
   directory containing `work/` as a vault. `tusker gc --vault /any/dir --yes`
   would have deleted `/any/dir/scratch/*`. Verified: `isVaultDir` really does
   return true on a bare `work/` directory.
2. **Crafted task IDs escaped scratch.** `reapTaskScratch` joined an unvalidated
   ID into `os.RemoveAll`, so a task whose stored id was `../work` would delete
   the canonical work tree. Task creation validates IDs but index loading does
   not, so a corrupt import or hand-edited note was enough — it did not need a
   malicious operator.
3. **A symlinked scratch root redirected deletion.** `os.ReadDir` and `WalkDir`
   both resolve through an intermediate symlink, so `.tusker/scratch ->
   ~/Documents` would have deleted from the external target.

Decision: adopt the reviewer's central recommendation — one fail-closed
deletion boundary rather than raw `os.RemoveAll` at each site. That became
locked decisions 7 and 8 in [[scratch-retention]]. Nine further findings
(link-only path normalization, TTL overflow reversing the predicate, dishonest
partial-failure reporting, the doctor's double scan, `Args.Bool` treating a
malformed `--yes` as true, mislabeled binary units) were fixed in the same pass.

Two HIGH findings were deliberately NOT fixed here and became SGC-T-0004 and
SGC-T-0005: the live-runner interlock on close (a pre-existing lifecycle bug
that cleanup makes destructive) and full daemon coordination for the sweep. The
sweep now rechecks each entry immediately before deleting, which narrows that
race but does not close it; closing it needs a lock with real participants, and
a lock only the sweep observes would fix nothing.

The reviewer explicitly cleared the reactor's idempotent-replay reap placement
and the fail-open direction of the link-only guard, so both were left alone.
