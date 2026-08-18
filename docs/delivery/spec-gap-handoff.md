# Spec gap handoff — build brief

Audit date: 2026-08-18. Baseline commit: `707273e0`. Auditors: three independent
Opus agents, read-only, each grading locked spec decisions against the compiled
binary rather than against docs or task status.

Read this with the ledger view: every item below is a locked decision from a
spec in `.tusker/specs/` that is not yet true of the binary. Tracked task IDs
already exist; contracts are unwritten (each task file has TBD intent and a
placeholder verification row) and must be filled in before the work starts.

Ground rules for whoever picks this up:

- The spec is the contract. Where this brief and a spec disagree, the spec wins;
  say so rather than silently following the brief.
- Verify before building. Several audit claims name a file and line; confirm the
  seam still exists at the current HEAD.
- Central gate before any commit: `gofmt -l cmd/tusker`, `go vet ./cmd/tusker`,
  `go test ./cmd/tusker -count=1 -timeout 25m`. Serve UI work also needs
  `cd internal/serve/ui && bun run typecheck && bun run build` (Bun only, never npm).
- `TestCrossScopePlanRewiring` fails on stale plan fingerprints after doc edits.
  The error prints `got <A> want <B>`; `A` is the stale recorded value, replace it
  with `B` in the named `docs/plans/*.yaml`. Expect to repeat about five times.
- No AI attribution in commit messages, ever. Subject plus body only.

---

## Work item 1 — Doc-touch drift rule (KNW-T-0003)

Spec: `.tusker/specs/knowledge-graph.md`, locked decision 4. Rollout wording is
locked as warning-first with the flip to blocker being a config change.

**State.** The `describes:` front-matter field does not exist anywhere. Not one
canonical doc declares code paths. `internal/docgraph/docgraph.go` has no
`Describes` field; there is no diff intersection, no waiver-row grammar, no
close-time hook.

**Build.** Parse `describes:` (list of coarse repo paths) into the docgraph
document type. At task close, intersect the task's landed diff with every doc's
`describes:` paths. Each implicated doc needs either a doc edit in the same diff
or a waiver row in the task's Verification table shaped
`doc_unchanged | <doc subject> | waived | <reason>`. Emit warnings only, behind a
config key (suggest `docs.touch_rule: warn|block|off`, default `warn`) so the
later flip is configuration, not a rebuild. Code paths no doc claims trigger
nothing.

**Watch for.** The close path may not have diff access at the seam you want; if
not, compute changed paths from the task's recorded branch range, or hook
`land`/`gate-run` instead — and report which seam you chose and why.

**Owns.** `internal/docgraph/**`, the close-path hook.

---

## Work item 2 — Freshness stamps and `docs status` (KNW-T-0004)

Spec: same file, locked decision 5. Depends on item 1: staleness is defined as
commits touching `describes:` paths since `last_verified`.

**State.** `last_verified` is read in exactly one place — `freshness()` in
`cmd/tusker/docmap.go` — which fills the INDEX freshness column and returns
"never" when absent. Every row currently reads never. No doc carries the stamp,
nothing bumps it, and `tusker docs status` is not a command.

**Build.** Parse `last_verified` (date). Add `tusker docs status`, sorting docs by
staleness with unstamped docs ordered as never-verified. Add the field to the
`docs new` template. Provide the stamping path — suggest
`tusker docs verify <subject>` setting today's date, refusing unknown subjects.

**Also here.** `docs map` emits `updates:` edges into `graph.json` but never
validates them, so dangling entries persist silently. Add a named defect
(suggest `DOCS_MAP_DANGLING_UPDATES`) for an `updates:` entry that is neither an
existing subject nor an existing repo path. Current specs use file paths in
`updates:` — those must pass.

**Owns.** `internal/docgraph/**`, `cmd/tusker/docs_cmd.go`, `cmd/tusker/docmap.go`.

---

## Work item 3 — `init` scaffolds the documentation system (KNW-T-0005)

Spec: same file, locked decision 7 and ruling K1. This is the decision that makes
the doc system exist "on day zero without a human demanding it."

**State.** `initCmd` in `cmd/tusker/install.go` has zero references to
`docs/system`. A fresh repo gets the `knowledge/domains` tree instead — the
opposite of what is locked. Consequence: in a new repo `docs find` returns
nothing and `docs map` fails outright on the missing `00-overview.md`.

**Build.** `tusker init` creates `docs/system/00-overview.md` — a stub carrying
the front-matter contract and the empty `tusker:docs-map` generated-region
markers so `docs map` works immediately — plus `.tusker/specs/` and
`.tusker/specs/decisions/`. Idempotent: never overwrite an existing overview or
specs tree. Print each created path with its undo line, matching init's existing
opt-in reporting style. This is core scaffolding, so it belongs in the default
`--yes` surface, unlike pointers and mounts.

