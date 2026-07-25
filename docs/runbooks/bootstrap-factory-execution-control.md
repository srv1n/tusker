---
capsule:
  what: "Safe bootstrap and copy-paste handoff for building W-0003 without accidentally enabling broad repository automation."
  use_when:
    - "Preparing, bootstrapping, arming, monitoring, pausing, or rolling back the opt-in factory execution-control wave."
  skip_when:
    - "Implementing a task already claimed by the resident daemon."
---

# Bootstrap the Factory Execution-Control Wave

Wave: `W-0003`
Governing spec: `docs/specs/14-opt-in-factory-execution-control.md`
Delivery plan: `docs/plans/14-opt-in-factory-execution-control-v1.yaml`

## Safety rule

Do not enable the Tusker repository and “see what the daemon does.”

The first task in this wave creates the missing dispatch-scope guard. Until
that code is installed, enabling this repository still carries the legacy
broad-project semantics and may pick up unrelated ready/rework or review work.

Keep:

- the Tusker project disabled;
- `.tusker/WORKFLOW.md` automation disabled;
- `W-0001`, `W-0002`, and `W-0003` disarmed;
- release, scheduled promotion, and paid triage separately disabled.

Planning/import is inert and safe in that state.

## Phase 1 — Review the frozen work

Read-only checks:

```bash
tusker show W-0003 --capsule
tusker wave preflight W-0003 --json
tusker automation explain ORC-T-0040 --repo . --json
tusker setup doctor --repo . --json
```

Expected frontiers:

```text
1  ORC-T-0040
2  ORC-T-0041  ORC-T-0042
3  ORC-T-0043  ORC-T-0044
4  ORC-T-0045
5  ORC-T-0046  ORC-T-0047
6  ORC-T-0048
7  ORC-T-0049
```

Do not arm while preflight is red.

## Phase 2 — Bootstrap the scope guard interactively

`ORC-T-0040` must be implemented in one user-opened interactive session while
repository automation stays disabled. This is the one bootstrap exception to
the future universal work-session rule: that command does not exist yet.

Copy-paste prompt:

```text
We are bootstrapping Tusker wave W-0003.

Implement ORC-T-0040 only: “Make daemon dispatch authority explicit and
wave-scoped by default.”

Use the installed Tusker skill. Start with:
  tusker show ORC-T-0040 --capsule
  tusker packet ORC-T-0040 --for agent

Read only the routed project canon and the governing spec
docs/specs/14-opt-in-factory-execution-control.md. Characterize the current
behavior before changing it and implement every acceptance row with focused
tests.

Hard boundaries:
- Work in this interactive session; do not start a daemon, invoke automation
  dispatch, or launch nested Codex/Claude workers.
- Keep the Tusker project and automation disabled.
- Do not arm W-0001, W-0002, or W-0003.
- Do not touch unrelated ready/review tasks.
- Do not enable release, scheduled promotion, or paid triage.
- Preserve backward compatibility for existing explicitly broad automation,
  but make fresh setup default to armed_waves.
- Finish with exact command + PASS/FAIL proof and request independent review.

The task is not complete until fresh-default, armed-wave-only, stale-wave,
unrelated-ready, explicit all-eligible, legacy-effective, provenance, and
no-side-effect cases pass.
```

The resulting change must be independently reviewed and landed through the
normal deterministic lane. Install or link the resulting Tusker binary before
using its new policy to enable background work.

## Phase 3 — Prove the bootstrap guard

Required facts before enabling this repository:

1. fresh setup resolves `automation.enabled=false`;
2. fresh enabled setup resolves `dispatch_scope=armed_waves`;
3. an unrelated ready/rework task is refused in that mode;
4. a disarmed or stale wave is refused;
5. only the exact armed fingerprint becomes eligible;
6. legacy-effective broad mode is visible and warned;
7. changing scope has no dispatch, Git, release, or paid-work side effect.

Run the task proof plus the repository's normal focused validation. Do not
substitute a green unit test for installing the binary that the resident daemon
will actually execute.

## Phase 4 — Prepare unattended execution

After the scope guard is installed, make the repository satisfy the preflight
one blocker at a time:

1. configure isolated worktree execution;
2. configure an unattended runner profile whose effective approval policy does
   not pause on routine commands;
3. set project automation on with `dispatch_scope: armed_waves`;
4. ensure the resident daemon is managed and healthy;
5. enable this registered project;
6. rerun whole-wave preflight.

Use the current binary's doctor and preflight remedies as the source of exact
configuration changes:

```bash
tusker setup doctor --repo . --json
tusker projects list --json
tusker daemon service status --json
tusker wave preflight W-0003 --json
```

Do not continue until preflight returns `ok: true` and shows only `W-0003`
members in the authorized first frontier.

## Phase 5 — One explicit start

Arming is the human execution boundary:

```bash
tusker wave arm W-0003 --by human:sarav
```

Do not call `tusker automation dispatch`. The independently running resident
daemon should reconstruct and claim the authorized frontier.

Monitor with:

```bash
tusker wave brief W-0003
tusker automation status --json
tusker streams --json
```

Safe stops:

```bash
tusker wave pause W-0003 --reason "operator pause"
tusker wave disarm W-0003 --reason "scope withdrawn"
```

Pause/disarm stops fresh claims and retries. It does not forge a terminal
outcome for a live worker.

## Phase 6 — Rollout order

Use the waves in this order:

1. `W-0003` — reliable opt-in execution control;
2. `W-0002` — requirements-to-DAG intake and Start product;
3. `W-0001` — continuous staging and scheduled full-green promotion.

The IDs reflect creation order, not recommended execution order.

Keep the current conductor or delivery path authoritative until the relevant
shadow/cutover task proves parity. No repository other than the explicitly
enabled dogfood target changes behavior.
