# Handoff evidence

Status: focused packet regression passed.

`v7Packet` keeps the complete authored task body and ownership metadata for
agent, reviewer, work-session, daemon attempt, and closeout consumers. The
integrator was a separate consumer that still truncated acceptance at 18 lines;
it now emits the full task contract and the same ownership metadata.

Executed on local macOS arm64 at `03201019`:

```sh
scripts/with-validation-lock.sh -- go test ./cmd/tusker -run '^(TestTrustHandoffPreservesIntegratorContract|TestV7PacketPreservesCompleteTaskContract|TestVerifyAddCannotForgeCommandExecutionResult)$' -count=1 -v
```

Result: PASS (2.124s). This does not prove a live provider handoff or resume;
those remain external acceptance work.
