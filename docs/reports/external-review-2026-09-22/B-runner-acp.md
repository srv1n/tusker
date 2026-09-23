# RESULT

```json
{
  "kind": "review",
  "verdict": "request_changes",
  "risk": "high",
  "summary": "The supplied snapshot contains reproducible cancellation and cleanup hangs, incorrect contained-process cleanup, two ACP response-ordering races, and protocol/outcome handling defects affecting informational extensions, Muse, and long permission-heavy turns. These can prevent shutdown, leave descendants running after an attempt, discard known terminal results, or reject otherwise valid work. The fixes should preserve the absence of wall-clock task timeouts and instead bound transport teardown and recovery.",
  "findings": [
    {
      "severity": "high",
      "path": "internal/acp/client.go",
      "line": 1957,
      "problem": "A permission response holds state.mu across a blocking stdin write. Cancel, Close, and poison acquire that same lock before closing the transport. If the adapter stops consuming stdin, cancellation blocks before its deadline-handling selects and cannot reach forced teardown.",
      "fix": "Linearize the permission decision under the state lock, release cancellation-visible locks before blocking I/O, and ensure the cancellation deadline can close the transport without acquiring locks held by that write.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "internal/acp/client.go",
      "line": 1337,
      "problem": "Close waits unconditionally for cmd.Wait, but the ACP command has no WaitDelay and uses a copied stderr stream. An adapter descendant retaining stderr prevents Wait from completing after the adapter is killed. Startup failure and synchronous Interrupt paths consequently cannot reach wrapper cleanup.",
      "fix": "Set a bounded post-exit pipe-drain WaitDelay before starting the ACP command, or explicitly own and close the stderr drain within a bounded teardown. Preserve unlimited prompt duration.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "cmd/tusker/runner_raw_log_limit.go",
      "line": 225,
      "problem": "Contained non-ACP cleanup either kills the wrapper before terminal publication or leaves descendants alive. ErrWaitDelay triggers SIGKILL on the wrapper's own group before status is written; overflow kills only the direct child, and the wrapper's post-status group cleanup excludes non-ACP runners.",
      "fix": "Do not kill a wrapper-owned group from the monitor before terminal publication. Extend wrapper-owned post-status containment cleanup to non-ACP runners, retaining separate cleanup behavior for uncontained commands.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "internal/acp/client.go",
      "line": 1150,
      "problem": "A valid session/new response immediately followed by session/update can poison startup. handleResponse removes the pending session/new entry before NewSession commits c.session, so the following update observes neither a pending creation nor an active session.",
      "fix": "Keep an explicit provisional session-creation binding until the validated session is committed, buffering bounded updates if necessary, or commit the validated binding before the reader processes subsequent frames.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "internal/acp/client.go",
      "line": 1478,
      "problem": "When a matched response is already queued and readerDone also becomes ready, awaitCall can select readerDone and return delivery_unknown even with DeliveryResponseSeen. A known terminal result or matched error is discarded solely because EOF wins select scheduling.",
      "fix": "Reconcile an already-settled matched response before classifying transport closure or deadline expiry. Return the observed result or error; use delivery_unknown only when no matched response was received.",
      "creates_followup_task": false
    },
    {
      "severity": "medium",
      "path": "cmd/tusker/runner_raw_log_limit.go",
      "line": 237,
      "problem": "Production bounded Muse execution bypasses classifyMuseCLIOutput. A process exiting zero with run.terminal.failed, or without any terminal record, publishes exit zero with no failure outcome and can subsequently be classified as succeeded.",
      "fix": "Apply Muse terminal decoding in the bounded monitor, preserving overflow and cancellation precedence. Map failed or cancelled native terminals to appropriate exit status, or preserve those typed outcomes before generic exit-code classification.",
      "creates_followup_task": false
    },
    {
      "severity": "medium",
      "path": "internal/acp/client.go",
      "line": 1673,
      "problem": "Only four named Devin extension notifications are ignored. Any other valid underscore-prefixed informational notification poisons the client, aborting startup or converting an active prompt to delivery_unknown.",
      "fix": "After validating the JSON-RPC envelope and frame bounds, ignore unrecognized underscore-prefixed notifications without executing them. Continue returning method-not-found for unsupported requests.",
      "creates_followup_task": false
    },
    {
      "severity": "medium",
      "path": "internal/acp/client.go",
      "line": 1701,
      "problem": "permissionInvocations is a lifetime counter compared against MaxPendingRequests and is never decremented. The 65th sequential permission request therefore poisons a default-configured client even when only one request is pending, imposing an unintended work limit on long turns.",
      "fix": "Bound currently live permission handlers and pending responses rather than lifetime invocations. Release handler capacity when the actual handler exits, including correct accounting for handlers that outlive cancellation.",
      "creates_followup_task": false
    }
  ]
}
```

