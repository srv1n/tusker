# Integration And Merge (Agent-Native)

Use this when work spans more than one lane, worktree, or branch; when planning
a merge; when a branch has outlived its session; or when proof cost (compile
time) is shaping decisions. It assumes agent-native development: implementation,
review, and verification are agent work, and humans appear only at the Human
Approval Boundary (authority, credentials, unresolved intent, contractually
subjective acceptance).

This is not imported human-team practice. Human teams ration labor; agent
fleets ration something else.

## The Constraint Model

Agent labor is elastic. Three things stay scarce, and every rule below follows
from them:

| Scarce resource | Why it binds | Consequence |
|---|---|---|
| Compile capacity | Rust/Xcode builds cost minutes to hours, not seconds. | Proof is tiered; full gates are batch jobs nobody waits on. |
| Merge serialization | One main; convergence is inherently serial. | Integration capacity, not agent count, caps useful parallelism. |
| Shared namespaces | Migration numbers, lockfiles, generated files collide silently. | A single integrator owns them; workers never touch them. |

Integration debt compounds roughly with the square of branch age times lane
count. Starting lanes is nearly free; converging them is not. Cap concurrent
implementation lanes at what the build hosts and one integrator can absorb —
two implementation lanes plus one integrator is the proven default.

## Lane Rules

- Every lane: own worktree, own branch, claimed Tusker task, disjoint owned
  paths from the plan packet. The primary checkout belongs to the human and the
  integrator; other lanes never edit, build, or gate in it.
- Claim before work. Read the project's stream board (`.tusker/dashboards/`)
  before claiming; on overlap, stop and report the conflict — do not negotiate
  ownership mid-flight.
- A lane delivers the smallest mergeable slice and targets merge within one
  work session. A branch older than 48 hours is a defect: stop feature work and
  integrate. Never stack a second session of work on an unmerged branch without
  integrator acknowledgment.
- Every lane report ends with: branch, HEAD SHA, worktree path, clean/dirty,
  and gate verdicts. "Merged" always means local main; publication (push) is a
  separate recorded authority.

## The Integrator

One lane per repo is the integrator. It exclusively owns: merges toward main,
migration numbering, lockfiles (`Cargo.lock`, `*.xcodeproj`, lock manifests),
generated files, and contract/schema regeneration.

- An implementation lane that needs a migration writes the SQL under a
  task-local name, lists it in its report, and the integrator assigns the
  number at merge time. Prefer timestamp-named migrations where the runner
  supports them — that deletes the collision class outright.
- Workers never merge each other's branches. Conflict resolution is integrator
  work, informed by an overlap audit (files touched on both sides), not by
  optimism.
- The integrator reviews worker diffs and runs one central gate after
  integration; it does not replay passing worker proof unless the merge
  invalidated it.

## Moving Main With Near-Zero Human Review

Default agent-native mode: the integrator merges to main when objective gates
pass. Independent reviewer agents may close every risk tier (see the Human
Approval Boundary in SKILL.md); a human gate on a merge exists only for
authority, credentials, unresolved product intent, or contractually subjective
acceptance — never as a substitute for agent verification.

A project may pin "only the human moves main" in its repo canon. Respect the
pin; it is a project fact, not a doctrine default.

## Proof Economics For Slow-Compile Ecosystems

In Rust, Xcode, and similar toolchains the cost of proof is not the number of
tests. It is:

    total cost ≈ (number of compile/link cycles) × (cost of one cycle)

Both factors are addressable, and agents systematically optimize the wrong one.
Reducing test coverage attacks neither. Every rule below reduces cycles or
reduces cycle cost.

### Harvest, never fail-fast, at gate tier

Fail-fast is correct in the inner loop, where the next error is the only thing
you care about. It is catastrophic at gate tier: N latent defects become N full
compile/link cycles, discovered one at a time.

