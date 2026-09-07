# Run and configuration

A run directive is deliberate human authority; preparing tasks or waves does
not start them. Interactive sessions implement work themselves. They never
start a daemon, dispatch automation, or launch nested workers. A dispatched
worker (`TUSKER_ATTEMPT_ID`) works only its claimed task and follows its packet.

## Resolve the execution configuration

```sh
tusker config resolve automation.model_levels --json
tusker config resolve automation.profiles --json
tusker daemon status --json
```

Tasks specify Light, Standard or Demanding. Ordered configured profiles are the only permitted fallbacks. Resolve profiles when each
execution/review starts: global defaults, project configuration and explicit
overrides determine the result. Keep task intent separate from actual harness,
model, reasoning effort and ACP/CLI transport recorded on the run. Never
hard-code model names or silently replace a failed profile/transport.

For configuration changes, discover the current schema and runner commands
through capabilities/help. Manual profiles are sufficient; model discovery is
optional where supported. Execution and review can use different profiles;
approval is a separate policy, human-owned only when explicitly gated.

## Inspect and control

```sh
tusker runs inspect <TASK-ID>
tusker runs logs <TASK-ID> --lines 50
tusker wave show <WAVE-ID>
tusker wave brief <WAVE-ID> --json
```

Use targeted help for installed manual task/wave controls. Report missing CLI
parity instead of substituting an autonomous dispatch command. Operator service
startup belongs in the operator shell. `tusker automation plan` is read-only.
Observe a bounded run only when requested; do not become a polling coordinator.

Runtime activity is separate from durable task status. A held lease and fresh
heartbeat establish liveness; exit success alone does not establish reviewed
completion. Reconnect/read current state before interpreting a stale display.
Use closeout status to identify missing proof, review or gates.

## Human gates

Satisfy or waive a human gate only on that human's explicit instruction and
attribute it to them. Optional screenshots/performance reports are evidence,
not automatic approval gates. Report the gate ID and exact required action.
