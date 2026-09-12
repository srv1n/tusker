# M1 pilot — prepared, not run

The independently rerun public mailbox acceptance is **11/11 PASS** on the Pro-reviewed candidate `a081654635afc6d1fad2e71b5c82ccca772c2487710cbedb45f4dd19ad503dad`. [Fresh receipt](acceptance-independent-recheck-2026-09-11.json). This pilot tests the remaining real-agent journey. It has not launched a model.

## Prepared fixture

- Repository: `/private/var/folders/wg/6tr40jxx4fz2l6pg1yjjs74c0000gn/T/tusker-coordination-m1-pow0b5mw/repo`
- Pinned executable: `/private/var/folders/wg/6tr40jxx4fz2l6pg1yjjs74c0000gn/T/tusker-coordination-m1-pow0b5mw/bin/tusker`
- Isolated state: `/private/var/folders/wg/6tr40jxx4fz2l6pg1yjjs74c0000gn/T/tusker-coordination-m1-pow0b5mw/repo/.runtime`
- Project: `01M279RSG4VB3Z3HTBX3J1BX91`
- Wave: `W-0001`; architect `MCO-T-0001`; worker `MCO-T-0002`.
- Both execution and independent review select `gpt-5.6-luna`, effort `low`. Execution uses workspace-write-offline; the reviewer uses read-only. No fallback list or subagent fanout is configured for these routes.
- Project execution capacity: one. Polling interval: five seconds. Fixture Serve address: `127.0.0.1:17421`.
- Task contracts, spec, workflow, artifacts, isolation, profile routes and approval-policy checks pass. Delivery review: `planValid=true`, `importReady=true`, `startReady=false`.
- Remaining preflight blockers: daemon must be alive/reconciling; project must be enabled/healthy. Wave remains disarmed.

[Preparation receipt](m1-preparation-2026-09-11.json), [task plan](/private/var/folders/wg/6tr40jxx4fz2l6pg1yjjs74c0000gn/T/tusker-coordination-m1-pow0b5mw/repo/m1.plan.yaml), [artifact checker](/private/var/folders/wg/6tr40jxx4fz2l6pg1yjjs74c0000gn/T/tusker-coordination-m1-pow0b5mw/repo/check_m1.py). The fixture is in a temporary directory; retain or relocate it through supported project operations before OS cleanup.

The profile setup checks found the installed executable and authentication. They ran with `live=false`, `ready=false`; they do not certify live route behavior. The public packet resolves the worker's architect contact. Both artifact checks fail before results exist, as required.

## Start the separate runtime

Run these commands in a separate operator terminal, leaving it open:

```sh
source /private/var/folders/wg/6tr40jxx4fz2l6pg1yjjs74c0000gn/T/tusker-coordination-m1-pow0b5mw/env.sh
tusker daemon run
```

This starts a runtime scoped to the fixture. It does not enable the test project or arm its wave. Do not replace, restart or repurpose the existing shared daemon. The shared daemon was alive during inspection; its active registry included the backend project, not an enabled coordination fixture.

The primary interactive session must not execute this launch. The repository [AGENTS.md](/Users/sarav/Downloads/side/tusker/AGENTS.md) explicitly says: “Never start `tusker daemon run`, invoke `tusker automation dispatch`, or launch nested `codex exec`/`claude -p` workers from an interactive agent session.” This is the reason for the separate operator terminal. It does not prevent preparing or observing the test.

Once that runtime is running, the driving session should source the same environment, enable only the fixture with `tusker projects enable --id 01M279RSG4VB3Z3HTBX3J1BX91 --json`, and rerun delivery review/preflight. If settings or the default branch changed, refresh the context through `tusker delivery context --spec .tusker/specs/coordination-m1.md --scope coordination-m1-20260911 --json`, update the plan's context fingerprint, and import the held plan again. The scope argument matters; a newly generated template's default scope does not describe this imported wave.

Start only the exact reviewed plan via `tusker delivery start --plan m1.plan.yaml --confirm <current-plan-fingerprint> --by human:sarav`. That is the one initial test start. Subsequent questions, replies and worker continuations must be driven by Tusker. Stop at the first actionable failure and retain evidence. Do not launch nested provider commands to repair a failed route.

## What must happen

1. The architect can wait safely for a question. One initial setup turn is allowed and counted separately. If no public operation can release an idle architect safely, report that missing operation; do not keep it alive by polling, sleeping or a dummy question.
2. The worker asks its recorded architect, using key `m1-color-question`, and yields the single execution slot. It cannot finish or choose a default.
3. Tusker wakes the architect for that question. The architect replies exactly `blue`, correlated to the durable question, and writes `architect-answer.json` with question and answer IDs.
4. Tusker resumes the legitimate worker. Only after the correlated answer, it writes `decision.txt` with `color=blue` and `question_id=<actual ID>` on separate lines.
5. Ordinary proof and independent review run. The reviewer does not fabricate coordination evidence.

No operator-authored question/answer, copied message, manual retry/resume, or direct provider invocation counts as a pass. An initial ready-task dispatch is not by itself proof of a message-caused wakeup: record and distinguish the initial setup turn from the turn caused by the worker's question.

## Evidence and verdict

Capture the question and answer from `tusker message list --project 01M279RSG4VB3Z3HTBX3J1BX91 --json`, current task/run records, native provider task/turn IDs, wake cause, released/reacquired ownership, and result artifacts. Keep initial setup, coordination execution and review turns separate. There must be no model polling or relay turns. Observe ordering: durable question → released slot → architect answer → legitimate worker continuation → result.

Run `python3 check_m1.py architect` and `python3 check_m1.py worker` in the respective task workspaces with the fixture environment. These checks prove artifact/correlation facts only. A file plus green checker cannot certify actual wake/resume, single-owner behavior, or turn counts.

Verdict stays **NOT RUN** until this live timeline is observed. On failure, record the first public command or transition that fails; do not replace the demonstration with SQL rows or internal test helpers.

After M1, run the [manual scenarios](independent-acceptance.md#manual-tests-in-order): peer-only exchange, restart while waiting, busy recipient, five-task stalled wave, then automatic next wave and stop. Repeat the basic round trip through Muse before calling that route qualified. Live steering and crash-after-remote-acceptance remain separate tests.
