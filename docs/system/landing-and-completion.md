---
title: "Landing and completion (how finished work reaches the integration branch and main)"
subject: landing-and-completion
keywords: [land, completion reactor, landing receipt, completion authority, integration branch, worktree projection, hand-run, integrator packet]
part_of: overview
status: canonical
read_when: "You need to know how a reviewed task's commit actually reaches an integration branch or main — the `tusker land` batch/bisect flow, landing receipts and their Ed25519 authority, the daemon completion reactor's phase machine, worker-safety admission, or why a landing/completion refused."
skip_when: "You only need what proof a task must carry before it is reviewable ([[proof-and-closeout]]), what a gate is ([[gates]]), or how work is dispatched and waves are scheduled ([[orchestration]])."
sources:
  - cmd/tusker/v7_land_cmd.go
  - cmd/tusker/completion_reactor.go
  - cmd/tusker/completion_reactor_mode.go
  - cmd/tusker/completion_authority.go
  - cmd/tusker/completion_worker_safety.go
  - cmd/tusker/landing_authority.go
  - cmd/tusker/worktree_review_projection.go
  - cmd/tusker/worktree_task_seed.go
  - cmd/tusker/integrator_packet.go
  - cmd/tusker/hand_run_marker.go
  - cmd/tusker/upstream_hold.go
---

# Landing and completion

Two independent paths move a finished commit forward. Do not confuse them.

| Path | Entry point | Who runs it | Target |
|---|---|---|---|
| **Landing** | `tusker land <TASK-ID…>` / `tusker land <W-XXXX>` (`cli.go:383` → `landV7Cmd`, `v7_land_cmd.go`) | a human/agent CLI invocation, or the departure scheduler | wave integration branch, then default branch |
| **Completion reactor** | `Daemon.reconcileReviewCompletion` (`completion_reactor.go:277`, called from `daemon.go:1523`) | resident daemon only | wave integration ref, via a resumable transaction |

Gate mechanics live in [[gates]]; dispatch, waves and departures in
[[orchestration]]; what a task must prove before review in
[[proof-and-closeout]].

## `tusker land`

Every land takes an exclusive lock at `<vault>/_system/land.lock`
(`acquireV7LandingLock`, `v7_land_cmd.go:244`) with a PID+host+process-start
owner record and a 30s recovery grace (`recoverV7LandingLock`).

Flow (`landV7TaskTargets`, `v7_land_cmd.go:407`):

1. Group targets by wave. A task with no wave gets an implicit singleton delivery
   unit (`ensureV7ImplicitSingletonDeliveryUnit`); if that still fails, refuse
   with `v7NoWaveRefusal`.
2. Resolve a source commit per task: frozen source (scheduled departures) →
   `task/<ID>` branch → `--from` discovery of a detached worktree
   (`ensureV7TaskLandingBranch`).
3. Demand **source provenance** (`v7LandingSourceProvenance`, line 585). Refuse
   otherwise unless `--trust-from`.
4. Phase 1 — stage each wave's batch into its integration branch, side effects
   deferred (`stageV7WaveBatch`).
5. If **nothing** landed, exit non-zero with `v7FailedBatchSummary` before
   touching any task state.
6. Phase 2 — kick failures to rework (`kickV7LandingTaskToRework`), append the
   wave landing audit, then try `landV7WaveToMainIfReady`.

### Source provenance ranks

| Value | Meaning | Trusted for scheduled landing |
|---|---|---|
| `durable_task_source` | commit equals the task's recorded `source_sha`/`source_commit` | yes |
| `workspace_record` | commit's workspace record ID is the task | yes |
| `workspace_claim` | `--from` dir is the task's workspace and HEAD matches | yes |
| `task_tracker` | commit touches the task's tracker file | yes |
| `task_branch_head` | `task/<ID>` HEAD matches the commit | no |
| `trusted_override` | `--trust-from` escape hatch | no |

