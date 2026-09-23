# Session recovery acceptance

Use a dedicated seeded repository. Keep its resident daemon running in a separate shell and open its registered project in Serve. Run one harness and one scenario at a time. A `fixture` proof checks the fake executable protocol; only an observed provider run may be called `live-provider`. `manual-required`, `unsupported`, and `unavailable` are not passes.

```sh
tusker demo seed --repo "$REPO" --scenario parallel-waves
sh scripts/test-real-work-project.sh --repo "$REPO" --mode real --require-harness codex_exec --variant session:restart --proof-dir "$PROOF_DIR"
tusker demo session --repo "$REPO" --harness codex_exec --scenario restart --json
tusker demo reset --repo "$REPO" --yes
```

Replace `codex_exec` with `claude-code`, `muse`, or `devin`; use a matching `--profile` when routing needs one. Run one `session:<scenario>` variant at a time. The script succeeds only for `passed` live provider proof with observed checks. `session:all` exits 3 without running scenarios because each needs a separate reset and standalone attempt. Run `tusker demo reset --repo "$REPO" --yes` between scenarios, then seed again; reset refuses an active attempt, so settle it first using Serve. The session command starts the standalone wave for supported automated scenarios. Its progress helper runs for about 60 seconds and prints a line roughly every five seconds. Watch the run page for state, native session ID, attempt IDs, transcript, and action availability. Record screenshots or exported run evidence outside the demo repo before reset.

The session command records its observed checks in the proof JSON. It drives Say, Stop/Continue, Start fresh, demo-owned worker kill, ask/reply, and a demo-scoped permission denial where the selected driver and profile support them. The `restart` driver reports `unsupported`: the daemon is shared and the demo cannot restart it without affecting other projects. Use the checklist to confirm the run page and capture provider evidence. An unavailable daemon is `refused`; a missing or unauthenticated harness is `unavailable`. These states make the script exit without a pass.

| Harness | Say soft | Remaining scenarios |
| --- | --- | --- |
| Claude Code | supported by hook/stdin path | execute checklist |
| Codex exec | unsupported; deliver on Continue | execute checklist |
| Muse | unsupported; deliver on Continue | execute checklist |
| Devin | unsupported; deliver on Continue | execute checklist |

### restart

While `s1` is Working and printing progress, restart the resident daemon from its independent shell. Reopen Serve. Pass when the same attempt and native session remain, progress resumes, and no new attempt appears.

### kill-worker

Kill only the detached worker process for `s1`. Wait for **Lost**, click **Continue**, and verify a second attempt resumes the same native session ID. Do not kill the daemon.

### say-hard

While Working, click **Say** and choose the interrupt/resume path. Send a unique sentence. Pass when the current turn settles, Continue resumes the same native session, and the sentence appears in the new transcript exactly once.

### say-soft

Claude Code only: while Working, send a unique sentence by the soft Say path. Pass when the running worker receives it between tool calls without an interrupt or new attempt. Other harnesses must show **unsupported** with the driver reason and the Continue fallback.

### ask-wait

Use a task prompt that calls Tusker's MCP ask tool with wait. When Serve shows **Waiting on you**, answer through the UI or `tusker message reply`. Pass when the answer reaches the same worker once and it continues. Capture the question and answer IDs.

### ask-nowait

Use a task prompt that asks without wait. Answer while the worker turn continues. Pass when the answer is delivered on the next Continue exactly once and the question does not pin the run in Waiting on you after it is answered.

### stop-continue

Click **Stop** while Working. Wait for **Stopped**, then click **Continue**. Pass when the old process is settled and the resumed attempt uses the same native session ID.

### start-fresh

Stop and wait for settlement, then click **Start fresh**. Pass when a new native session ID appears, the prior session remains in history, and a new attempt works on the same task.

### permission-deny

Choose a profile that denies a tool required by the task. Pass when Serve shows **Blocked** with a typed permission or sandbox reason. Fix the profile, click **Continue**, and verify the task resumes without losing the prior transcript. Record the exact profile and reason.
