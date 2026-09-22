# Task authoring completion review

The task-authoring path is implemented across the shipped skill, V2 intake,
task records, tier routing, task and wave UI, author identity, admission and
qualification tooling. The architect chooses `light`, `standard` or
`demanding`; configured tier mappings choose the harness, model and effort.
Human work remains a named gate. Batch plans create a visible wave, while a
standalone task remains valid without a wrapper wave.

## Independent review disposition

| Finding | Resolution |
| --- | --- |
| Direct task authoring could silently choose Standard | Fixed. `tusker new task` and create-task proposals now require an explicit work level. Legacy readers and internal fixtures retain their compatibility default. |
| Unknown author context was omitted | Fixed. Fresh direct and imported tasks record `source: unknown`, `binding_state: unbound` when no trusted host identity exists. |
| A partial runtime contact list could hide authored contacts | Already fixed in the reviewed tree. The identity projection unions registered and authored contacts and marks unmatched authored contacts unbound. |
| Equal work and review levels were rejected without a review reason | Fixed. A reason is required only when review deliberately differs from work. |
| Review routing could let legacy complexity override an explicit work level | Fixed. Review inherits the explicit work level before legacy complexity is considered. |
| A self-implementation claim or review could proceed without a comparable native conversation ID | Fixed. Current-workspace claims require a known Codex or Claude conversation, and independent review fails closed when either native identity is unavailable. |
| Create-task proposals dropped review-level fields during apply | Fixed. Proposal capture and apply preserve `review_level` and `review_reason`. |
| Live qualification accepted receipt assertions without matching installed authorities | Fixed fail-closed. Failed/corrected attempts, self claims, reviewer conversations and workspaces are correlated with installed runtime history, with each authorization bound to its exact attempt. The lane now returns explicit failure for blocked-start, mapping-history and completed-human-gate claims until the runtime exposes durable authorities for them. |

## Current proof

| Lane | Result |
| --- | --- |
| Focused Go authoring, routing, identity, Serve and contact tests | PASS |
| Direct CLI and create-task proposal tier enforcement | PASS |
| V2 equal-tier review inheritance | PASS |
| Deterministic offline authoring journey | PASS |
| Weak live-receipt rejection | PASS; unavailable installed authorities now prevent a live PASS |
| UI authoring experience | PASS, 6 tests and 36 assertions |
| TypeScript typecheck | PASS |
| Delivery doctor | PASS, five ordered frontiers and zero findings |
| W-0023 validation | PASS, zero errors and warnings |
| FLW-T-0037 through FLW-T-0041 validation | Zero errors; three plain-language warnings remain in 0039 through 0041 |
| Contract convergence release gate | PASS; independently reproduced in 90.27 seconds and accepted as `ORC-T-0088-E-0001` |
| Final independent re-review | PASS; no unresolved actionable findings, including exact authorization-to-attempt binding and legacy migration |
| Repository validation | PASS, zero errors; 121 non-blocking warnings remain classified below |
| Strict skill doctor | PASS, zero errors; 77 historical/scoped warnings |
| Candidate build | PASS, `/tmp/tusker-w0023-candidate`, SHA-256 `001f579c98dfe7012a250a589f6f0a966bc9c349263fcdd98657b403a2b4c3de`, 49,621,986 bytes |
| Diff whitespace check | PASS |

The original 45 repository errors are closed. Four historical readiness tasks
now carry durable test-result evidence; ORC-T-0087 carries the current skill
benchmark; ORC-T-0088 carries independently accepted convergence evidence.
The orphan decision and stale task spec references were repaired through the
document/proposal authorities. The remaining landing/completion documentation
obligation is a detailed Light task in disarmed singleton wave W-0024.

The 121 remaining warnings are not release errors: 49 historical high-risk
tasks lack a knowledge-delta note, 35 records retain migrated dangling spec
paths, 16 historical tasks exceed a zero evidence budget, and 21 are smaller
documentation, capsule, proof-format, review-state, or proposal-rationale
warnings. Treat the dangling references and active review-state warning as
targeted migration/lifecycle work. Bulk rewriting old knowledge deltas or
evidence budgets would manufacture history and should not be done merely to
reach zero warnings.

## Remaining installed qualification

Browser, installed-run and human acceptance stay open because this attached
agent session cannot start the resident daemon, choose the user's real tier
mappings, impersonate a human actor or manufacture execution receipts.

The operator setup must provide a stable human-started Serve endpoint and a
registered disposable project with one classified wave task, one wave-free
classified task, one named open human action, explicit execute and review tier
mappings, one task safe to fail and correct, and one task eligible for a
current-conversation claim.

Run the read-only browser lane with:

```sh
TUSKER_REALWORK_BASE_URL=http://127.0.0.1:<port> \
TUSKER_REALWORK_PROJECT=<project-id> \
TUSKER_AUTHORING_TASK_ID=<classified-wave-task-id> \
TUSKER_AUTHORING_WAVE_ID=<wave-id> \
TUSKER_REALWORK_OUT=/tmp/tusker-task-authoring-browser \
python3 scripts/test-task-authoring-journey.py \
  --scope authoring \
  --mode browser \
  --report /tmp/tusker-task-authoring-browser.md
```

The authorized runtime trial must preview both lanes, remove a mapping after
preview and prove Start creates no directive or attempt, restore the mapping,
execute through the configured profile, record and correct one real failed
check, review through a distinct attempt and conversation, accept the result,
complete the named human action, and exercise:

```sh
tusker work start <task-id> --by current-conversation --current-workspace
```

The final JSON receipt must match the installed binary and runtime authorities.
It must include requested, admitted and actual execute/review profiles and
policy fingerprints; attempt IDs; route preview revision and fingerprint;
mapping removal/restoration times; proof that blocked Start created no work;
failed-check and correction attempts with output; human gate ID, owner, action
and receipt; self-claim attempt, actor, conversation, workspace and trigger;
the reviewer conversation; observation identity; and true corrected,
independent-review and accepted outcomes. Validate it with:

```sh
TUSKER_LIVE_RECEIPT=<receipt.json> \
TUSKER_LIVE_BUILD_ID=<installed-build-id> \
TUSKER_LIVE_PROJECT_ID=<project-id> \
TUSKER_LIVE_TASK_ID=<task-id> \
python3 scripts/test-task-authoring-journey.py \
  --scope authoring \
  --mode live \
  --report /tmp/tusker-task-authoring-live.md
```

Fixture, built-browser, installed-runtime and human-acceptance evidence remain
separate. The authoring source is complete; product qualification closes only
after the last two commands pass against the intended installed candidate.