**Open question to answer, not guess.** The spec also says init installs the spec
skill. Report what that requires versus what exists rather than inventing
distribution machinery.

**Owns.** `cmd/tusker/install.go`.

---

## Work item 4 — Two doc validation rules (KNW-T-0009, KNW-T-0010)

Spec: same file, locked decisions 6 and 8. Neither had a task before this audit.

**KNW-T-0009 — a locked spec must land its doc updates.** No code reads
`decisions_locked` anywhere (`grep` returns zero hits) even though
`knowledge-graph.md` sets it and names two `updates:` targets. Build the
deterministic form: resolve each `updates:` target; a missing target is an error;
an existing target that predates the spec with no task in the owning epic
referencing that spec in `spec_refs` emits a warning (suggest
`SPEC_UPDATES_UNLANDED`). Test against this repo's real spec.

**KNW-T-0010 — readable-shape lint for docs.** The plain-language lint
(`cmd/tusker/v7_plain_top_layer_lint.go`) exists but applies to tasks only.
Extend its entry point to accept a doc body and check the opening paragraph with
the same code-word heuristic; warning severity, suggest
`DOC_OPENING_CODE_WORDS`. Delegate front-matter completeness to the existing
docgraph header validation rather than duplicating it.

**Owns.** `cmd/tusker/v7_plain_top_layer_lint.go`, additive validate wiring.

---

## Work item 5 — Wave-boundary batch review (ORC-T-0092)

Spec: `.tusker/specs/decisions/2026-07-22-gates-over-records.md`. The accept gate
was justified there as "what lets the operator review at wave boundaries instead
of babysitting." **This is the highest-priority item in the brief: the operator
named it critical and it does not exist in any form.**

**State.** Nothing assembles a wave-scoped review batch. `/api/review/batch`
(`cmd/tusker/serve_command.go`) returns a flat list of every task with status
`review`, ungrouped. `internal/serve/ui/src/features/work/ProjectWork.tsx` says
in its own comment that the endpoint is a stub and the batch bar is "deliberately
client-side fan-out." `.tusker/dashboards/review-queue.md` has an empty Wave
column on every row.

**Build.** Group the review batch by wave and expose readiness — a wave whose
members are all terminal (review, done, or discarded, nothing running or pending)
is ready for human review. Shape suggestion:
`{waves: [{waveId, title, authorization, readyForReview, members: [...]}], unwaved: [...]}`.
Update the serve UI in the same change (you own both sides): group by wave, show
a "Wave N ready for your review" state, batch bar acts per wave. Populate the
Wave column in the review-queue dashboard generator
(`cmd/tusker/v7_state_runtime.go`) and order rows wave-first.

**Owns.** `cmd/tusker/serve_command.go`, `cmd/tusker/v7_state_runtime.go`,
`internal/serve/ui/src/features/work/**`.

---

## Work item 6 — Wave brief honesty (ORC-T-0093)

**State.** On the newest wave, `wave brief` reports "8 parked for machine rework"
and lists all eight under Rework/parked when every one is plain `backlog`, never
attempted. Never-started work is being reported as failed work, which corrupts
the morning read.

**Build.** Split the outcome counts into not-started, in-flight, parked/rework,
and landed; give never-started members their own heading.

**Owns.** `cmd/tusker/v7_wave_brief.go`.

---

## Work item 7 — Reviewer independence (ORC-T-0094)

Spec: `.tusker/specs/software-factory.md`, principle 6 — the validator is never
the author, and should differ in vendor and model.

**State.** The principle is a config knob with no check behind it, and the
fallback actively defeats it: `cmd/tusker/workflow.go` (around line 667) and
`cmd/tusker/daemon.go` (around 1397) silently overwrite an unavailable
`reviewer.runner` with `agents.default` — the implementer's own runner. This repo
currently runs both lanes on Codex.

**Build.** At profile-resolution time, warn when the effective reviewer vendor
equals the implementer vendor (derive vendor from the runner-kind prefix). When
the fallback itself triggers, say so explicitly: what was configured, that it was
unavailable, and what it fell back to. Warning, not refusal.

**Related, worth reporting.** `reviewerPolicyCoversRisk` is a literal alias of
`reviewerMayAutoCloseRisk`, so a risk tier excluded from auto-close currently gets
no review at all rather than review-without-auto-close. Splitting "may review"
from "may auto-close" is a real fix; confirm it against the spec before doing it.

**Owns.** `cmd/tusker/workflow.go`, `cmd/tusker/daemon.go`.

---

## Work item 8 — Spec link on demanding tasks (ORC-T-0095)

Spec: `.tusker/specs/software-factory.md` — hard tasks are blocked from ready
without a spec link.

