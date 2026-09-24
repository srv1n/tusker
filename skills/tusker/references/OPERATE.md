# Operate

Diagnose tracker state read-only:

```bash
tusker capabilities --json
tusker show <TASK-ID> --capsule
tusker proof status <TASK-ID>
tusker closeout status <TASK-ID> --json
tusker execution show <EXECUTION-ID>   # one execution strand
tusker execution inbox                 # unbound execution strands
tusker execution list                  # execution graph
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
tusker doctor <TASK-ID|WAVE-ID> [--json]
tusker wave review <WAVE-ID> --check --json
tusker projects automation-scope --id <project-id>
```

`tusker doctor` is read-only and leads with the first actionable cause, the
next actor, and the exact permitted next action. Exit 0 covers healthy,
complete, **and normal waits**: a normal wait is not dispatchability, and
structural validity (`wave review --check`, `tusker validate`) is separate
from current Start readiness. `--json` retains every finding with its stable
code; UI wave/task surfaces and Settings render the same codes. A repeated
poll that changes nothing is evidence, not a fix.

| Symptom | Command | Permitted action | Stop condition |
|---|---|---|---|
| Queued with no progress, cause unknown | `tusker doctor <ID>` | Follow only the reported next action | Cause resolved or reported as defect/decision |
| Dependency/capacity wait | `tusker doctor <ID>` | Wait for the named owner, or raise capacity in Settings | Owner advances |
| Background work off, or stale authorizations | `tusker projects enable --dry-run`, `tusker projects automation-scope` | `tusker projects enable` resumes the shown scope and arms nothing new | Scope resumed; excluded waves need their own Start/resume |
| Missed wake or restart | `tusker refresh` | One poll tick; Play/enable/acceptance/capacity changes wake automatically, dropped notifications recover on the periodic poll | Work advances or doctor names a cause |
| Proof/review failure | task packet + `tusker verify` | Rerun the check, reconcile the record | Green proof recorded |
| Uncertain outcome | `tusker doctor <ID> --json` | Use only the typed recovery action the finding carries | Postcondition verified, or bounded retries end in a human decision |
| Human gate | `tusker closeout status <TASK-ID> --json` | Stop and report the gate and required action | Human decides |

Bounded automatic repair applies only to recoverable metadata
inconsistencies under still-valid authority: one repair per unchanged
diagnostic fingerprint, then the postcondition is verified against a fresh
read. A failed or recurring fault escalates once with evidence and stays
visible. Repairs never arm inert work, enable a project, widen scope, change
models/budgets, forge proof, approve a human gate, or retry an uncertain
external effect. Never hand-edit the runtime database; for a suspected
product defect, attach `tusker doctor <ID> --json --output <new-path>` (it
refuses to overwrite) and report the file.

## Documentation defects

Product knowledge lives under `docs/system/`; `.tusker/` holds tracker state
and thin pointers only. For a stale or misplaced document, route by subject
first (`tusker docs find`), fix the owning document, and check the repair:

```bash
tusker docs check [<subject-or-path>] --json
tusker docs map --vault ./.tusker
```

## Recovery boundary

Correct clearly-requested state through ordinary lifecycle commands. Setup repair, daemon, dispatch, service, and fleet operations are their own explicitly-requested tasks, never a side effect of task management. Tracker failure stays separate from implementation results, and Tusker repair becomes a task only when the user asks.
