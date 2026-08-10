---
title: "Scratch retention: task scratch is ephemeral and reaps itself"
subject: scratch-retention
keywords: [scratch, retention, gc, disk, cleanup, doctor, ttl]
part_of: software-factory
status: canonical
created: 2026-08-04
read_when: "Planning, implementing, or reviewing scratch cleanup, disk-usage GC, the scratch_size doctor finding, or any policy about what may live under .tusker/scratch."
skip_when: "Working on evidence, attempts, events, or task lifecycle with no change to scratch handling or disk retention."
decisions_locked: true
updates:
  - docs/system/gates.md
sources:
  - "Operator session 2026-08-04 — [[2026-08-04-scratch-retention-grill]]"
  - "Disk audit of song_rendition_lab_studio_ready: 956M of 962M .tusker was scratch, dominated by 288 WAV renders in four directories, oldest 9 days"
  - "Code map: no TTL, size cap, or GC exists for any .tusker subdirectory; the only scratch deletion is removeTaskPlanFile, and the daemon close path never calls it"
---

# Scratch retention

## Why we are building this

`.tusker/scratch/` is the prescribed dumping ground for raw exhaust: plan
files, runner wrapper logs, noisy command output, and any large intermediate
artifact an agent produces on the way to durable evidence. Nothing ever
deletes it. In practice a few days of agent work leaves hundreds of megabytes
to a gigabyte of dead renders, rejected variants, and stale working copies
parked under task IDs and ad-hoc names.

The audit that triggered this spec found one project at 956M of scratch out
of a 962M vault — three full generations of 44M audio renders in directories
literally named `rejected_*`, untouched for over a week. Scratch is safe to
delete: evidence attachment always copies files into the evidence store, and
validation forbids recording a scratch path as a durable artifact. The bloat
is pure waste with no proof value.

## The customer story

An operator never thinks about scratch. Task-scoped scratch disappears when
the task closes, everything else ages out on its own, and if a project's
scratch ever grows past a sane budget, `tusker doctor` says so and offers a
one-command repair. Agents can still dump anything they like into scratch
while working — they just know it expires, so anything worth keeping must be
promoted to evidence before close.

## Locked decisions

1. **Scratch is ephemeral by contract.** Nothing under `.tusker/scratch/` is
   durable. Anything worth keeping is promoted to evidence before task close.
   The retention contract is stated in the installed skill text and signs so
   agents plan for it.
2. **Task scratch is reaped at close.** Closing or discarding a task deletes
   `scratch/<TASK-ID>/` entirely, on every close route — manual close,
   discard, and the daemon's automated close all pass through the same
   cleanup. This also fixes the existing leak where daemon-closed tasks keep
   their `PLAN.md`.
3. **Link-only evidence is the one stay of execution.** A task whose evidence
   records `link-only:` paths into its own scratch keeps that scratch at
   close; the sweep may still take it after the TTL. Link-only evidence
   already self-labels as non-durable.
4. **Everything else ages out at 14 days.** `tusker gc` deletes any top-level
   scratch entry — task-keyed or hand-named — whose newest content is older
   than 14 days. Dry-run by default; `--yes` applies. The TTL is
   configurable, 14 days is the default.
5. **Doctor warns at 200M.** `tusker setup doctor` emits a repairable
   `scratch_size` warning when a project's scratch exceeds 200M; repair runs
   the same GC logic. The budget is configurable, 200M is the default.
6. **One deletion engine.** Close-time reaping, the GC sweep, and the doctor
   repair share one implementation with one policy. No parallel cleanup code
   paths.
7. **Deletion is authorized, not assumed.** Nothing is deleted until the target
   is proven to be a Tusker vault. Merely containing a `work/` directory is not
   proof — ordinary repositories have one. Authorization requires the V7 work
   layout plus a vault marker.
8. **Only a direct child of a real scratch directory may be removed.** The
   target must be a single clean path element, and a task ID must match the
   canonical task ID form. A symlinked scratch root is refused outright, because
   following it would redirect deletion outside the vault. A symlinked entry
   inside scratch is fine — removing it takes the link, not the target.
9. **Cleanup never fails a close.** Reaping happens after the task is already
   durably closed, so a cleanup failure is reported as a warning and the sweep
   repairs it later. It never turns a committed close into a reported failure.
10. **The sweep reports what it did, not what it planned.** A partial failure
    names what was deleted, what was skipped, and where it stopped. Sizes are
    logical file sizes, not measured freed disk.

These four safety decisions (7-10) come from an external review on 2026-08-05
that found three blocker-severity deletion escapes. See
[[2026-08-04-scratch-retention-grill]] for the review outcome.

## Known limits of the staleness signal

Age is the newest modification time found beneath an entry. That is a heuristic
for "nobody is using this", not proof. Metadata-preserving copies (`cp -p`,
`unzip`, `rsync --times`, a git checkout) can land content that is already older
than the TTL, and a large forward clock correction ages everything at once. The
dry-run default, the explicit confirmation, and the recheck before each deletion
are what make the heuristic safe to act on.

## Deferred (not now)

- Daemon-scheduled periodic GC. Manual `tusker gc` plus the doctor warning
  covers current usage; wire a daemon timer only if scratch bloat recurs
  despite them.
- Per-file size caps, compression, or relocating scratch outside the repo.
  The TTL sweep makes all three unnecessary.
- Retention for `events/` and `_generated/` (single-digit megabytes after
  weeks of use; not worth machinery yet).

## Developer notes

- Evidence safety: `prepareV7EvidenceArtifacts` copies sources into
  `evidence/<task>/artifacts/`; validation rejects scratch as an artifact
  path; the gate ledger ignores scratch. Deleting scratch cannot break proof
  except for explicitly `link_only` evidence (decision 3).
- The natural close-time attachment point is the shared close projection so
  all close routes are covered; today only the manual route removes even the
  plan file.
- The purge command already has dry-run-then-apply action-list machinery, and
  the Xcode doctor already ships a repairable disk-reclaim finding; both are
  shape precedents, not required dependencies.
