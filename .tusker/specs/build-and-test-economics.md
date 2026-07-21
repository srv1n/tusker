---
title: "Build and test economics: how often we build and test at each stage"
subject: build-and-test-economics
keywords: [build, test, validation]
part_of: software-factory
status: canonical
created: 2026-07-21
read_when: "Deciding what to build/lint/test for a change, a landing wave, or a nightly run; setting up parallel story work; tuning gate budgets, worktree caps, or disk floors; onboarding a worker or reviewer to the build rhythm."
skip_when: "Your task contract already spells out exactly what to run, or you are doing Xcode/Mac work (which stays local by rule and needs no VM routing decision)."
decisions_locked: true
sources:
  - ".tusker/specs/software-factory.md (Stream C — Build and test economics)"
  - "Operator locked decisions (2026-07-21)"
  - "The 2026-07 cold-build disk-fill incident"
---

# Build and test economics: how often we build and test at each stage

## The short version (read this first)

We build and test at three stages, and each stage does deliberately less or more
than the others. The whole point is to spend compute where it catches problems
and to stop spending it everywhere else.

1. **Every change** gets a light check: compile the thing that changed, lint it,
   and run only the tests for the crates or packages that change touched.
   Nothing more. This is fast on purpose.
2. **When a batch of work lands together** (a "wave"), we run one combined
   build-and-test across the whole workspace: compile everything, lint
   everything, run the full workspace test set — once, for the whole batch, not
   once per story.
3. **Every night**, the full suite runs on the heavy machine (our Hetzner box,
   using Crabbox VMs), the exhaustive run we do not want to pay for during the
   day.

If you remember nothing else: **small check per change, one big check per wave,
the exhaustive check overnight.** A change should never trigger a full workspace
test, and a wave should never skip one.

## Why the rhythm exists (a two-day lesson)

In July 2026 we learned the cost of getting this wrong. Several worktrees were
each cold-building at the same time, with no shared build cache between them.
Every worktree compiled the same dependencies from scratch into its own target
directory. The duplicated build output filled the disk, the machine wedged, and
recovering it — clearing space, rebuilding warm caches, re-running work that had
been lost — cost roughly two days.

Two rules in this page come straight out of that incident:

- **Shared build caches across every worktree**, so the same dependency is
  compiled once and reused, not rebuilt per worktree.
- **A hard cap on how many worktrees can be live at once**, so we can never
  again spin up enough cold builds to fill the disk.

The incident is also why we do not guess at numbers (see "Measured floors"
below).

## How the parallel work is arranged

The rhythm above assumes a specific way of splitting work, so the stages line up
cleanly:

- A wave is cut into **story slices that each own a disjoint set of files.** No
  two slices touch the same file.
- Slices **apply optimistically**: each worker lands its change directly and
  only makes its own slice compile. Workers do **not** run the full test suite —
  that is the wave's job, not the slice's.
- Because ownership is disjoint, "merging" the wave is just the union of what is
  already on disk. There are no merge conflicts to resolve, so the single
  wave-end build-and-test is the first and only place cross-slice seams are
  checked.

This is why stage 1 can be so light: a slice only has to prove it compiles and
that its own touched packages still pass. The wave-end run (stage 2) is what
guarantees the pieces fit together.

## Where each stage runs

| Stage | When | What it runs | Where it runs |
|---|---|---|---|
| Per-change gate | On every change that touches code | Compile check + lint + focused tests on the touched crates/packages only | Local, in the worker's worktree |
| Wave-end gate | Once, when a batch of slices lands together | One collective compile + lint + full workspace test | Local (or a VM if escalated — see below) |
| Nightly full suite | Every night | The exhaustive full suite | The heavy machine: Hetzner box, Crabbox VMs, sccache to the Hetzner bucket |

**Xcode / Mac work stays local.** We do not run Mac builds in a cloud sandbox.
There are no macOS VMs in this rhythm; Apple-platform work is checked on a local
Mac and is out of scope for VM routing.

## When a local run gets escalated to a VM

