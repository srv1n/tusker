---
title: "Proof recording and closeout"
subject: proof-and-closeout
keywords: [proof, verification, evidence, attempts, closeout, close, accept, completion receipt, scratch, gc]
part_of: overview
status: canonical
read_when: "You must record that work is proven — verify rows, evidence records, attempts — or you are trying to close/accept a task, emit a closeout checkpoint, understand a close refusal, or reason about scratch retention."
skip_when: "You only need the task frontmatter schema ([[tasks-and-proof]]) or how gates are opened, satisfied, and batched ([[gates]])."
sources:
  - cmd/tusker/v7_proof_cmd.go
  - cmd/tusker/v7_evidence_attempt_cmd.go
  - cmd/tusker/v7_closeout_cmd.go
  - cmd/tusker/v7_close_authority.go
  - cmd/tusker/v7_close_ceremony.go
  - cmd/tusker/v7_completion_receipt.go
  - cmd/tusker/v7_recipe_cmd.go
  - cmd/tusker/trace_replay.go
  - cmd/tusker/scratch_retention.go
  - cmd/tusker/scratch_gc.go
  - cmd/tusker/accept_cmd.go
  - cmd/tusker/close_policy_migration.go
  - cmd/tusker/v7_control_cmd.go
  - internal/v7schema/schema.go
  - internal/v7policy/close.go
---

# Proof recording and closeout

Never set `proof_status` by hand: it is **recomputed** from the task body, evidence
records, and gates on every proof write (`computeV7ProofReport`,
`cmd/tusker/v7_proof_cmd.go`). Add rows and evidence; status follows.

## Proof rows (inline verification)

A proof row is one line of the `## Verification` markdown table in the task body.
`tusker verify add <TASK-ID>` upserts it (`verifyV7AddCmd` →
`upsertV7VerificationRow`, `cmd/tusker/v7_proof_cmd.go`).

| Column | Flag | Required | Rule |
|---|---|---|---|
| Covers | `--covers` | yes | acceptance IDs: `A1`, `A1,A2`, `A1-A3`, `ALL`, or `TASK:A1`; unknown IDs are dropped (`v7CoversToAcceptanceIDs`) |
| Check | `--check` | yes | `command: <exact shell command>`, `manual proof: <exact steps>`, or `ledger: <gate-ledger-id>` — a placeholder (`<…>`, empty, `-`) does not count (`v7VerificationGrammarHint`, `v7_validation.go`) |
| Result | `--result` (default `pass`) | yes | one of `pass fail blocked skipped waived`; `pending` is rejected on write |
| Notes | `--note` | no | free text only; never consulted when deciding whether a check proves anything |
| Blocked By | `--blocked-by` | only when result is `blocked` | path, task ID, or gate ID; its absence is a hard error |

- Only `pass` and `waived` rows cover acceptance (`v7VerificationResultCovers`).
- A pre-grammar bare command (`go test …`, `rtk …`, `make …`, `cargo …`, `npm …`,
  `pytest`, `git …`, `tusker …`, …) still validates through a closed legacy prefix
  list (`v7VerificationLegacyPrefixes`, `cmd/tusker/v7_validation.go`). That list is
  frozen — write new rows as `command: …`.
- Upsert key is (Covers, Check), case-insensitive. Rows whose Check is `TBD` or
  whose result is `pending` are dropped on any write.
- Batch: `--rows "A1|go test ./...|pass|note|blocked_by"` (newline-separated) or
  `--batch-file <path>`. The result cell is found by scanning right-to-left for a
  valid result token, so `|` inside a check survives (`parseV7VerificationRowArg`).
- A Check of the trace-replay form is re-run at read time and forced to `fail` if
  the mock replay diverges (`evaluateV7ReplayVerificationRow`,
  `cmd/tusker/trace_replay.go`).
- Writes take `<vault>/.tusker/locks/proof-<TASK-ID>.lock` (2s, `--lock-timeout-ms`);
  `PROOF_WRITE_BUSY` and `CAS_CONFLICT` both carry an exact `retry_command`
  (`withV7ProofWriteLock`, `v7VerificationRetryCommand`).

## Proof mode and required checks

`tusker proof set-mode <TASK-ID> <mode>` (`proofV7SetModeCmd`) sets `proof_mode`
and, unless `--required` is given, resets `proof_required` and `evidence_budget`
to the mode defaults.

