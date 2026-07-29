# Agent workflow

The goal is not to make contribution harder. The goal is to make sloppy contribution harder.

## Principles

- Problems should be legible before implementation starts.
- Review should start with evidence, not archaeology.
- AI is allowed, but slop is not.
- Contributors must understand their own changes.
- Raw transcripts are optional appendix material, not required reading.

## Expected flow

```text
idea / report
   ↓
task contract or delivery DAG
   ↓
read-only review → optional held import
   ↓
interactive work start
   or
explicit unattended Start → resident daemon
   ↓
implementation → acceptance-mapped proof
   ↓
independent review → deterministic landing/close
```

Review, dry-run, and held import are inert. They do not enable automation,
start a daemon, arm a wave, dispatch a model, call a provider, move a ref, or
spend. A user-opened session implements the task itself through `tusker work
start`; it needs task/dependency/gate/ownership/revision/workspace readiness,
not daemon authority. Unattended Start is a separate, fingerprint-confirmed
operation and fails closed until project, runner, workspace, integration,
daemon, and wave authorization facts pass.

When Tusker refuses, preserve the typed blocker and remedy. Do not collapse a
dependency, human gate, owner conflict, unsafe workspace, disabled project,
daemon outage, or optional adapter drift into a generic “not ready.”

## What we want from contributors

Before opening a PR, contributors should be able to explain:

- what changed
- why it changed
- how it was tested
- what the risk is
- what a reviewer should focus on

If AI was used, disclose it. If the contributor cannot explain the final behavior without leaning on the tool, the work is not ready.

For broad, high-risk, or agent-heavy changes, generate an explainer packet before review:

```bash
tusker packet <TASK-ID> --for explainer --write
```

The explainer packet is for human understanding. It does not replace evidence, tests, or reviewer approval.

## What maintainers should enforce

- keep new features or architecture discussion out of surprise PRs
- reject refactor-only churn unless requested
- ask for evidence on user-visible changes
- keep the bar proportional to risk
