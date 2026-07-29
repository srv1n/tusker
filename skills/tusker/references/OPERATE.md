# Operate

Use this guide for resident-daemon automation, wave control, integration,
fleet diagnosis/repair, and recovery.

## Automation ownership

Tusker automation is opt-in per project. `tusker automation plan` and status
surfaces are read-only; they do not dispatch. Only an independently managed
resident daemon may turn an eligible plan into a background worker. Interactive
agents may inspect or explicitly change requested settings, but never start the
daemon or dispatch from their session.

Eligibility combines task state, dependencies/gates, project configuration,
exact wave authorization, runner policy, workspace/integration safety, runtime
capacity, and circuit/budget controls. Keep these typed dimensions separate;
one boolean must not masquerade as project health.

Wave controls:

```bash
tusker wave preflight <WAVE-ID> --json
tusker wave arm <WAVE-ID> --by human:<name>
tusker wave pause|resume|disarm <WAVE-ID> ...
```

Preflight is read-only. Arm is explicit fingerprint-bound authorization and
must never be inferred from an epic, plan, review, import, project enablement,
or prior wave.

## Review and integration

Daemon workers and reviewers follow their injected attempt-bound packets;
operators do not replace typed results with prose. Deterministic handlers own
review consumption, merge, landing, closure, and successor wake.

For multiple lanes, one integrator owns shared namespaces, lockfiles,
migrations, generated files, merge order, and the serialized integration
branch. Each lane reports branch, head SHA, workspace, dirty state, changed
files, proof/gate verdicts, and unresolved overlap. Never merge an unproven or
ambiguous lane.

Run cheap preflight first. Gate-tier validation is serialized, harvests all
failures, and never repeats on an unchanged tree. Slow-compile projects batch
format/lint/test after coherent implementation instead of paying the same
compile setup per edit.

## Fleet health and repair

```bash
tusker delivery rollout doctor --json
tusker delivery rollout repair --scope core --dry-run --json
tusker delivery rollout repair --scope core|automation|service|integrations
```

Doctor reports core compatibility, interactive readiness, automation
configuration, wave authorization, project runtime, optional integrations, and
the managed service independently. Missing vaults or incompatible core schemas
may quarantine core work. Missing/stale optional providers block only workflows
that require them.

Repair defaults to `core`. Each explicit scope changes only deterministic
files/config in that authority domain:

- `core`: vault registration, workflow location/schema, canonical generated
  skill installs;
- `automation`: known runner/policy configuration, never enablement;
- `service`: definition files only, never load/start;
- `integrations`: deterministic local adapter config, never credentials or
  provider calls.

No repair enables automation, arms a wave, changes credentials, invokes a
provider, moves a ref, releases, spends, or starts a service. Service start and
project enablement remain separate explicit operations. Re-run doctor until
the selected scope is idempotent; one broken project must not block siblings.

## Recovery

Use typed status before intervention:

```bash
tusker daemon status
tusker automation status --json
tusker runs inspect <TASK-ID> --json
tusker trace list
```

Do not steal a healthy lease. Expired work is reclaimable only after heartbeat
grace and failed holder-liveness checks. Pause/disarm stops future claims, not
already running work. Preserve exact task/work revisions and authorization
fingerprints during redrive.

For invariant, crash-loop, budget, provider, or integration failures, repair
only the named domain and leave unrelated projects runnable. Provider
credentials, billing, release, destructive recovery, and subjective production
acceptance require explicit human authority.

For direct Codex/Claude execution identity, provider-native child visibility,
unbound-binding conflicts, timeline cursor reset, or cancellation settlement,
use `tusker execution inbox|list|show|cancel` and read
`docs/runbooks/execution-observability.md`. Provider observations remain
authority-neutral: do not manufacture a lease, retroactively make pre-binding
history proof-eligible, or infer child/process termination from a parent event.
