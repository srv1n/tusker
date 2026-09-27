---
kind: report
subject: live-harness-qualification
part_of: real-testing-campaign
created: 2026-09-28
read_when: "Checking which harnesses are qualified for live Tusker work, or what the 2026-09-27 pass found."
skip_when: "Changing harness code without needing the qualification evidence."
sources: [../system/proposals/real-testing-campaign.md, ../qualification/live-harness/README.md]
---

# Live harness qualification, 2026-09-27/28

```text
Date:            2026-09-27 evening to 2026-09-28 ~03:00 IST    Operator: human:sarav (run by the Claude session)
Binary:          main fb71afec (F83) via make install; `tusker --version` reports
                 archive/pre-convergence-main-20260727-714-gfb71afec-dirty (nearest tag is an old archive tag)
Daemon/Serve:    TuskerBar-managed daemon, Serve on 127.0.0.1:7420
Project:         01M3HJSNAC8SJ0BBAPZXJPF5A7 (Q6-Q8), reseeded as 01M3J72A00AAH0A8EHQ0QZND2V (Q10-Q12)
Repo:            /tmp/tusker-live-qual (completion_reactor.mode: authoritative in config.local.yaml)
```

Result: Codex, Claude Code and Devin are qualified end to end: dispatch, worker
questions, review by a different harness, landing on the wave integration
branch by the Phase 2 pass handler, and close. Muse is parked by the owner
until a `META_API_KEY` is available. A four-task dependency wave (Q10) ran to
completion with no operator step once the fixes below were in. Every harness
reports a typed reason on a forced failure.

## Per-harness results

`PASS`, `FAIL`, `BLOCKED` (a gap prevents the step) or `NOT RUN`.

| Step | Codex | Claude Code | Devin | Muse |
| --- | --- | --- | --- | --- |
| Profile / model | `codex_exec-gpt-6-luna` | `claude-opus-high` (danger-full-access) | `devin-swe-2-max` | `muse-spark-1.3-high` |
| (a) live preflight ready | PASS (Q3 route check) | PASS | PASS | PASS |
| (b) wave armed → daemon claim | PASS | PASS | PASS | PASS |
| (c) CLI + Serve state and session | PASS (CLI) | NOT RUN | PASS (CLI) | PASS (CLI) |
| (d) Say route (soft/hard), same session | PASS: hard Say resumed the same session with the token in the prompt | NOT RUN | NOT RUN | FAIL: hard Say opened a fresh session and lost the message (F52) |
| (e) ask → Needs you → reply → received | PASS (`indigo`) | NOT RUN | PASS (`teal`, reply resumed the session) | PASS |
| (f) interrupt → Continue, same session | PASS: Serve Stop then `runs continue` resumed the same session | NOT RUN | NOT RUN | NOT RUN |
| close via review | PASS: Devin reviewer, landed `integration/W-0005` dcedf27 | PASS: Codex reviewer, landed `integration/W-0006` ed9be12 | PASS: Codex reviewer, landed `integration/W-0007` 6c54b3e | BLOCKED (parked) |
| as reviewer | PASS (Sol low reviewed Claude and Devin) | NOT RUN | PASS after F60, F71, F72, F74, F80, F83 | NOT RUN |
| (g) failure: admission block or reason code | PASS: failed `provider_error`, driver | PASS: failed `provider_error`, driver | PASS after F81: blocked `config_invalid`, driver | NOT RUN |
| (h) architect wave report | PASS (Q12, inbox level) | NOT RUN | NOT RUN | NOT RUN |

Token totals were not collected for this pass.

## Wave checks

- **Q10, four-task wave (W-0009).** Diamond: base (Codex) → left (Claude) and
  right (Devin) → join (Codex). Dependencies held the three later tasks before
  arming. Base landed, left and right unlocked together and ran in parallel,
  join ran only after both landed and found the left output in its workspace.
  All four results are on `integration/W-0009`; the wave then waits for the
  owner's batch review before departing to main, as designed.
- **Q12, architect report (W-0010).** A task authored with `--architect
  execution:q12-architect` produced a `wave_result` (outcome completed,
  evidence `QLH-T-0009:landed`) in that address's inbox, with a reply command.
  Delivery into a live interactive session through the hook was not tested.

## Findings fixed during the pass

All are logged with evidence in the campaign doc
([real-testing-campaign.md](../system/proposals/real-testing-campaign.md)).

| ID | Finding | Status |
| --- | --- | --- |
| F57 | A Devin worker could not submit: ACP stripped every `TUSKER_*` variable | fixed |
| F59 | Execution retries used the whole attempt cap, so review never dispatched | fixed |
| F60 | A Devin reviewer could not deliver its verdict (plan mode, marker not in raw log) | fixed |
| F61 | A landed task missing from the integration branch aborted every project poll | fixed |
| F62 | One project's typed error stopped the daemon for every project; now isolated per project | fixed |
| F63 | A test interrupted its own process group | fixed |
| F64 | Serve's new-task form had no owned-paths field | fixed |
| F67 | TuskerBar wrote daemon logs only in 64 KB batches | fixed |
| F68 | A reviewed daemon-built submission could not land (no provenance rule) | fixed |
| F69 | The first F62 change skipped every successful poll | fixed |
| F70 | No owner retry after a landing hold; redrive now clears it | fixed |
| F71 | Tusker refused every Devin permission request as malformed | fixed |
| F72 | Devin's review marker came back wrapped and escape-coded | fixed |
| F74 | The marker scan saw only the bare completed update | fixed |
| F76 | A grep check cannot satisfy the default proof_required; the rejection now names the gap | partly fixed |
| F77 | Routine CAS retries logged as skipped polls | fixed |
| F78 | Dependent reviews blocked when the dependency record was not on the integration branch | fixed |
| F80 | The Devin permission check refused quoted metacharacters | fixed |
| F81 | A runner that could not start lost its typed reason | fixed |
| F83 | A completed Devin review was recorded as failed (terminal status race) | fixed |

## Open gaps

| ID | Gap |
| --- | --- |
| F65 | Probably a duplicate of F61; recheck that a redrive of a review-lane run dispatches. |
| F73 | Vault discovery in a review worktree ignores `TUSKER_VAULT`; Devin recovers by adding `--vault`. |
| F75 | `TestRunLivenessClassifier` flakes under load. |
| F76 | Warn at authoring time when no verification row can satisfy `proof_required`. |
| F79 | A done task's run shows `failed` or `blocked` in `runs inspect`; check Serve. |
| F82 | `tusker new task --help` omits `--architect`, `--origin`, `--peers`. |
| Muse | Parked: no `META_API_KEY`; hard Say loses the message (F52). |
| Claude | Steps (c) to (f) not rerun on the current build. |