**State.** `validateV7SpecTraceability` (`cmd/tusker/v7_traceability.go`) warns
only about dangling refs. An empty `spec_refs` on a p1 or high-risk task passes
silently.

**Build.** Reuse the existing `v7TaskIsDemanding` predicate (already written for
the plain-language lint) to warn at validate and at `status ready` when a
demanding task has empty `spec_refs`. Warning-first per the established probation
pattern; tier 1 exempt.

**Owns.** `cmd/tusker/v7_traceability.go` plus the ready-transition check.

---

## Work item 9 — Operator surface honesty (ORC-T-0096)

**State, all verified live.** `--help` on `wave`, `land`, `brief`, `dashboard`,
`closeout` prints the global usage blob instead of command help; the real usage
appears only on bare invocation. Worse, two commands act on `--help`:
`tusker gate-run --help` executes the gate preflight, and bare `tusker dashboard`
*writes* dashboards. `digest`, `escalate`, and `departure` are absent from the
main help list. `tusker digest` with no `--since` reports "Since: all recorded
state" and dumps everything since July, which undercuts the morning framing.

**Build.** `--help` prints that command's usage and exits with no side effects,
on every command. Add the three missing commands to the help list. Default
`digest` to the last 24 hours with an explicit flag for all-time.

**Owns.** `cmd/tusker/cli.go`, `cmd/tusker/v7_escalation_digest_cmd.go`.

---

## Work item 10 — Wave member state and re-fingerprint (ORC-T-0097)

**State.** `wave show` reports every member of the newest wave as
`stale-authorization` on a wave whose authorization is `disarmed` and which was
never armed — never-armed and drifted-authorization are conflated. Separately,
that wave is un-armable: preflight blocks with "factory-intake plan contract is
stale or contradictory; remedy: regenerate the V2 plan," so an imported wave goes
stale simply because the embedded factory-intake provenance moved.

**Build.** A `disarmed` member state distinct from `stale-authorization`, and a
re-fingerprint path that does not require re-authoring the plan.

**Owns.** `cmd/tusker/v7_wave_authorization.go`.

---

## Work item 11 — Two scratch-retention holes

Spec: `.tusker/specs/scratch-retention.md`, locked decisions 1 and 2. The rest of
this spec audited clean; `SGC-T-0001` and `SGC-T-0003` sit in review and these two
holes are inside their scope, so returning them to rework is more honest than
accepting them.

**Hole A.** `tusker status <id> done` (the direct-completion branch in
`cmd/tusker/v7_control_cmd.go`, reachable at tier 0 and 1) never reaps scratch,
while every other close route does. Add the same
`warnScratchReapFailed(id, reapTaskScratch(...))` call.

**Hole B.** The packaged skill tree never states the ephemerality contract. Add
one sentence to the proof paragraph in `skills/tusker/references/TRACK.md`
mirroring the wording at `cmd/tusker/install.go:1402`, then recount the track case
in `skills/tusker/testdata/progressive-disclosure-budget.json` (the test counts
`strings.Fields(router + guide)`; set max to actual + 10 rounded up to a multiple
of 25).

---

## Not in this brief, deliberately

**Tracker reconciliation.** Roughly 50 ORC tasks and all 8 ACP tasks describe
shipped features while sitting at backlog — delivery v2, dispatch scope,
departure, scheduled promotion, execution observability, the ACP runner itself.
One wave reached `landed` while disarmed. This is the drift the gates-over-records
decision calls a system defect. It should be a separate reconcile pass, run after
the build work lands so statuses settle against the final binary.

**Domains retirement.** The `.tusker/knowledge/domains` tree is the live violation
of the one-corpus decision, but retiring it touches packet routing (including a
default-domain fallback every task without declared domains relies on), eight
validate rules, `skill doctor`, the vault-discovery marker, and feedback
promotion. It needs its own wave, and work item 3 must land first so `init`
scaffolds the replacement.

**Awaiting a product decision, not engineering.** VM escalation, the nightly
remote suite, and the shared build cache are prose in
`.tusker/specs/build-and-test-economics.md` with zero code and no tasks — build
them or move them to that spec's Deferred list. The worktree cap and disk floor
are built but unset in this repo; the spec's own rule says measure, never guess.
The completion reactor (reviewer auto-return) defaults to disabled. ACP gates
`ACP-G-0001` through `0003` are human gates by design.

**Execution observability.** The ledger is built — roughly 5,800 lines, schema,
triggers, adapters, UI — but the daemon never populates it: the constructors have
zero production callers and the live database holds zero edges, zero observations,
and zero timeline events. Everything visible is backfill. `ORC-T-0066` through
`0076` already map this work one-to-one; it is a large separate stream.