`trustedV7LandingSourceProvenance` (line 621) defines the trusted set. Under
scheduled-departure authority an untrusted provenance is accepted only if the
source is already an ancestor of the integration branch (line 483).

### Batch staging and bisect

`landV7BatchRecursive` (line 884) merges the batch in a throwaway detached
worktree (`stageV7LandingBatch`, line 956): `git merge --no-ff` per task,
recording base/merge-commit proofs, then a workspace-metadata cleanup commit,
then the landing gate. On failure the batch **halves and recurses** until a
single task is isolated as the culprit. Only a passing batch does a
compare-and-swap `updateGitRef` from `BatchBaseSHA` to `BatchHeadSHA`.

Conflicts confined to generated projections are auto-resolved
(`resolveV7GeneratedProjectionMerge`, line 1071); any source or task-contract
conflict is a hard failure. `guardV7LandingTerminalTaskRewinds` (line 1204)
refuses a merge that rewinds a terminal task record.

A source already an ancestor of the integration branch is a replay: under
scheduled authority it must be recovered from a **verified receipt**
(`recoverV7LandingAuditFromReceipt`) or the land refuses; without that authority
it is audited as "already integrated; no control-plane receipt".

### Landing gate and receipts

`runV7LandingGateEvidenceWithIsolation` (line 1287) runs `backpressureCommands`
(fallback: `go build ./...`, `go vet ./...`, `go test ./... -count=1`). Scheduled
(`isolated`) runs execute inside `v7GateSandbox` and **never** consult the JSON
gate cache — a cache hit would let a same-UID process mint green evidence.

Receipt schema `tusker.landing-receipt/v3` (`v7LandingReceipt`, line 83) binds
gate fingerprint, lane identity, target, batch base/head/tree, the merge
segment, per-task proofs, commands, toolchain fingerprints, and command
outcomes. Storage (`writeV7LandingReceipt`, line 1824), all under
`<state-root>/landing-cache/<project>/`:

| Artifact | Path | Schema |
|---|---|---|
| Receipt + gate cache record | `<fingerprint>.json` | `tusker.landing-gate-cache/v2` |
| Per-task discovery index | `by-task/<sha256>.json` | `tusker.landing-receipt-index/v2` |

Receipt JSON is **discovery material, not authority** (`landing_authority.go:214`).

### Landing to main

`landV7WaveToMainIfReady` (line 2282) requires scheduled promotion policy to
permit a default-branch advance and every wave member to be `done` on the
integration branch. `landV7WaveToMain` then re-gates the integration ref, syncs
wave control state, builds a two-parent `commit-tree`, prepares checked-out
worktrees (`prepareV7WaveMembersForDefaultAdvance`), CAS-advances the default
branch, and deletes the integration branch. An integration branch already an
ancestor of main is audited as "already landed" and deleted.

## Landing authority (scheduled departures)

`landV7CmdWithFrozenSources` always refuses: frozen sources require a daemon
capability (`v7_land_cmd.go:182`). Only `landV7CmdWithDepartureAuthority`
(caller: `scheduled_promotion.go:90`) carries one. `landV7CmdAsWaveDrain` is a
workflow label, never signing authority (line 193).

`issueV7LandingAuthority` (`landing_authority.go:116`) mints an ephemeral
Ed25519 keypair; the private half stays **in daemon memory only**, the public
issuance row is durable (`landing_authority_issuances`), expires after 30
minutes, and is generation-numbered per departure. The receipt fingerprint is
what gets signed (`stageV7LandingBatch`, line 1059).

`verifyV7LandingReceiptAuthorityWithStore` (line 219; shape and repo identity)
then `…InStore` (line 244) accept only if all hold:

