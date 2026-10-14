# Operate

Diagnose tracker state read-only:

```bash
tusker version --json
tusker show <TASK-ID> --capsule
tusker show <TASK-ID> --proof
tusker show <TASK-ID> --closeout --json
tusker show <EXECUTION-ID>             # one execution strand
tusker list executions [--unbound]     # execution graph
```

Provider observations remain authority-neutral. Current execution behavior is
in `docs/system/execution-observability.md` and its listed source files.

The smallest command that names the task, lifecycle state, open gate, or missing proof wins.

An interactive architect registered as a Claude Code or Codex contact receives
worker questions and wave reports as injected context from its `UserPromptSubmit`
or `Stop` inbox hook. Read each message ID and answer a question with:

```bash
tusker message reply --project <project-id> --reply-to <message-id> --sender execution:<your-execution-id> --key <unique-key> --body-file -
```

The sender must be the execution ID returned by contact registration. The
inbox is checked at the next prompt or turn end, not pushed live. Treat wave
reports as information; `ApplyArchitectContinuation` stays operator-invoked.

## Diagnosis and bounded self-recovery

When authorized work stalls, diagnose before acting:

```bash
tusker show <TASK-ID|WAVE-ID> --why [--json]
tusker show <WAVE-ID> --check --json
tusker projects set <project-id> --dry-run
```

`tusker show <ID> --why` is read-only and leads with the first actionable cause, the
next actor, and the exact permitted next action. Exit 0 covers healthy,
complete, **and normal waits**: a normal wait is not dispatchability, and
structural validity (`tusker show <WAVE-ID> --check`, `tusker check --only validate`) is separate
from current Start readiness. `--json` retains every finding with its stable
code; UI wave/task surfaces and Settings render the same codes. A repeated
poll that changes nothing is evidence, not a fix.

| Symptom | Command | Permitted action | Stop condition |
|---|---|---|---|
| Queued with no progress, cause unknown | `tusker show <ID> --why` | Follow only the reported next action | Cause resolved or reported as defect/decision |
| Dependency/capacity wait | `tusker show <ID> --why` | Wait for the named owner, or raise capacity in Settings | Owner advances |
| Background work off, or stale authorizations | `tusker projects set <project-id> --dry-run` | `tusker projects set <project-id> --on` resumes the shown scope and arms nothing new | Scope resumed; excluded waves need their own `tusker start <WAVE-ID>` |
| Missed wake or restart | `tusker daemon run --once` | One poll tick; Play/enable/acceptance/capacity changes wake automatically, dropped notifications recover on the periodic poll | Work advances or doctor names a cause |
| Proof/review failure | `tusker show <ID> --proof` | Fix the check with `tusker edit`, then rerun `tusker done` | Green proof recorded |
| Uncertain outcome | `tusker show <ID> --why --json` | Use only the typed recovery action the finding carries | Postcondition verified, or bounded retries end in a human decision |
| Human gate | `tusker show <TASK-ID> --closeout --json` | Stop and report the gate and required action | Human decides |

Bounded automatic repair applies only to recoverable metadata
inconsistencies under still-valid authority: one repair per unchanged
diagnostic fingerprint, then the postcondition is verified against a fresh
read. A failed or recurring fault escalates once with evidence and stays
visible. Repairs never arm inert work, enable a project, widen scope, change
models/budgets, forge proof, approve a human gate, or retry an uncertain
external effect. Never hand-edit the runtime database; for a suspected
product defect, attach `tusker show <ID> --why --json --output <new-path>` (it
refuses to overwrite) and report the file.

## Documentation defects

Product knowledge lives under `docs/system/`; `.tusker/` holds tracker state
and thin pointers only. For a stale or misplaced document, route by subject
first (`tusker list docs --grep`), fix the owning document, and check the repair:

```bash
tusker check --only docs --json
tusker check --fix
```

## Recovery boundary

Correct clearly-requested state through ordinary lifecycle commands. Setup repair, daemon, dispatch, service, and fleet operations are their own explicitly-requested tasks, never a side effect of task management. Tracker failure stays separate from implementation results, and Tusker repair becomes a task only when the user asks.
