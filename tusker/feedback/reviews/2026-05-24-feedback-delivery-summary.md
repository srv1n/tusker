# Feedback Delivery Summary - 2026-05-24

Status: delivered slice plus shaped backlog

Owner view: agent feedback is telling us the same thing from several repos: Tusker must make the happy path shorter, make blocked proof honest, and keep repo-level instructions tiny. The product work splits into platform fixes we can ship in Tusker and downstream product stories that belong in the owning repos.

## Product Goals

| Goal | Why it matters | Status |
|---|---|---|
| Reduce token burn from repo instructions and proof logs | Agents were rereading root guidance, large task files, raw logs, and status-like feedback notes. | Patched in first batch; reinforced in this slice |
| Make proof closure self-explanatory | Agents were adding partial proof and then burning turns discovering what was still missing. | Patched in this slice |
| Keep feedback useful, not a progress diary | Several notes were status updates disguised as product feedback. | Patched in first batch |
| Avoid dead routes in task packets | Agents were told to read missing project skills/domain canon files. | Patched in this slice |
| Route work by domain/lane | Backend/rznapp feedback shows broad queues produce wrong task pickup and mega-task drift. | New backlog |

## Already Patched In VSD-T-0030

| Story | Delivered behavior | Product impact |
|---|---|---|
| AFS-001 | Mixed legacy/V7 task ID collisions are detected before writing and validation reports exact repair paths. | Stops invalid task stubs and cleanup churn. |
| AFS-002 | Protected durable state errors explain the V7 attempt -> verify -> handoff -> finish flow; capsules/briefs surface runtime attempt state. | Agents can keep working without guessing about `active`. |
| AFS-003 | `verify add` has proof locking, deterministic retry hints, and batch rows. | Fewer CAS loops and fewer repeated proof writes. |
| AFS-004 | `tusker feedback add` writes small structured notes with required product fields and size budgets. | Feedback becomes product signal instead of transcript bloat. |
| AFS-005 | `tusker feedback digest` groups multi-repo feedback by theme, repo, priority, command, and malformed notes. | Review no longer requires opening every repo note manually. |
| AFS-006 | Install/update/sync create feedback templates and concise AGENTS/CLAUDE bootstrap pointers idempotently. | Downstream repos stay flat while Tusker skill carries the detailed workflow. |
| AFS-007 | Scoped verification recipes can be defined and suggested from `verification-recipes.yaml`. | Agents can prove owned work without over-running shared workspaces. |
| AFS-008 | Blocked verification rows require `--blocked-by` and separate external/shared blockers from owned proof gaps. | Handoffs stay honest without pretending blocked proof passed. |
| AFS-009 | `skill audit-agent-guidance` classifies unmanaged AGENTS/CLAUDE guidance and can write migration drafts. | Critical repo rules can move into project skill/canon instead of bloating root prompts. |

## Patched In VSD-T-0031

| Story | Delivered behavior | Product impact |
|---|---|---|
| AFS-010 | After `tusker verify add`, output now includes `Remaining proof gaps: ...` grouped by owner/class, and `finish` proof errors include the same compact gap summary in the hint. | Agents see the next proof action immediately after adding proof. |
| AFS-011 | If `tusker finish <task-id>` has satisfied proof but no attempt, the error explains attempts are runtime/session state and prints `tusker attempt start <task-id>` plus the finish retry. | Routine closeout becomes recoverable without reading help or inspecting task internals. |
| AFS-012 slice | Agent/reviewer packets now warn when `tusker/SKILL.md` is missing, domain route files are missing, or acceptance looks placeholder/vague; project routing falls back to README/task contract instead of dead links. | Packets stop sending agents into missing files or vague proof loops. |
| Validation hygiene | Validation now ignores superseded historical closeout fingerprints when a newer valid closeout exists. | Old closeout history no longer blocks delivery validation after a refreshed checkpoint. |
| Summary artifact | This file separates delivered Tusker platform work from new backlog and downstream product stories. | Product planning gets a clean rollup instead of raw agent notes. |

## Still-New Backlog

| Story | Recommendation | Reason |
|---|---|---|
| AFS-012 full enforcement | Add policy so proof cannot become `satisfied` when acceptance is still stub/placeholder unless a waiver exists. | The packet warning is useful, but the ledger should eventually prevent dishonest closure. |
| AFS-013 | Build `tusker next --domain <name>` and `--lane <name>`, plus skip/explain output and lane-split helper. | This is the clearest remaining platform gap from backend/rznapp feedback. |
| AFS-014 final polish | Finish any remaining managed-bootstrap/audit repair paths under one compact-command policy. | Most guidance is patched, but repair should be turnkey for old repos. |
| DPS-001 to DPS-009 | Promote into Cinta, backend, and rznapp trackers before implementation. | These are product-specific, not Tusker platform changes. |

## Suggested Next Build Order

1. AFS-013 domain/lane routing, because it prevents wrong task pickup and over-broad work.
2. AFS-012 proof-blocking for stub acceptance, because warnings alone still allow bad closure.
3. Downstream DPS promotion into each owning repo, starting with backend domain-pack contract completion and rznapp canonical projection debug leakage.

## Verification Snapshot

| Covers | Check | Result |
|---|---|---|
| VSD-T-0031 A1-A4 | `/opt/homebrew/bin/go test ./cmd/tusker -run 'TestV7(VerifyAddPrintsRemainingProofGaps\|FinishWithoutAttemptPrintsRecoveryCommand\|PacketWarnsOnMissingRoutesAndStubAcceptance\|ValidateIgnoresSupersededCloseoutFingerprint)$' -count=1` | pass |
| VSD-T-0031 A1-A4 | `/opt/homebrew/bin/go test ./cmd/tusker -count=1` | pass |