| Mode | Default `proof_required` | Default `evidence_budget` | Mode gap beyond required |
|---|---|---|---|
| `none` | (none) | 0 | nothing required |
| `inline` (default) | `focused_test`, `broad_test` | 0 | none |
| `card` | `focused_test`, `broad_test` | 1 | one accepted evidence record |
| `artifact` | `build`, `manual_smoke`, `screenshot` | 3 | accepted evidence **with an artifact path** |
| `audit` | `focused_test`, `broad_test`, `independent_review` | 5 | accepted evidence with an artifact path |

Default mode is `inline`, or `audit` when `risk: critical`
(`defaultV7ProofMode`). Each `proof_required` entry is satisfied by an inline row,
an accepted evidence record, or a satisfied/waived gate
(`v7ProofRequiredClassSatisfied`) — under the matching rules below. Computed `proof_status`: `satisfied` with no
acceptance and no mode gaps, and only when the mode is `none`, the body declares
acceptance IDs, or `proof_required` is empty; `partial` when anything is covered or
gates/evidence exist; else `pending`. An explicit `waived` is preserved, and placeholder ("packet
stub") acceptance without a waiver is forced down to `partial`
(`v7ComputedProofStatus`, `upsertV7VerificationsLocked`).

### What satisfies a required class

Matching is **structural, never textual** — a row or record that merely mentions
"test" or "lint" proves nothing.

- **Inline rows.** The Check must yield a command (`v7VerificationCommand`); a
  `manual proof:` / `ledger:` row never satisfies a machine class. The command is
  then parsed, not searched: split into segments on `&&`, `||`, `;`, `|` (quote-
  and escape-aware), leading `VAR=value` assignments and wrapper words (`rtk`,
  `proxy` — so `rtk proxy go test …` reduces correctly — `env`, `exec`, `command`,
  `sudo`, `timeout`) stripped, then each segment's basename is looked up in a
  per-class tool map (`v7CommandInvokesAny`, `v7CommandInvokesTest`). So
  `focused_test`/`broad_test` take `go test`, `cargo test`, `swift test`,
  `dotnet test`, `npm|pnpm test` or `run test…`, `yarn test`, `bun test`, `pytest`,
  `jest`, `vitest`, `make test`, `python[3] -m pytest|unittest`, `npx jest|vitest`;
  `lint` takes `eslint`/`golangci-lint`/`ruff`/`staticcheck`/`cargo clippy`/
  `make lint`/`npm|pnpm run lint…`; `build` takes `go build|test`, `cargo
  build|test`, `swift build`, `xcodebuild`, `tsc`, `npm run build…`, `make build`;
  `benchmark` needs `go test -bench…` or `cargo bench`. An unlisted class falls
  back to invoking a tool literally named after it.
- **Evidence.** Kind matching is exact, with no summary/body text fallback:
  `focused_test`/`broad_test` need `automated_test`, `unit_test`,
  `integration_test`, `e2e_test`, or `ci_run`; `typecheck`, `lint`, `build`, and
  `ci` need `ci_run`; `independent_review` is satisfied only by a `human_review`
  record (`v7EvidenceSatisfiesProofRequired`).
- **Gates.** A satisfied/waived gate matches by `gate_kind`
  (`v7GateKindSatisfiesProofRequired`), or — only for `manual_smoke`,
  `physical_smoke`, `human_signoff` — by a `verification` field that *starts with*
  that exact phrase (`v7StartsWithProofPhrase`).

### Gap ownership

Every gap and open gate is classified `machine | reviewer | human | external`
(`classifyV7ProofReport`). A gate classifies from its own `owner`/`gate_kind`; an acceptance
gap from a blocked row's `--blocked-by`, else its covering open blocking gate, else machine;
a `proof_required:` gap from a blocked row, the task's `proof_required_owner` map, an owning
open blocking gate, then a fixed class default (`human_signoff`, `manual_smoke`,
`physical_smoke`, `release_smoke`, `security_review`, `privacy_review`,
`accessibility_review` → human; `independent_review` → reviewer; `ci`,
`provider_probe` → external; else machine). When *only* human gaps
remain the report sets `terminal_wait: true` and `agent_action: stop_until_human_response`: stop
working and emit a closeout. `tusker proof status <TASK-ID> [--json] [--verbose]` prints the report.

## Verify recipes

`tusker proof recipe <TASK-ID>` / `tusker verify recipe <TASK-ID>`
(`cmd/tusker/v7_recipe_cmd.go`) prints scoped commands from
`<vault>/verification-recipes.yaml` — a missing file yields no recipes, not an
error, and a recipe with no `command`/`commands` is skipped. A recipe matches when
task `domains` intersect its `domains`, task `risk` is in `risks`, a `--files`
entry matches `file_globs` (`dir/**`, `a/**/b.go`, `filepath.Match`), or it
declares none of those. Recipes are advice; nothing enforces running them.

## Evidence vs scratch vs attempts

| Store | Path | Lifetime | Purpose |
|---|---|---|---|
| Evidence | `.tusker/evidence/<TASK-ID>/<ID>.md` (+ `artifacts/<ID>/`) | durable, committed | proof that survives the task |
| Scratch | `.tusker/scratch/<TASK-ID>/` | ephemeral; reaped at close, swept by `tusker gc` | noisy logs, raw output |
| Attempts | `.tusker/attempts/<TASK-ID>/<ID>.md` | durable | narrative of one agent run |

`tusker evidence add <TASK-ID> --covers A1 [--kind …]` (`evidenceV7AddCmd`) refuses
evidence not tied to acceptance. Kind defaults to `manual_smoke` and must be in
`EvidenceKinds` (`internal/v7schema/schema.go`); schema `tusker.evidence/v1`, ID
`<TASK-ID>-E-NNNN`.

Artifacts (`prepareV7EvidenceArtifacts`): `--path` sources are **copied** into
`evidence/<TASK-ID>/artifacts/<EVIDENCE-ID>/` — scoped per record, so two records
cannot collide on a basename — and recorded vault-relative
(`artifact_durability: copied`). Refused as non-durable: URLs without
`--external-url`, absolute paths, `/tmp/…`, paths escaping the workspace,
directories, missing files. `--external-url` (scheme required) records
`external:<url>`; `--link-only` records `link-only:<path>` (durability
`link_only`).

Evidence writes are transactional (`v7_evidence_attempt_cmd.go`). The record lands
via a synced temp file promoted with `os.Link` — **no-clobber**, so an existing
path is `ALREADY_EXISTS`, never an overwrite (a filesystem rejecting `link` falls
back to an `O_EXCL` reservation plus rename, and stale `.<name>.tmp-*` files older
than an hour are swept). Artifacts copy through the same temp+`fsync` path and
publish with an atomic rename. When the ID is already taken, `evidence add`
**resumes** only if every argument matches the record on disk — kind, ordered
covers, status, summary, artifact request, and a `state_rev` that still verifies —
and the task simply has not been linked yet; anything else is
`ALREADY_EXISTS: Evidence ID is taken with different content`. A resume finishes
the missing half (task proof-status update + `evidence_added` event) instead of
writing a second record.

Acceptance:

- Status defaults to `accepted`, except reviewer-acceptance kinds (`screenshot`,
  `video`, `manual_smoke`, `physical_smoke`, `release_smoke`, `security_review`,
  `privacy_review`, `human_review`, `performance_profile`), which default to
  `pending_review` (`v7EvidenceRequiresReviewerAcceptance`). Accepting those
  requires `--accepted-by human:<n>`/`reviewer:<n>`; creator self-acceptance is
  refused unless the task is `risk: low` **and** `--allow-self-check` is passed.
- Accepted `screenshot` evidence also needs `--checked-by`; without both
  `screenshot_checked_by` and `_at` it never counts toward proof.
- Evidence counts toward proof only when `status: accepted` and every artifact path
  is external or durable (`v7EvidenceUsableForProof`, `v7ArtifactPathDurable` in
  `cmd/tusker/v7_validation.go`) — one `link-only:` artifact disqualifies the record.

`tusker evidence promote <TASK-ID> --from .tusker/scratch/…` copies a scratch
artifact into evidence (kind defaults `log_excerpt`); a non-scratch source needs
`--force`. `tusker evidence prune <TASK-ID>` is **dry-run only**, classifying
records and legacy attachments into keep / move_to_attempt / move_to_scratch /
delete_candidate / promote_to_gate / forbidden (`classifyV7EvidenceBloat`); source
code, project files, and archives are `forbidden` as artifacts.
`tusker attachments migrate --write` moves a legacy `Attachments/` tree into
`<vault>/.tusker/scratch/<task>/legacy-attachments/` (unmapped files under
`_unmapped`).

Attempts (`tusker attempt start|handoff`, `attemptV7StartCmd` in
`cmd/tusker/commands_v7.go`) write `tusker.attempt/v1` at
`attempts/<TASK-ID>/<TASK-ID>-A-NNNN.md` with runner, model, workspace, branch and
status (`started|handoff|failed`). With a `WORKFLOW.md` present, `attempt start`
delegates to the work-session command instead.

## Closeout: the terminal human-wait checkpoint

A closeout is not a close — it is a durable "machine work is done, a human owes us
something" checkpoint (`cmd/tusker/v7_closeout_cmd.go`).
`tusker closeout <TASK-ID> --validate '<command>' --emit-packet` refuses unless:

| Check | Failure |
|---|---|
| report is `terminal_wait` (only human gaps left) | `EVIDENCE_GATE` listing machine/reviewer/external gaps, or `INVALID_TRANSITION` when nothing is human-owned |
| `--validate <command>` given and non-empty | `MISSING_ARG` |
| `--emit-packet` passed | `MISSING_ARG` |
| the validation command exits 0 (run via `sh -c` from the repo root) | `EVIDENCE_GATE` with a 20-line output tail |
| terminal state unchanged after validation (re-read and recomputed) | `EVIDENCE_GATE` |

On success it writes `_generated/packets/<TASK-ID>.reviewer.md`, then
`work/closeouts/<TASK-ID>-C-NNNN.md` (`tusker.closeout/v1`, state
`machine_complete_waiting_for_human`), projects the task to `status: review`,
`machine_status: complete`, `human_status: pending`, `closeout_status:
machine_complete_waiting_for_human` (`prepareV7TaskForHumanWait`), and releases the
active lease. A failed task write removes the checkpoint file.

`state_fingerprint` (`tusker.closeout-fingerprint/v2`) hashes the close policy, the task doc and body,
the full proof report, the state_revs of related gates/evidence/attempts, the validation record, the
packet contents, and **repo state**: git HEAD, porcelain status, hashes of `git diff` /
`git diff --cached`, and hashes of every untracked file — vault directory excluded from the pathspec
(`v7CloseoutRepoState`). Any code change therefore invalidates the checkpoint.
`tusker closeout status <TASK-ID> --json` reports `checkpoint_valid`, which also requires a recorded
`pass` validation with a command and every packet file still present (`v7CloseoutCheckpointValid`).

## Close and accept

`tusker close <TASK-ID>` (`closeV7Cmd`, `cmd/tusker/v7_control_cmd.go`) and
`tusker accept <TASK-ID> --by reviewer:<n>` (`acceptV7Cmd`) share one eligibility
implementation, `v7ClosePreflight` (`cmd/tusker/v7_close_ceremony.go`), which
blocks a close in this order:

| # | Check | Function |
|---|---|---|
| 1 | task bytes match `state_rev`; identity/revision/state did not drift | `v7ClosePreflight` (`CAS_CONFLICT`) |
| 2 | status is `review` (close only; `--force` bypasses) | `v7ClosePreflight` |
| 3 | no open blocking gate lists the task in `blocks` | `v7ClosePreflight` |
| 4 | every dependency is `done` — readiness softness does not apply to close | `v7UnclosedDependency` |
| 5 | task `evidence_required` kinds all present | `missingRequiredEvidence` |
| 6 | close policy: acceptor namespace, policy evidence, required gate kinds | `enforceV7ClosePolicy` |
| 7 | no placeholder acceptance without a waiver; no acceptance/mode gaps | `enforceV7AcceptanceClose` |

Close policy defaults are objective for every risk: `required_acceptor: reviewer_agent`, no required
evidence, no required gates (`internal/v7policy/close.go`). `reviewer_agent` accepts `reviewer:`
**or** `human:`; `human` accepts only `human:`. A repo config declaring any
`required_acceptor` other than `reviewer_agent` — `human` included — is rejected as
invalid config (`validateV7ClosePolicyConfig`), as is an unknown evidence or gate kind;
`tusker migrate close-policy [--write]` (`cmd/tusker/close_policy_migration.go`) rewrites it to
`reviewer_agent`, drops `required_gates`, resets `WORKFLOW.md` `reviewer.auto_close_risks` to all
four risks, and deletes `human_required_risks`.

`accept` never re-judges proof: it reads the report, refuses with `EVIDENCE_GATE` on any
acceptance/mode gap, requires an explicit `--by reviewer:`/`human:` identity (no default actor), runs
the full close preflight *before* writing, then composes `status … review` + `close`. If close still
fails it reports the task as stranded in review rather than pretending nothing happened.

Closing sets `status: done`, `readiness: done`, `proof_status: satisfied` (unless
`waived`), `accepted_by`/`accepted_at`/`closed_at`, projects
`next_owner: none` / `next_source: status` with empty `next_ref`/`next_action`, and
clears `agent_action`/`machine_status`/`human_status`/`closeout_status`
(`applyV7TaskCloseProjection`). The write re-checks identity and `state_rev`
*under the document lock* (`saveV7CloseProjectionCAS`) — the preflight read is only
a hint. Close then emits the `closed` event, retires runtime rows, and reaps
`.tusker/scratch/<TASK-ID>/`.

## Close authority and completion receipts

An interactive close writes no `close_authority`. Daemon-driven completion closes
attach one (`cmd/tusker/v7_close_authority.go`) binding transaction ID, receipt
ID, task ID, review result revision, reviewed `state_rev`, close-authority
fingerprint, actor, `closed_at`, and a self `binding_fingerprint`.

That frontmatter fact is a **projection, never authority** (`authenticateV7TaskCloseAuthorityCommitWithStore`).
Verification needs the exact issuing daemon store and proves, against
`refs/heads/<integration branch for the task's wave>`, that a reachable commit carries
`Tusker-Completion: <transaction id>`, its task blob is byte-identical to the file on disk and still
the blob at the tip, the committed task holds the same fact and wave, and the sibling receipt at
`.tusker/completion-receipts/<digest>.json` validates. Every digest in the markdown is public, so
none of it authenticates alone.

Schemas and identities: the fact is `tusker.task-close-authority/v2` with
`binding_fingerprint` = sha256 of itself minus that field; the receipt is
`tusker.completion-receipt/v2` with ID `receipt:` +
sha256(`"tusker.completion-receipt/v2\0" + transaction_id`); its embedded close
projection is `tusker.completion-close-authority/v2` whose sha256 must equal the
transaction's frozen `CloseAuthorityFP`. `validateCompletionReceipt` (`cmd/tusker/v7_completion_receipt.go`) also requires
byte-canonical JSON, a 64-byte completion-authority signature, a non-legacy review
result deep-equal to the consumed one, every frozen transaction field to match, and
the transaction ID to re-derive from its inputs. Completion `closed` events get a
deterministic ID (`close-` + first 32 hex of the binding fingerprint), written
link-then-verify so replay is idempotent and a differing file at the same path is a
`CAS_CONFLICT` (`writeDeterministicV7CloseEvent`).

## Scratch retention and GC

Scratch is ephemeral by contract. `tusker gc [--ttl <days>] [--yes] [--json]`
(`cmd/tusker/scratch_gc.go`) sweeps every top-level entry under `<vault>/scratch` —
task-keyed or hand-named — whose **newest mtime beneath it** predates the cutoff.

- Default TTL 14 days; `--ttl 0` purges everything; above 100000 days is rejected
  (a duration overflow would wrap the cutoff into the future). Dry-run by default;
  `--yes` is the only apply flag and is parsed strictly — a non-boolean value is an
  error, not a deletion (`scratchGCConfirmed`).
- Deletion is authorized only when the path is provably a Tusker vault (v7 layout
  plus one of `WORKFLOW.md`, `SKILL.md`, `config.yaml`, `_system/`) and `scratch`
  is a real directory, not a symlink (`resolveScratchRoot`).
- Apply holds a flock at `_system/locks/scratch-retention.lock` (10s timeout),
  re-measures each entry immediately before deleting, and skips anything that
  became fresh or whose task has a live run process group
  (`applyScratchGCUnlocked`). Deletion targets exactly one direct child through
  `O_NOFOLLOW` directory FDs (`removeScratchChild`).
- On failure it reports what it already deleted and exits non-zero
  (`SCRATCH_GC_FAILED`). Sizes are logical `FileInfo` sizes, not freed blocks.

Close also reaps `<vault>/scratch/<TASK-ID>` (`reapTaskScratch`), but no-ops when
the ID is not a canonical task ID, the vault does not authorize deletion, or any
evidence record is `artifact_durability: link_only` with a path inside
`scratch/<TASK-ID>/` — a check that fails **open** on anything it cannot read
(`taskHasLinkOnlyScratchEvidence`).

Task frontmatter and lifecycle: [[tasks-and-proof]]. Gate kinds, satisfaction, and
batch windows: [[gates]]. Waves, integration refs, and the completion reactor:
[[orchestration]].