# Findings

References are relative to the attached ZIP. The findings concern the supplied source snapshot; I did not establish introduction dates against the parent commit. No supplied source files were changed.

### 1. High — Permission-response writes can make cancellation itself unbounded

**Location:** `internal/acp/client.go:1955–1983`.

`respondPermission` holds both the writer mutex and the per-permission state mutex while calling `writeAll`. Meanwhile, `Cancel` synchronously invokes `cancelPendingPermissions` at `internal/acp/client.go:1273`; that function acquires the same state mutex while holding the client mutex at `internal/acp/client.go:1915–1927`.

If the permission response blocks writing to the adapter, cancellation cannot reach its deadline-select branches. `Close` and `poisonLocked` also acquire the permission-state lock before transport teardown, so they cannot break the wait.

This reaches the supervisor: `cmd/tusker/runner_wrapper.go:385–389` calls `Interrupt` synchronously, **before** the bounded status wait at `cmd/tusker/runner_wrapper.go:276–277`.

**Reproduction:** With a blocked permission-response writer and a 10 ms cancellation-drain setting, cancellation remained blocked beyond 150 ms and proceeded only after the writer was explicitly released.

**Minimal fix:** Preserve the decision-versus-cancellation ordering, but do not hold cancellation-visible state locks during blocking writes. The deadline path must be able to close stdin and terminate the adapter independently of those locks.

### 2. High — Inherited stderr can hang ACP teardown after the adapter is dead

**Location:** `internal/acp/client.go:1325–1337`; command setup at `internal/acp/client.go:465–480`.

The command’s stderr is connected through an `io.Writer`, and no `WaitDelay` is configured. `waitLoop` calls `cmd.Wait` at `internal/acp/client.go:520`; `Close` then waits unconditionally for that loop. Killing the direct adapter and closing stdin/stdout does not close a descendant’s inherited stderr descriptor.

Go documents that, without `WaitDelay`, these pipe-copy waits can remain blocked until orphaned subprocesses close their descriptors. A nonzero `WaitDelay` bounds post-exit draining rather than overall task duration. ([Go Packages][1])

This is not rescued by the normal status-first ACP cleanup. Startup errors call `handle.close()` before returning at `cmd/tusker/runner_acp.go:383–386`, and `Interrupt` calls it synchronously at `cmd/tusker/runner_acp.go:197–204`. Both can prevent the wrapper from reaching its containment cleanup.

**Reproduction:** An adapter spawning a descendant that retained stderr left `Close` blocked after the adapter was killed; killing the descendant released it.

**Minimal fix:** Configure bounded post-exit pipe draining before `cmd.Start`, or explicitly close an owned stderr drain during bounded teardown. Do not add a prompt-duration deadline.

### 3. High — Non-ACP containment cleanup runs before status, or not at all

**Location:** `cmd/tusker/runner_raw_log_limit.go:207–227`; `cmd/tusker/runner_wrapper.go:309–316`.

Contained runners inherit the wrapper’s process group, as established by `cmd/tusker/runner_exec.go:242–258`. Two paths mishandle that ownership:

* When a successfully exited child leaves an output pipe open, `monitorBoundedRunnerCommand` handles `ErrWaitDelay` by killing the entire group at lines 225–226. That group includes the wrapper executing the monitor, so it dies **before** terminal publication at line 255.
* On raw-log overflow, `killContainedRunnerCommand` kills only the direct child. The post-status wrapper cleanup is restricted to ACP runners, leaving non-ACP descendants alive.

The distinction between these paths matters: `ErrWaitDelay` is not the generic return value for every forced pipe closure; a non-successful process exit can retain its normal exit error. ([Go Packages][1])