- Gate-tier runs use the runner's no-fail-fast/continue mode and collect the
  COMPLETE failure set in one pass (`cargo nextest --no-fail-fast` or a
  dedicated profile; `xcodebuild -maximum-parallel-testing-workers` with test
  continuation; `go test ./...` without `-failfast`).
- Then diagnose the whole set, fix as one batch, and rerun once to confirm.
  Two cycles instead of N.
- A project whose gate profile still defaults to fail-fast has a one-line
  defect. Check this before blaming compile time.

### Preflight before expensive work

A 30-second precondition check beats a 35-minute failure. Before any gate-tier
run, verify in this order and refuse early with a named cause:

1. Ledger: has this exact gate already passed on this exact tree state?
2. Disk headroom above the project's measured peak build footprint.
3. Build slot/lock free; no competing stream on the host.
4. Feature/scheme profile matches the canonical gate profile exactly.
5. Working tree frozen (committed), so the result is attributable to one revision.

`tusker gate-run` mechanizes this section: it evaluates those five in order,
refuses with a named `cause` and `remedy` before spending a cycle, runs the
project's configured harvest commands in one pass, and returns the complete
defect list (command, target, first actionable diagnostic) for a single repair
batch. Configure it under `orchestration.gate` in `.tusker/WORKFLOW.md`; see
`references/COMMANDS.md`. Running gates by hand is still allowed, but then the
five checks and the harvest flag are yours to remember.

### Cadence

| Tier | Trigger | Scope | Mode |
|---|---|---|---|
| Inner loop | every edit | type-check only (`cargo check`, build without tests) | fail-fast |
| Slice | end of a coherent batch of tasks | touched packages' focused tests | fail-fast |
| Integration | at merge, tree frozen | full suite + lints, canonical profile | harvest |
| Nightly | scheduled, unattended | full suite + lints + parity profiles | harvest |

The type checker is the in-batch tripwire. Agents may implement several
disjoint tasks before running any tests, provided each edit round ends in a
clean type-check and each task lands as its own commit. That preserves
bisectability while paying the link cost once. Batch boundaries follow
coupling, not counts: tasks sharing a seam belong to one batch, and a task that
establishes a new foundation is proven alone before others stack on it.

### Cycle cost is a build-system property, not fate

- **Binary count dominates link time.** Each integration test file becomes its
  own binary that links the entire library. Hundreds of such binaries is a
  build-performance defect, not a scale fact: consolidate integration tests
  into a small number of harness binaries with modules. Track binary count as a
  metric.
- **Lint debt gets a recorded baseline.** Gates assert "no NEW findings versus
  the baseline"; legacy debt is burned down as its own task. Discovering
  years-old warnings during a merge is a process failure, and adjudicating them
  mid-integration is worse.
- **Never delete build caches to reclaim disk mid-gate.** The rebuild tax lands
  on every lane at once. Disk is a precondition, not a remedy.
- Every worktree keeps its own incremental build directory for its lifetime;
  never point divergent trees at one target dir; keep the integrator's tree
  warm permanently.
- Gate parity: gates run the canonical profile exactly. A green gate on a
  different profile proves nothing — the next cross-build discovers whatever
  the gate skipped.
- One build stream per host; route overflow through the project's build-routing
  seam and queue rather than poll. Capability parity matters: if the remote
  host cannot run the test runner, the slowest tier is trapped on one machine —
  fix the capability, not the schedule.

### Waiting is not work

A lane watching a long gate produces nothing. Gate-tier runs are detached and
report on completion. An agent that has narrated the same "still compiling, no
diagnostics" observation more than twice has stopped working: it should either
be doing disjoint useful work or have handed the gate to the batch tier.

## Enforcement Roadmap

These rules are prose contracts today. Candidates for mechanical enforcement in
the Tusker CLI, in order of leverage: owned-path conflict refusal at claim
time, a generated stream board from live runs/leases, migration-name linting,
and branch-age warnings in `tusker automation plan`. Generic improvements to
this doctrine patch this canonical file; project-specific facts (build hosts,
feature profiles, main-movement pins) stay in repo-local canon.
