# Run and verification

A run directive is deliberate human authority; task creation and inspection
are inert. Follow the entrypoint's execution-mode boundary and the claimed
packet. Read its complete acceptance, governing sections, dependencies, and
owned paths before editing.

Never hard-code a model or silently replace a failed transport. Inspect with:

```sh
tusker work readiness <TASK-ID> --lane execute --json
tusker runs inspect <TASK-ID> --json
tusker runs logs <TASK-ID> --lines 50
tusker closeout status <TASK-ID> --json
```

Runtime activity is separate from durable task status. A heartbeat and lease
establish liveness; exit success does not establish review. Refresh stale state
when it affects the next action. Operator service startup stays in the
operator shell.

Resolve routine implementation choices locally. For conflicting requirements,
missing context, or ownership questions, use the packet's architect/origin/peer
contact or recorded fallback; `HANDOFF.md` explains contact binding. Send only
through an authorized supported route and retain accepted clarification in the
canonical contract. A contact address alone does not prove message delivery.

## Verify the outcome

Choose checks that exercise the acceptance outcomes and relevant failure paths.
Prefer existing tests; `tusker verify recipe <TASK-ID> --files <CHANGED-PATHS>`
can help when the right check is unclear. Run local disposable checks within
the authorized scope, fix failures introduced by the change, and rerun affected
checks without a new approval at each step. For checks touching shared state
or external services, establish prerequisites, permitted mutations, and cleanup
first; the task does not grant missing access or spending authority.

Preserve the command, result, first failure, and artifact path. Register command
checks as pending with `tusker verify add`; the verification executor records
PASS or FAIL through the supported lifecycle. A skip, zero-match test, source
read, or old log is not a pass. Finish when acceptance is covered or name the
specific blocker and unverified outcomes.

Label limits honestly: **synthetic** proves only the fake/demo; **source** proves
what was inspected, not execution; **local executed** proves that checkout and
environment; **installed** proves the identified installed build; **human**
proves only the named observation or acceptance. None implies remote/provider
behavior without observing it.

For a disposable demo, use an empty directory, retain
`tusker demo check --json`, then preview and apply only its owned cleanup:

```sh
tusker demo reset --repo <DEMO-PATH> --dry-run --json
tusker demo reset --repo <DEMO-PATH> --yes --json
```

Never delete shared evidence to make a check repeatable. Live wake/resume or
message delivery stays unverified unless supported by inspected help/source and
actually observed.

## Independent review and repair

Inspect the exact submitted material and the actual check path, not only the
worker's summary. For critical acceptance, identify a plausible wrong
implementation the check rejects. Admission, receiver execution, product
output, recovery, quality and performance are distinct claims. Record a
blocking acceptance gap when the observed boundary falls short; advisory
cleanup is separate. Use the injected typed review submission and identities.

Repair against durable finding IDs and verify each closure on the repaired
material, including affected neighboring behavior. Use an inexpensive
failing-before/passing-after regression when practical; otherwise explain the
alternative evidence. Preserve earlier findings and receipts. Existing repair
and spending limits remain binding; exhaustion yields an actionable decision
request, never acceptance. A changed locked decision requires a controlled
amendment, not a worker's improvised interpretation.

After interruption, reconcile retained attempts and external operation IDs
before repeating uncertain effects. Persist the first incomplete action and
required recovery evidence; failure to save required evidence blocks further
unsafe progression. Keep raw logs in existing retention and report concise
artifact references. Unknown cost or delivery remains unknown.

## Human gates

Satisfy or waive a human gate only on that human's explicit instruction and
attribute it. Screenshots and performance reports are evidence, not automatic
approval gates. Report the existing gate ID, affected task/wave, and Tusker Mac
app destination; an already approved decision must not create a replacement
gate. Existing wave authority may continue eligible work, but approval never
starts an inert wave or resumes a paused one.
