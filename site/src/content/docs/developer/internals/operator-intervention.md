---
title: "Operator intervention"
description: "The dispatcher and tusker CLI cover the steady-state loop. When things wedge — a retry budget exhausts, a human needs to cancel a run mid-flight, or somebody hand-edits a note — a human operator steps in. This doc is the audit-trail contract for those interventions so the vault history stays truthful."
tusker:
  audience: "developer"
  publish_path: "developer/internals/operator-intervention"
  publish_section_title: "Internals"
  route: "/developer/internals/operator-intervention/"
  source_kind: "repo_doc"
  source_path: "skill/docs/OPERATOR_INTERVENTION.md"
  summary: "The dispatcher and tusker CLI cover the steady-state loop. When things wedge — a retry budget exhausts, a human needs to cancel a run mid-flight, or somebody hand-edits a note — a human operator steps in. This doc is the audit-trail contract for those interventions so the vault history stays truthful."
  tags:
    - "internals"
  updated: "2026-04-28"
---

# Operator intervention

The dispatcher and `tusker` CLI cover the steady-state loop. When things wedge — a retry budget exhausts, a human needs to cancel a run mid-flight, or somebody hand-edits a note — a human operator steps in. This doc is the audit-trail contract for those interventions so the vault history stays truthful.

For ORC-era run viewing, workpad, review packet, resume, and Codex-vs-Claude guidance, see `ORCHESTRATION_RUNBOOK.md`. This file is the manual override guide.

## When a human overrides dispatch_state

Every state mutation that bypasses the dispatcher still belongs in the `transitions[]` array on the note's frontmatter. The CLI appends transitions for you on `set-status`, `pickup`, and `release`; if you edit YAML by hand, append the entry yourself.

Transition shape:

```yaml
transitions:
  - at: "2026-04-21T14:22:00Z"
    kind: "release"          # status | claim | release
    from: "running"
    to: "cancelled"
    actor: "sarav"           # human identity, not "dispatcher"
    reason: "superseded by MEM-S-0004"
```

The `actor` field is what separates operator action from dispatcher action in audits — `dispatcher`, `claude-code`, `codex`, `gemini` are machine actors; anything else is a human.

## Common intervention recipes

### Retry budget exhausted, work is still valid

```bash
tusker release --vault <v> --id MEM-S-0003 --to failed  # already in this state
# Edit frontmatter: reset run_attempts: 0, clear failure_class
# Then:
tusker set-status --vault <v> --id MEM-S-0003 --status active --actor sarav --reason "retry budget reset after infra fix"
```

The `set-status --reason` is required here — the transitions log must explain why the budget was reset.

### Cancel a running agent

```bash
# Kill the process yourself (dispatcher's PID table is at _system/logs/runs.json)
tusker release --vault <v> --id MEM-S-0003 --to cancelled --by sarav --reason "scope changed, story obsolete"
```

Cancellation is terminal. Do not flip `dispatch_state` back to `unclaimed` to "reuse" the note — create a new story that supersedes it and link via `related:`.

When ORC run commands are available, prefer:

```bash
tusker runs inspect <run-id>
tusker runs interrupt <run-id>
```

Inspect first unless the run is causing damage. Interrupts should leave the note, workpad, and runtime state consistent enough for a human to resume intentionally.

### Forcibly unclaim a zombie

If the dispatcher crashed and left a note in `claimed` with no live process:

```bash
# Verify no PID in _system/logs/runs.json is actually running
tusker release --vault <v> --id MEM-S-0003 --to stalled --failure-class stuck --reason "dispatcher crashed 2026-04-21, no live PID"
# Then decide: retry (set status active, run_attempts stays so the limit still applies) or cancel.
```

### Hand-edit frontmatter

Allowed, but:

1. Run `tusker validate --vault <v>` before committing. The validator will catch `dispatch_state` coherence violations (e.g., state set without `claimed_by`).
2. If you change `status`, `dispatch_state`, `claimed_by`, or `failure_class`, append a transition with `actor: <your-handle>` and a `reason`.
3. Never delete prior transitions. The array is append-only; the history is the point.

## What the dispatcher will NOT do

- Retry a `failed` note that is not `failure_class: transient`.
- Re-pickup a note with `run_attempts >= config.retry.max_attempts`.
- Touch `cancelled` notes.
- Modify `transitions[]` entries it did not write.

If any of those need to happen, it's operator work.

## Audit expectations

When something goes visibly wrong (a retry storm, a wedged agent, a silent data loss incident), the first place to look is the note's `transitions[]` and its `Attachments/<ID>/session-*.log` files. The transitions array should answer: _who changed this, when, from what, to what, and why._ If any of those five are missing, the intervention was sloppy — fix the log entry before closing out.
