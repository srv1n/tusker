---
capsule:
  what: "Exact one-time activation and rollback runbook for the singleton V2 factory MVP."
  use_when:
    - "An operator has explicitly chosen the first Tusker daemon dogfood run."
  skip_when:
    - "Normal interactive work or phase-two strict/provider expansion."
---

# Factory MVP cutover

## Decision

**Code readiness is green; activation is deliberately still off.** The target
is the not-yet-imported V2 contract, whose fresh root dry-run must prospectively
allocate `ORC-T-0051`, `W-0005`, and `integration/W-0005`. This runbook does not
grant any action by itself.

The MVP is one Terra-medium implementation attempt, configured bounded
Terra-high read-only review (maximum three cycles), and deterministic
authoritative staging completion. The existing conductor remains authoritative
throughout this MVP; any transfer is a separate later human approval. `main`,
release, spend, provider credentials, and scheduled promotion remain out of
bounds.

## Current facts

- The operations projection and adaptive/cold parity slice are code-green.
- The installed Tusker binary is stale. Before its separately approved update,
  use source-built `go run ./cmd/tusker ...` commands.
- The root project is registered but automation-disabled. No daemon service is
  installed or live. None of this is permission to repair, enable, arm, or
  start anything from an interactive agent session.
- The global registry currently has four unrelated enabled projects:
  CarelessWhisper, backend, cinta, and rznapp. They must be temporarily
  disabled for the isolated global-daemon observation, then restored exactly.
- The previous v1 singleton (`ORC-T-0050` / `W-0004`) was cancelled before any
  run and remains disarmed. It is historical evidence only, never the target.

## Read-only preparation

Run in the registered root repository, not an unregistered authoring worktree:

```sh
go run ./cmd/tusker delivery context \
  --spec docs/specs/19-factory-mvp-wave-one.md \
  --scope factory-mvp-wave-one/v2 --json
go run ./cmd/tusker delivery doctor \
  --plan docs/plans/19-factory-mvp-wave-one-v2.yaml --json
go run ./cmd/tusker delivery import \
  --plan docs/plans/19-factory-mvp-wave-one-v2.yaml --dry-run --json
go run ./cmd/tusker setup doctor --repo . --json
go run ./cmd/tusker skill doctor --strict --json
go run ./cmd/tusker validate --json
go run ./cmd/tusker projects list --json
go run ./cmd/tusker factory operations --json
go run ./cmd/tusker automation status --json
go run ./cmd/tusker daemon status --json
go run ./cmd/tusker daemon service status --json
```

Refuse unless the dry-run maps the source key to exactly `ORC-T-0051`, exactly
`W-0005`, and concurrency one. It must not move `main` or create an integration
ref. The plan's authoring-worktree context fingerprint is structural only; the
root coordinator recomputes and patches it after merge and approved
configuration, then repeats doctor/dry-run.

## Approval and isolation sequence

Every numbered mutation below needs the named human approval. No interactive
agent may start the daemon/service or invoke dispatch.

1. **Durable prerequisites:** approve Full Disk Access for the managed-service
   host and the binary update, then apply a reviewed configuration commit that
   sets all three enable switches: `tusker.yaml` `automation.enabled: true`,
   `.tusker/WORKFLOW.md` `automation_enabled: true`, and later registry
   enablement for Tusker project `01KXJNZRC931C7E7FPMXBP7QQ1`. The same reviewed
   commit must set exactly `automation.completion_reactor.mode: authoritative`
   and keep scheduled promotion disabled. Only after that approval may the
   human run `make install-bin`. Until then, the stale installed binary is not
   a readiness signal.
2. **Registry isolation:** record the current enabled set, then approve exactly
   these temporary disables before global-daemon start:

   ```sh
   tusker projects disable --id 01KXFGVD3NQY780QTDCVX933JN --json # CarelessWhisper
   tusker projects disable --id 01KX5WD37K47F1C19EC08T94PN --json # backend
   tusker projects disable --id 01KXJXEAAHP5VGTN9ANZ5KE72M --json # cinta
   tusker projects disable --id 01KXJPZPC9VTEPR66NFYT9JAAZ --json # rznapp
   ```

   Do not alter kurpod or any other record. After the daemon is stopped, restore
   precisely the recorded enabled set—no guessed "all enabled" reset.
3. **Third enable switch and readbacks:** after the reviewed commit, approve
   only the Tusker registry change:

   ```sh
   tusker projects enable --id 01KXJNZRC931C7E7FPMXBP7QQ1 --json
   tusker projects list --json
   tusker automation status --json
   tusker factory operations --json
   ```

   Readbacks must show the two repository switches, the registry enablement,
   `dispatch_scope: armed_waves`, and exactly
   `completion_reactor.mode: authoritative`; never widen to `all_eligible`.