**Reproductions:** A contained root exiting zero with a pipe-holding descendant killed its wrapper without producing status. A contained overflow run produced failure status, exited its wrapper, and left its descendant running.

**Minimal fix:** For wrapper-contained commands, publish durable terminal status first and let the still-owning wrapper kill its own group. Extend that existing post-status mechanism to non-ACP runners; retain separate uncontained-command cleanup.

### 4. High — Session creation has a response-to-commit race

**Location:** `internal/acp/client.go:1150–1156`.

`handleResponse` removes the pending request at `internal/acp/client.go:1620–1628`. The caller does not commit the new session until `internal/acp/client.go:734–737`. A following `session/update` can therefore reach validation during an interval where neither the pending `session/new` nor `c.session` exists.

An agent advertising commands immediately after session creation is valid ACP behavior, not an invalid out-of-session message. ([Agent Client Protocol][2])

**Reproduction:** A helper wrote the valid `session/new` response and an `available_commands_update` notification in one stdout write. Startup failed in all 20 exercised runs.

**Minimal fix:** Make the creation-to-active-session handoff explicit. Retain a provisional binding until validation and commit complete, or commit the validated session before processing the next frame. Any buffered updates must remain bounded and be checked against the returned session ID.

### 5. High — EOF can overwrite an already-known terminal result

**Location:** `internal/acp/client.go:1478–1481`.

`handleResponse` marks `DeliveryResponseSeen` and queues the matched result or error at `internal/acp/client.go:1634–1655`. Nevertheless, `awaitCall` independently selects between that queue and `readerDone`. The EOF branch treats **every phase at or after write-started** as uncertain, including response-seen.

Thus an adapter sending its terminal response and exiting can have known completion replaced by `delivery_unknown`. `cmd/tusker/runner_acp.go:530–531` then projects that into an unknown attempt requiring inspection rather than preserving the terminal result. Matched errors are vulnerable to the same race.

**Reproduction:** With both the received-result channel and EOF ready, 498 of 1,000 forced selections discarded the known terminal response. This is a scheduling reproduction, not an estimated production failure rate.

**Minimal fix:** Arbitrate settlement before classifying EOF or deadline expiry. An already-observed matched response must determine the call result; subsequent transport failure can retire the client without rewriting that result.

### 6. Medium — Bounded Muse execution ignores the native terminal record

**Location:** `cmd/tusker/runner_raw_log_limit.go:237–255`.

Daemon preparation assigns the authoritative raw-log bound to Muse at `cmd/tusker/daemon.go:4289–4302`, selecting the bounded monitor through `cmd/tusker/runner_exec.go:275–277`.

However, Muse terminal decoding exists only in the other monitor at `cmd/tusker/runner_exec.go:354–364`. The bounded monitor treats zero process exit as no failure regardless of whether output contains `run.terminal.failed`, cancellation, or no terminal record. In a review lane, that zero-exit status reaches the success return at `cmd/tusker/runner_exit_classification.go:48`.

**Reproduction:** Both an explicit failed terminal record and output lacking a terminal record produced exit code zero with no failure outcome in the bounded monitor.

**Minimal fix:** Apply `classifyMuseCLIOutput` to bounded Muse output too. Also preserve its failure/cancellation through the common classifier: merely assigning a typed failure while leaving exit code zero is insufficient, because that classifier does not generally honor those typed outcomes before its success inference.

### 7. Medium — Informational extension handling is tied to four Devin method names

**Location:** `internal/acp/client.go:1672–1678`.

The four `_cognition.ai/...` exceptions prevent failures for those exact notifications, but any other extension notification still poisons the transport. A harmless additional Devin notification—or another adapter’s informational extension—can therefore terminate work.

ACP reserves underscore-prefixed methods for extensions and says unrecognized notifications should be ignored. Ignoring an informational notification does not grant permission or execute an extension. ([Agent Client Protocol][3])

**Reproduction:** Sending the specification’s `_zed.dev/file_opened` notification to an initialized client immediately set its protocol error.

**Minimal fix:** Ignore unknown underscore-prefixed notifications after envelope and frame validation, with bounded diagnostic observation if needed. Keep unsupported **requests** on the existing method-not-found response path and retain validation of known core messages.

### 8. Medium — The pending-permission limit is actually a lifetime work limit