| Check | Detail |
|---|---|
| Shape | schema, self-consistent fingerprint, `control_authority == scheduled_departure`, 64-byte signature |
| Repo identity | `sha256(root ∥ git-common-dir)` — remote URLs deliberately excluded |
| Issuance | exists, not revoked, valid public key |
| Departure run | durable row matches project/policy/window |
| Time | `issued ≤ receipt_issued ≤ expires`, and now ≥ issued−5m |
| Binding | session, host, process, generation, target all equal the issuance |
| Cargo | receipt tasks are a non-empty, duplicate-free, **exact subset** of the frozen candidate, with matching source SHAs |
| Signature | Ed25519 over the fingerprint |

## Completion reactor

Turns a stored, signed review result into mechanical Git/tracker operations. It
never asks a model to resolve a merge or pick a transition
(`completion_reactor.go:3`).

### Modes

`automation.completion_reactor.mode` is read from the **raw** config layer so an
absent value is distinguishable from an inherited one
(`completion_reactor_mode.go:41`).

| Effective mode | Behavior |
|---|---|
| `disabled` | reactor no-ops; fresh-project default |
| `shadow` | records a frozen transaction plan, applies nothing |
| `authoritative` | full transaction, plus `validateCompletionWorkerAuthority` |
| `legacy` | synthesized when an automation-enabled project omits `mode`; no-ops, emits warning + repair hint |

### Transaction phases

`completionTransaction` (schema `tusker.completion-transaction/v4`) is saved
after every step, so a crash resumes rather than repeats.

Success path (`completePassingReview`, line 821):
`planned → staging → staged → gated → ref_intent → ref_committed →
canonical_intent → canonical_done → audited → woken → terminal`.

Failure path (`beginCompletionDisposition` / `resumeCompletionDisposition`):
`failure_intent → failure_handback → failure_released → failure_audited →
terminal`.

| Step | Effect |
|---|---|
| `staging` | `stageExactReviewCompletion` builds the exact candidate commit and binds the task/receipt blobs to the authority |
| `staged` | `gateExactReviewCompletion` on the staged SHA |
| `ref_intent` | **point of no return**: CAS `updateGitRef` on the integration ref (zero-old when creating it) |
| `ref_committed` | authority consumed after CAS (`consumeCompletionAuthorityAfterCAS`) |
| `canonical_intent` | project the done task back into the canonical vault |
| `canonical_done` | emit task-closed event, append wave landing audit |
| `audited` | `scheduleProjectReconcile` — never promotes a default ref |

Verdict routing (`reactToReviewResult`, line 310): `pass` →
`completePassingReview`; `changes_requested` → disposition `rework`;
`blocked` → disposition `park` (never a disguised completion). A rework
handback stamps `<!-- tusker:completion-handback <txid> -->` into the task body;
`validateTerminalCompletionDisposition` refuses a terminal rework row that lacks
that durable effect or still holds an active review lease.

A `COMPLETION_REPAIR_REQUIRED` error parks one task for operator repair and lets
independent reviewed work in the same project continue (line 296).

### Completion authority

Separate from the Git lane on purpose: Git objects are writable by a linked
worktree worker, so the daemon's short-lived private key is what turns a
well-formed receipt into an authenticated close (`completion_authority.go:3`).

- Schema `tusker.completion-authority-issuance/v2`; row in
  `completion_authority_issuances` with the public key; private key in daemon
  memory (`d.completionAuthorityKey`).
- Context binds project, repo identity, transaction, task, result revision, task
  state rev, work revision, implementation SHA, review attempt, wave, worker
  policy fingerprint, integration ref/base.
- Lifecycle: issue → bind (adds `task_blob` + `receipt_blob`) → consume.
- `receipt_blob` is deliberately **excluded from the signature payload**
  (line 193) — it is bound by the daemon-owned issuance and re-proved against
  the exact candidate tree entry at every use.
- `verifyCompletionReceiptAuthority` additionally requires the store identity to
  match the verifying store and the tree entry to be mode `100644` blob.

### Worker safety admission

