# Run and verification

A run directive is deliberate human authority. Interactive sessions implement
work themselves; they never start a daemon, dispatch automation, or launch
nested workers. A dispatched worker (`TUSKER_ATTEMPT_ID`) follows its packet.

Never hard-code a model or silently replace a failed transport. Inspect with:

```sh
tusker work readiness <TASK-ID> --lane execute --json
tusker runs inspect <TASK-ID> --json
tusker runs logs <TASK-ID> --lines 50
tusker closeout status <TASK-ID> --json
```

Runtime activity and task status differ. A heartbeat and lease establish
liveness; exit success does not establish review. Re-read stale displays.
Operator service startup stays in the operator shell.

Before editing, read the complete packet and its exact governing sections.
Check its surrounding-work dependencies, owned paths, and architect/origin/peer
contacts. For missing context or contact-routing questions, read `HANDOFF.md`. Resolve routine implementation choices
locally; return missing or conflicting product decisions to the recorded
architect through an authorized supported route. If that route is unavailable,
report the missing capability to the recorded fallback; never assume a message
was delivered. Pause only dependent work and preserve any accepted clarification
in the canonical contract through supported task operations.

## Verification recipe

Keep the packet's complete acceptance and conditions. Pick the smallest check;
`tusker verify recipe <TASK-ID> --files
<CHANGED-PATHS>` can suggest one. Before running it, record prerequisites,
readiness, user action, expected state, mutations, evidence location, and
cleanup. Preserve the command, result, first failure, and artifact path. The
executor records PASS or FAIL with `tusker verify add`; a
skip, zero-match test, source read, or old log is not a pass.

Label limits honestly: **synthetic** proves only the fake/demo; **source** proves
what was inspected, not execution; **local executed** proves that checkout and
environment; **installed** proves the identified installed build; **human**
proves only the named observation or acceptance. None implies remote/provider
behavior without observing it.

Prefer an existing test or demo. Disposable demos use an empty
directory, retain `tusker demo check --json`, then preview and apply only their
owned cleanup:

```sh
tusker demo reset --repo <DEMO-PATH> --dry-run --json
tusker demo reset --repo <DEMO-PATH> --yes --json
```

Never delete shared evidence to make a check repeatable. Live wake/resume or
message delivery stays unverified unless supported by inspected help/source and
actually observed.

## Human gates

Satisfy or waive a human gate only on that human's explicit instruction and
attribute it. Screenshots and performance reports are evidence, not automatic
approval gates. Report the gate ID and required action.
