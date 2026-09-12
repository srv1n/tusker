# Independent coordination acceptance — 11 September 2026

## Current independent recheck

The unchanged public CLI acceptance script was independently rerun against the exact Pro-reviewed candidate: **11 PASS, 0 FAIL**. SHA-256: `a081654635afc6d1fad2e71b5c82ccca772c2487710cbedb45f4dd19ad503dad`. [Fresh command receipt](acceptance-independent-recheck-2026-09-11.json). No provider was launched.

The former three public failures are repaired on this candidate. The source-review findings below describe the earlier snapshot; the [Pro-review reconciliation](pro-review-reconciliation.md) reports their repairs. This recheck does not independently certify those source changes or live scheduler behavior.

A separate two-participant M1 fixture is now prepared and imported, with valid contracts, explicit Luna low profiles and one execution slot. [Pilot and operator instructions](m1-pilot.md). It remains disarmed; live M1–M6 are **NOT RUN**.

## Repair verification — 11 September 2026

The implementation was repaired against this script without changing the
script. A freshly built candidate was copied into a new disposable fixture and
all **11/11 checks passed**. Receipt:
`/tmp/tusker-coordination-after.json`.

The shared fixes canonicalize packet project identity and typed addresses,
reject an idempotency key reused with changed material, validate both directions
of a correlated reply, and enqueue one durable wakeup for every substantive
public message. Task packets now include unapplied messages and runnable reply
commands. Wakeup scheduling uses an optimistic snapshot and refuses to replace
a live owner. Daemon polls now emit deduplicated completed/stalled wave reports
to the recorded architect; the architect continues through existing delivery
commands and authorization rather than a bypass.

The live M1 agent demonstration remains separate: project automation is still
intentionally disabled, and interactive sessions are forbidden from starting a
resident daemon. The public mailbox round trip passes and scheduler wiring is reported repaired;
an independently running enabled test daemon is still required to claim an
actual provider wake/resume and `decision.txt` artifact.

## Original pre-repair findings — historical

**Original verdict: the mailbox has useful working pieces, but the requested agent-to-agent journey is not yet end-to-end qualified.** Public CLI testing found three reproducible failures. A separate Sol review at medium effort found that public messages and wave completion are not connected to the required model wakeup/continuation paths.

The primary session inspected reports, public commands and the qualification script, and wrote black-box tests. Implementation source review was delegated. No product implementation was changed, no daemon or worker was launched, and no provider conformance model turn was repeated. The reviewer itself used model tokens; the acceptance script starts zero models.

## Run the independent check

From the repository root:

    python3 scripts/test-agent-coordination-acceptance.py --output /tmp/tusker-coordination-acceptance.json

The script copies the selected installed executable into a fresh temporary directory, initializes a disposable repository and isolated TUSKER_STATE_ROOT, registers a disabled project, and creates five held test participants. It uses public CLI operations exclusively. Each operation is a separate process. It never reads or edits SQLite, invokes internal Go helpers, arms work or launches a model. Failed checks produce a nonzero exit.

Use --tusker /absolute/path/to/candidate to test an explicit candidate. The JSON report records the exact copied binary hash, commands, outputs, project identity and retained fixture directory. No build is performed. The fixture is deliberately a mailbox test, not a runnable five-worker wave: its held placeholder task contracts cannot establish live execution.

Original pre-repair evidence: [command receipts and results](acceptance-2026-09-11.json).

- Source executable: /Users/sarav/Downloads/side/tusker/dist/tusker.
- Tested snapshot SHA-256: 50f8df06a17d1ddd8921334b4b71a9fde50ca1d923b8e2f699c2a64f1a3de7e8.
- Result: **8 PASS, 3 FAIL**.
- Process restart here means reopening the mailbox through a new CLI process. Daemon crash recovery and provider session resumption were not exercised.

| Public behavior | Result |
| --- | --- |
| Task packet includes architect, origin and named peer | PASS |
| Packet's own ask example routes to the canonical project and recipient | FAIL |
| Asking through the contact route preserves content, reply intent and queued state | PASS |
| Identical request with the same key returns the original message | PASS |
| Changed request using the same key produces an explicit conflict | FAIL |
| Saved question survives a fresh CLI process | PASS |
| Normal architect answer is accepted and correlated | FAIL |
| Answer addressed to another task is rejected | PASS |
| Another project cannot read the question by ID | PASS |
| Named peer question resolves to the recorded peer | PASS |
| Payload above 32 KiB is rejected | PASS |

### Failure 1: the packet gives the worker a broken example

Substituting the displayed architect contact into the packet's own ask example produces:

    projectId: repo
    recipient: {kind: task, id: "task:ACT-T-0001"}