4. **Clean authoritative staging:** derive the runtime-store path from the
   source-built daemon status and require zero rows for the Tusker project in
   both tables:

   ```sh
   RUNTIME_STORE_PATH=$(go run ./cmd/tusker daemon status --json | jq -r '.status.runtime_store_path')
   sqlite3 "$RUNTIME_STORE_PATH" \
     "SELECT COUNT(*) FROM review_results WHERE project_id='01KXJNZRC931C7E7FPMXBP7QQ1';"
   sqlite3 "$RUNTIME_STORE_PATH" \
     "SELECT COUNT(*) FROM completion_transactions WHERE project_id='01KXJNZRC931C7E7FPMXBP7QQ1';"
   ! rg -n '^delivery_unit:' .tusker/work/waves
   ```

   Both SQL results must be `0`. This forbids inheriting any stored review or
   completion transaction, not merely a legacy V3 subset; the ripgrep check
   forbids implicit delivery units.
5. **Service first:** source-built automation status and factory operations
   must show `W-0001` through `W-0004` disarmed, scheduled promotion disabled,
   and no active runs. The human then starts the managed service—before V2
   re-fingerprinting, review, import, or delivery start—using exactly:

   ```sh
   tusker daemon service install --allow-protected-projects --json
   tusker daemon service status --json
   tusker daemon status --json
   ```

   Refuse unless the service is alive and reconciling while every wave remains
   disarmed. Interactive agents never run these commands.
6. **Re-fingerprint, review, import:** only after the service is healthy, the
   root coordinator recomputes root context and patches the plan fingerprint,
   then runs:

   ```sh
   go run ./cmd/tusker delivery context --spec docs/specs/19-factory-mvp-wave-one.md --scope factory-mvp-wave-one/v2 --json
   go run ./cmd/tusker delivery doctor --plan docs/plans/19-factory-mvp-wave-one-v2.yaml --json
   go run ./cmd/tusker delivery review --plan docs/plans/19-factory-mvp-wave-one-v2.yaml --json
   go run ./cmd/tusker delivery import --plan docs/plans/19-factory-mvp-wave-one-v2.yaml --json
   ```

   The human separately approves exactly one Terra-medium implementation attempt
   plus at most three Terra-high read-only review cycles, recording a
   launch-count and token ceiling. No other implementation, triage, provider,
   release, or model attempt is authorized.
7. **Human start:** after review/import, the human alone uses the reviewed plan
   fingerprint returned by delivery review:

   ```sh
   tusker delivery start --plan docs/plans/19-factory-mvp-wave-one-v2.yaml \
     --confirm sha256:<reviewed-plan-fingerprint> --by human:<name>
   ```

   This arms only `W-0005`; the only expected implementation claim is
   `ORC-T-0051`. Interactive agents do not start the service or dispatch.

## Observation and success

Observe with source-built `wave brief W-0005`, `wave preflight W-0005`,
`automation status`, `automation queue --repo .`, `factory operations`, and
daemon/service status. The worker writes the report and submits. The
Terra-high reviewer(s) are read-only. The deterministic control plane owns the
only completion transaction; afterward the coordinator makes one read-only
audit of review count/verdict, task completion, optional `integration/W-0005`
advance, and unchanged `main`.

Success means exactly one singleton task was claimed once; no unrelated project
or claim ran; no broad scope/default ref/release/spend/provider action happened;
the report and operations surface agree; all temporary registry disables were
restored only after Tusker returned to baseline; and the legacy conductor
remained authoritative and usable.

## Rollback

On post-success or on unexpected claim, stale fingerprint, daemon-health
failure, review-cap breach, transaction row appearing before the authorized
run, ref ambiguity, or budget breach, a human returns to baseline in exactly
this order:

```sh
tusker wave disarm W-0005 --reason "factory MVP baseline return"
tusker daemon service stop --json
tusker daemon service uninstall --json
tusker projects disable --id 01KXJNZRC931C7E7FPMXBP7QQ1 --json
# Apply the reviewed rollback commit: both repository automation switches false,
# completion_reactor.mode: disabled, scheduled promotion disabled.
tusker automation status --json
```

Verify zero active runs, then—and only then—restore the exact recorded
four-project registry enabled ledger. Preserve history and diagnostics. Do not
delete history, blindly kill a live lease, rewrite task state, move refs,
disable or transfer authority away from the legacy conductor, or reactivate the
historical v1 singleton. The installed binary may remain.

## Deferred

Plan 17 convergence, Plan 18 K1–K5 strict authority, provider backend/full-gate
expansion, release, paid triage, notifications, and scheduled promotion are
phase two. They are not repair work for this dogfood run.
