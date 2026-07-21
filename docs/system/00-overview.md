---
title: "Tusker system overview"
status: canonical
read_when: "You are new to Tusker (human PM or fresh agent session) and need the mental model, the moving parts, and where to look next."
skip_when: "You already know the architecture and just need one command or one subsystem — jump straight to the sibling reference docs."
---

# Tusker system overview

Tusker is a **repo-local, agent-native work tracker**. It lives inside a single
repository (in a `.tusker/` vault) and treats every piece of work as a *task
contract*: a plain-language statement of what should change and how we will know
it is done. Agents (Claude/Codex sessions) or an operator claim those contracts,
do the work, attach **proof**, pass **gates**, and get **reviewed** before the
change lands. Nothing is tracked in a cloud service — the state is files in the
repo, so it travels with the code and is truthful to whoever reads it.

The core idea: writing code is cheap now; the expensive parts are a clear
definition-of-done, clean context for the worker, and an honest record of state.
Tusker is the harness that supplies all three. See the driving spec:
[software-factory.md](../../.tusker/specs/software-factory.md).

## Mental model in one breath

- A **task contract** has two layers: a PM-readable top (what/why/acceptance)
  and an implementer appendix (files, symbols, commands).
- Work is a **DAG of contracts on the outside** (dependency-ordered,
  gate-fenced) with **loop-ish workers on the inside** (one agent free to
  traverse its own path within one task).
- **Proof, not vibes.** A task closes only when its proof rows are green and, for
  risky work, an independent reviewer has signed off.
- **Whoever drives, it registers.** A background daemon worker and a hands-on
  interactive session both claim, update, and close through the same CLI, so the
  logbook, stream board, and serve UI always reflect reality.

## Architecture

```mermaid
flowchart TD
    spec["Operator / spec session\n(grill → spec + decisions)"] -->|emits| vault["Task contracts\nin the .tusker vault"]
    vault --> claim{"Who claims?"}
    claim -->|resident daemon| worker["Dispatched worker\n(codex_exec / claude-code)"]
    claim -->|interactive session| worker2["Hands-on agent\n(claim sets 'worked outside daemon')"]
    worker --> proof["Proof rows + evidence"]
    worker2 --> proof
    proof --> gates["Gates\nper-change / wave / nightly"]
    gates --> review["Reviewer lane\n(independent model, review→rework loop)"]
    review --> land["Merge window / batch gate\n(tusker land, gate-ledger)"]
    land --> views["Logbook • stream board • serve UI"]
    views -.reflects.-> vault
```

## Major subsystems

| Subsystem | One-line purpose | Reference |
|---|---|---|
| Tasks & proof | Two-layer contracts, lifecycle status, proof rows, evidence, acceptance | [tasks-and-proof.md](tasks-and-proof.md) |
| Orchestration | Daemon polling, dispatch, runs/leases, waves, interactive session attach | [orchestration.md](orchestration.md) |
| Gates | Per-change / wave-end / nightly gate tiers, gate-ledger, closeout checkpoints | [gates.md](gates.md) |
| Skills & knowledge | The `tusker` operator skill, per-repo project-knowledge skill, domain canon | [skills.md](skills.md) |
| Serve UI | Read-only localhost control room (runs, streams, tasks) | [serve-ui.md](serve-ui.md) |
| CLI | Every user-facing `tusker` command, grouped by purpose | [cli.md](cli.md) |

## Where things live

| Path | What it holds |
|---|---|
| `.tusker/specs/` | Canonical specs — the durable *why / what-changes* (e.g. `software-factory.md`, `build-and-test-economics.md`) |
| `.tusker/specs/decisions/` | Decision logs — a record of *what was actually said* in a grill/spec session |
| `.tusker/work/` | The live task/epic/gate records (the vault's tracked work) |
| `.tusker/dashboards/` | Generated dashboards and the stream board |
| `.tusker/logbook/` | Plain-language daily digests for a product reader |
| `docs/system/` | These canonical *how the system is today* docs |

Other `.tusker/` subtrees (`events`, `_generated`, `attempts`, `evidence`,
`Attachments`, raw logs) are machine state — do not read them unless a task
explicitly requires it.

## The documentation contract

Three kinds of writing, three jobs — keep them separate:

- **Specs** (`.tusker/specs/`) say *why* we are building something and *what
  will change*. Start at [software-factory.md](../../.tusker/specs/software-factory.md).
- **Decision logs** (`.tusker/specs/decisions/`) record *what was said* — the
  alternatives weighed and the calls made in a session. Point-in-time, not
  updated.
- **These system docs** (`docs/system/`) describe *how the system actually is
  today*, read from the code. They are living reference: **update them whenever a
  design changes**, in place, no versions.

If code and these docs disagree, the code wins and the doc is the bug — fix it.