The registered runtime project was 01M27283GETVXERBXBNMVQPHX7, and the task recipient ID was ACT-T-0001. The example puts the message in a different project namespace and retains the type prefix inside the task ID. The test invokes the displayed command with argument arrays; it does not invent an alternative parser.

### Failure 2: a changed request is silently discarded

The test repeats worker-question with a different body. The CLI returns success, duplicate=true and the old body. It should reject a key reused for different material, allowing the caller to correct the request; silent success loses the changed question. Identical retries already pass.

### Failure 3: the ordinary correlated answer cannot be sent

Question sender: task:ACT-T-0004. Architect recipient: task address ACT-T-0001. The answer uses sender task:ACT-T-0001, recipient-kind task, raw recipient ACT-T-0004 and the returned question ID.

The CLI rejects it:

    message rejected: reply recipient does not match original sender

Do not change the acceptance test to send task:ACT-T-0004 as the raw recipient. The reviewer found that such a workaround then cannot match the runtime's raw task ID. Fix address normalization at the common boundary.

## Original source review — historical evidence

These findings come from the delegated source review, not from a live daemon test. They apply to the reviewed dirty source snapshot and should be checked against subsequent fixes.

| Priority | Finding and direct source evidence |
| --- | --- |
| P1 | Public CLI/Serve asks only persist messages. Wakeup creation has test callers but no production caller; yield intent has no production consumer. The daemon consumes already-created wakeups. [CLI save](../../../cmd/tusker/agent_message_commands.go#L86), [Serve save](../../../cmd/tusker/serve_agent_messages.go#L45), [wakeup processing](../../../cmd/tusker/agent_coordination.go#L44). |
| P1 | Wave report/proposal/application methods are called only by tests. No completed/stalled-wave path creates a report, wakes the architect, receives a proposal and invokes delivery. [Continuation methods](../../../cmd/tusker/agent_coordination.go#L144), [daemon polling](../../../cmd/tusker/daemon.go#L693). |
| P1 | A task wakeup rewrites its run to retry_queued without protecting a busy lease's owner/generation/state. Wiring this path directly can allow a second writer. [Wakeup](../../../cmd/tusker/agent_coordination.go#L78), [runtime update](../../../cmd/tusker/runtime_store.go#L4198). |
| P1 | Continuation application invokes its mutating callback before claiming application in the database. Concurrent callers can both create/start work. Configured MaxWaves and proposal AutoStart are not consulted. [Application](../../../cmd/tusker/agent_coordination.go#L167). |
| P1 | CLI sender text is trusted; reply validation checks the reply recipient but does not authenticate the reply sender as the original recipient/current owner. [Sender input](../../../cmd/tusker/agent_message_commands.go#L51), [reply validation](../../../cmd/tusker/agent_messages.go#L79). |
| P2 | Public constructors omit work revision and route generation; consumption/application is an unconditional project/message update. Contact endpoint generation APIs are disconnected from public authoring/routing. [Message construction](../../../cmd/tusker/agent_message_commands.go#L86), [state update](../../../cmd/tusker/agent_messages.go#L137). |

The source review also explains the two independently observed address failures: [packet example](../../../cmd/tusker/commands_v7.go#L2949), [contact parsing](../../../cmd/tusker/agent_contacts.go#L100), [project/recipient ingestion](../../../cmd/tusker/agent_message_commands.go#L14).

The supplied qualification script's live mode runs Codex and Muse print conformance. That proves those execution routes can run a prompt. It does not prove a question reaches an architect, a reply resumes the worker, or a second wave starts. Its internal “five tasks/two waves” check is not a substitute for the public journeys below.

## Manual tests, in order

Use a disposable live test project with a known installed build and explicit test profiles. Use the cheapest qualified profile for these trivial messages; this tests transport and orchestration, not frontier-model reasoning. Keep the architect's selected profile stable within a scenario.

Create/register contacts and work only through the supported CLI/UI. If a required registration/control operation is unavailable, record BLOCKED with that missing operation. Do not seed wakeup rows or continuation records through SQL or internal test helpers. Operator setup actions are allowed; manually relaying a question/answer or starting the subsequent wave cannot count as an autonomous pass.

### M1 — One question, one answer, one result

Start with two participants: architect and worker. Bind the worker's architect contact through the public surface.

Architect instruction:

> When this test worker asks which color to use, answer “blue” through Tusker's correlated reply operation. Include the question ID. Do not implement the worker's task.

Worker instruction:

> Ask your recorded architect which color to use. Do not choose a default. Save the question and yield while its answer is required. After receiving the correlated answer through Tusker, write decision.txt containing color=blue and the question ID, then finish through the normal task process.

**Pass:** one question, one architect wakeup caused by it, one correlated answer, continuation of the correct worker, and decision.txt written once after the answer. Inspect actual provider session/turn IDs and the file. No copy/paste, manual wake or user-authored answer.

**Current status:** NOT RUN live. The CLI reply now passes independently; wakeup/yield repairs are reported. See the prepared [M1 pilot](m1-pilot.md).

### M2 — Ask a peer without waking the architect

Give a second worker the agreed contract value schema=2. Have the first worker ask its named schema peer and write the returned value into peer.txt.

**Pass:** the peer answers through the correlated route; peer.txt contains schema=2; the architect receives no model turn for this exchange. An addressable peer conversation is retained after its earlier work finishes. Dependency order and ownership remain unchanged.

**Current status:** contact resolution passes in the CLI; actual peer wake/reply/resume is NOT TESTED.

### M3 — Restart while waiting

Repeat M1, stopping the test runtime through the normal operator control after the question is durable and before the architect handles it. Restart that same test runtime and allow delivery. Repeat once with the answer durable before the worker resumes.

**Pass:** the original message/question IDs survive, the answer reaches the current legitimate worker, and there is one accepted reply and one result file. No second question, extra architect turn or duplicate task attempt is caused by replay.

**Current status:** fresh-CLI-process persistence passes; live runtime restart is NOT TESTED.

### M4 — A busy recipient stays a single owner

Have the architect perform a bounded task long enough to observe an active turn. Send the worker's question while it is busy. Repeat on a route that does not support active injection.

**Pass:** verified Codex steering targets the expected active turn, or the message queues for the next safe turn. There is never a second active writer for that conversation or a task lease overwritten to make room. Unsupported active delivery is visibly queued. Keep stale-turn rejection separate from successful steering.

**Current status:** NOT TESTED live. The earlier lease overwrite risk is reported repaired; live single-owner proof remains open.

### M5 — Five tasks, task four stuck

Run five tiny, independent file-writing tasks. Task four must ask for its color before writing. Let the other four complete first.

**Pass:** independent tasks progress while task four waits; the wave is visibly incomplete; the architect receives a truthful clarification/stalled report; its answer resumes task four; the wave completes only after the required evidence/review/landing. Repeated unchanged observations cause zero extra architect turns.

**Current status:** NOT TESTED live. Clarification and stalled-wave wiring are reported repaired. A database fixture with five task strings does not satisfy this test.

### M6 — Architect creates the next wave, then stop works

Give the architect a two-step objective: first obtain decision.txt, then create a subsequent task that writes summary.txt from that accepted result. Use configured automatic continuation for this disposable objective.

**Pass:** completed-wave evidence wakes the architect, its proposal goes through public delivery validation/import and configured start, and summary.txt is produced without a second manual Play. Record trigger ID, architect turn, proposal and applied wave ID. Repeated completion events must create no extra turn or wave.

Repeat with a stop immediately before the next wave starts. **Pass:** no new model turn or wave dispatch starts after the acknowledged stop, and the proposal remains inspectable. Resume once through the normal control and observe a single subsequent wave.

**Current status:** NOT TESTED live. Production continuation/delivery wiring is reported repaired. Persisting an “applied” flag alone cannot pass.

### Small UI check alongside M1

Open task four in the existing inspector. Verify architect/origin/peer links, send one question, refresh, inspect the exact message and correlation, and observe the eventual answer/wait state. Try the narrow window once. Queued must not be labeled delivered or answered. Save before/after screenshots. No rendered-browser pass was claimed in this independent run.

## Original repair order — retained for history

1. Make the independent script green: normalize project and typed addresses at the shared boundary, repair the packet's runnable example, and reject conflicting idempotency reuse. Preserve its fresh-runtime/public-CLI approach.
2. Complete M1 through public messages, authenticated replies, safe waiting, recipient wakeup and message-bearing worker continuation. Fix busy-lease handling and revision/endpoint checks before connecting scheduler dispatch.
3. Prove M2–M4 with actual turn/session evidence and zero bookkeeping model calls.
4. Connect completed/stalled wave reports, architect proposals and existing delivery operations. Fence application before side effects and honor stop/continuation limits. Prove M5–M6.
5. Rewrite the qualification report around the observed public journeys. Keep source, mailbox, protocol, live-provider and UI evidence separate. Experimental history injection is not a prerequisite for the basic mailbox/resume loop.

Related tracked work: ACO-T-0001 through ACO-T-0007. Independent findings were recorded through Tusker feedback; this review did not change their lifecycle state.