Local is the default for the per-change and wave-end stages. A run is routed off
the local machine to a VM only when one of two things happens:

- **The local gate exceeds its time budget.** If the gate is taking longer than
  its allotted budget, we stop paying for it locally and hand it to a VM.
- **Free disk drops below the measured floor.** If running the gate locally
  would take free space under the floor, the run goes to a VM instead of risking
  a repeat of the disk-fill incident.

This is the "escalation timer": local first, VM when the budget or the disk floor
is crossed.

## Measured floors, not guesses

Every resource floor in this policy — the disk floor that triggers escalation,
the per-stage time budgets, the worktree cap — is a **measured** number, set from
observed behavior on the real machines. None of them is estimated.

This is a hard rule because of the **2026-07-20 unmeasured-15GB incident**: a disk
floor was assumed rather than measured, and the assumption did not hold. This
page therefore names no specific numbers. The actual figures live in the separate
measurement work that owns them, and this policy points at whatever those
measurements currently say. If you find yourself about to type a number here,
stop and go measure it.

## What enforces this

- Per-change (stage 1) gate tiering: `cmd/tusker/gate_tier.go`.
- Wave-end (stage 2) collective gate: `cmd/tusker/batch_gate.go`.
- Parent policy and stream context: `.tusker/specs/software-factory.md`
  (Stream C — Build and test economics).

---

## Appendix: exact commands and configuration (for workers)

This appendix is the implementer layer. The plain-language policy above is
authoritative; the commands here are the mechanical form of it. Where a command
below and the policy disagree, the policy wins and this appendix is stale.

### Stage 1 — per-change gate (focused, local)

Run only against the crates/packages the change touched. Do **not** run the full
workspace here.

Rust (touched crate):

```bash
cargo check -p <touched-crate>
cargo clippy -p <touched-crate> -- -D warnings
cargo test  -p <touched-crate>
```

Go (touched package):

```bash
go build ./path/to/touched/pkg/...
go vet   ./path/to/touched/pkg/...
go test  ./path/to/touched/pkg/...
```

The gate-tier logic that decides "touched only" lives in
`cmd/tusker/gate_tier.go`.

### Stage 2 — wave-end gate (collective, local, once per wave)

Run once after all slices in the wave have landed and each compiles. This is the
single cross-slice seam check.

Rust workspace:

```bash
cargo check --workspace
cargo clippy --workspace -- -D warnings
cargo test  --workspace
```

Go workspace:

```bash
go build ./...
go vet   ./...
go test  ./...
```

The collective wave gate is driven by `cmd/tusker/batch_gate.go`.

### Stage 3 — nightly full suite (heavy machine)

Runs on the Hetzner box via Crabbox VMs, with sccache pointed at the Hetzner
bucket so the VM's compilation is cache-hot rather than cold.

- Compiler cache: `sccache`, backed by the Hetzner object bucket, shared so
  nightly VMs reuse warm build artifacts instead of cold-building.
- Execution substrate: Crabbox VMs on the Hetzner box.
- Scope: the exhaustive suite (everything the daytime stages deliberately skip).

### Shared build cache across worktrees (incident fix)

Every worktree must share one build cache; no worktree cold-builds into a private
target dir.

- Rust: point every worktree at one shared `sccache` (and, where used, a shared
  `CARGO_TARGET_DIR`) so a dependency compiles once and is reused.
- Set the shared cache in the environment all worktrees inherit; do not let a
  worktree default to a private, unshared cache.

### Worktree cap and disk floor (measured)

- A hard cap limits how many worktrees may be live at once. The number is
  measured, not guessed, and is enforced where worktrees are provisioned.
- The disk floor that triggers VM escalation is a measured number. Read it from
  the current measurement source at gate time; never hard-code it here.
- Escalation rule in mechanical terms: before/while running a local gate, if
  elapsed time exceeds the stage's time budget, or if free disk would fall below
  the measured floor, route the run to a VM instead of continuing locally.

Per the 2026-07-20 unmeasured-15GB incident, treat any hard-coded resource number
in a gate as a bug until it is traced back to a measurement.