**Location:** `internal/acp/client.go:1700–1707`.

`permissionInvocations` increases for each permission request and is never decreased. It is compared against the same limit used by the concurrent permission semaphore. Consequently, completing earlier requests does not restore capacity.

**Reproduction:** A single prompt issuing 65 sequential permission requests, with a default-sized limit of 64 and an immediately returning handler, failed with:

`delivery_unknown (write_complete): ... total permission request limit exceeded`

This can stop a healthy long-running turn despite the deliberate removal of task-duration limits.

**Minimal fix:** Account for live handlers and pending responses, not total historical requests. Preserve the bound on handlers that ignore cancellation: release their handler capacity when the actual handler goroutine exits, not merely when the response-waiting goroutine times out.

# Architecture notes

The explicit disabling of prompt and stall deadlines at `cmd/tusker/runner_acp.go:326–332` is consistent with the requested behavior. **None of these findings requires restoring a wall-clock task timeout.** Cancellation and post-exit recovery need independent bounds that cover the entire synchronous path, including permission writes, `Interrupt`, and `Close`.

The existing wrapper-owned containment model is sufficient; it does not need redesign. Its critical ordering must be consistent: preserve any observed protocol terminal result, publish durable attempt status, and then terminate the still-owned containment group. The current implementation violates that ordering on some paths and omits containment cleanup on others.

# Missing tests

| Area                                  | Focused coverage needed                                                                                                                                                                                                                                                                                              |
| ------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Permission cancellation               | In `internal/acp/client_test.go`, block the **permission-response write itself**, then exercise `Cancel`, `Close`, and poisoning. The existing blocked-prompt-write test at line 1415 does not cover the state-lock cycle.                                                                                           |
| ACP teardown                          | Spawn an adapter descendant retaining stderr. Exercise initialization failure and explicit interruption; assert teardown returns and wrapper containment cleanup remains reachable.                                                                                                                                  |
| Session handoff                       | Emit `session/new` response followed immediately by `available_commands_update` in the same write. Verify acceptance and correct session binding; also cover the analogous restore-response handoff.                                                                                                                 |
| Terminal settlement                   | Deterministically make a matched terminal response or error ready together with EOF and deadline expiry. Assert the observed response wins. Keep a separate no-response EOF case that remains `delivery_unknown`.                                                                                                    |
| Contained non-ACP cleanup             | Extend `cmd/tusker/runner_raw_log_limit_test.go` and wrapper tests with a real wrapper-owned process group. Cover successful root exit with inherited pipes, overflow, and cancellation; require durable status and no surviving descendants. The descendant test at line 291 does not exercise wrapper containment. |
| Muse outcome propagation              | Exercise the bounded monitor through final exit classification for completed, failed, cancelled, and missing terminal records, including a zero process exit and a review lane.                                                                                                                                      |
| Protocol compatibility and long turns | Accept all four existing Devin notifications plus an unknown valid extension notification. Run more than 64 sequential permissions within one prompt while retaining a bound on non-returning handlers.                                                                                                              |
| No task-duration timeout              | Add behavioral coverage showing a quiet, unfinished prompt remains active with prompt/stall deadlines disabled, while explicit cancellation remains bounded. The test at `internal/acp/client_test.go:397` only verifies configuration defaults.                                                                     |

**Validation boundary:** The repository requires Go 1.26.5 at `go.mod:3`; that toolchain was unavailable and its download failed. Validation used copied, unmodified ACP sources and separate targeted runner-function harnesses under the installed Go 1.23.2. These reproductions are not a full-repository test pass or live-provider qualification.

# If you change one thing

**Remove blocking permission-response I/O from the cancellation-critical lock at `internal/acp/client.go:1955–1983`.** Make the existing cancellation deadline capable of closing the transport even when the adapter stops reading. Until that is true, the wrapper’s bounded recovery can be unreachable precisely when it is needed most.

[1]: https://pkg.go.dev/os/exec "https://pkg.go.dev/os/exec"
[2]: https://agentclientprotocol.com/protocol/slash-commands "https://agentclientprotocol.com/protocol/slash-commands"
[3]: https://agentclientprotocol.com/protocol/extensibility "https://agentclientprotocol.com/protocol/extensibility"