`completionWorkerSafety` (`completion_worker_safety.go:23`), its lane variant
`completionWorkerSafetyForLane` (line 48; review-profile and project-command
rows) and `validateCompletionWorkerAuthority` (line 418; lane-profile row) gate
which runner profiles may produce a result the reactor will honor:

| Requirement | Refusal reason if unmet |
|---|---|
| sandbox mode `workspace-write` or `read-only` | "enforceable sandbox must be…" |
| not `danger-full-access` | not admissible |
| `sandbox.network` explicitly `false` | must be explicitly disabled |
| harness is `codex_exec` or `codex_acp` | runner does not mechanically enforce the sandbox |
| daemon state root outside the worker workspace | state root inside worker-writable path |
| review lane profile is `read-only` | requires read-only review profile |
| no project-defined runner `command` | project command not admissible |
| profile declared in project/machine-local config, bound to its lane | requires explicit lane profiles |

`codex_exec` gets an exact frozen argv (`--ignore-user-config --ignore-rules
--disable hooks --strict-config --json --skip-git-repo-check`, workspace
`trust_level="untrusted"`, `approval_policy="never"`, network off — line 91).
The binary is resolved on a non-login PATH excluding the workspace and repo,
then content-hashed (`completionExecutableIdentity`). `codex_acp` binds through
a verified adapter bundle receipt instead. Both collapse into a
`worker_policy_fingerprint` that the review result must match exactly, or
completion refuses with "worker policy drift".

## Worktree projection and seeding

- **Seed in** (`worktree_task_seed.go`): `seedCanonicalV7TaskIntoWorkspace`
  copies the canonical task bytes into an isolated worktree vault with `O_EXCL`;
  a pre-existing non-identical record is a `CAS_CONFLICT`.
  `syncCanonicalV7TaskIntoWorkspace` refreshes it before each execute attempt.
  Review lanes and shared workspaces are skipped — a reviewer must see the
  immutable source snapshot, and injecting state would dirty a read-only
  workspace.
- **Project out** (`worktree_review_projection.go`): after an execute run hands
  off, `projectCompletedWorktreeReviewToCanonical` copies the worktree task back
  to canonical. It requires exactly one worktree attempt bound to the runtime
  attempt, `status: handoff`, the checked-out `task/<ID>` branch, a committed
  HEAD, and `status: review` + `proof_status: satisfied`. Canonical `state_rev`
  must still equal the attempt's start revision or it is a `CAS_CONFLICT` —
  an intervening human edit is a loud conflict, never an overwrite. Wave
  membership is re-imposed from canonical (a worker cannot invent or drop it),
  and `work_revision` is incremented here — this is the only place it advances.

## Integrator packets

`integratorPacket` (`integrator_packet.go:15`, emitted via
`v7_migration_cmd.go:45`) renders a merge-lane brief: required reads, workflow
shared namespaces, a per-dependency lane end-state table (branch / HEAD /
worktree / dirty / gate verdicts), a **cross-lane file overlap audit** from
`git diff --name-only <default>...<head>` per lane, and the task's
`## Acceptance` section as the merge contract.

## Hand-run markers and upstream holds

`hand_run_marker.go`: a claim without `TUSKER_ATTEMPT_ID` is hand-run. Every
claim restamps `<lease-dir>/<TASK>.hand_run` — hand claims write it, daemon
claims delete it (`restampHandRun`, called from `commands_pickup.go:49`). The
marker is **task-keyed**, so board/web rows must render `run.HandRun` instead;
`runHandRunOrigin` falls back to the marker only for runs predating that stamp.

`upstream_hold.go`: a task carrying `build_failed: true` holds all its
dependents (`v7HeldByFailedUpstream`), as does a wave carrying
`build_failed_command`. Dependencies in `cancelled`/`superseded` are ignored so
a dead marker cannot pin a dependent forever; `done` is deliberately *not* dead.
This is the dependency-scoped cousin of project-wide quarantine — see
[[gates]].
