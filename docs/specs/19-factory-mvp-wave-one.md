---
capsule:
  what: "One inert V2 contract for a singleton, opt-in Tusker factory dogfood run."
  use_when:
    - "Preparing the bounded factory MVP activation sequence and its post-run audit."
  skip_when:
    - "Enabling automation, arming a wave, starting a daemon, promoting a ref, releasing, or spending."
---

# Factory MVP wave one

Status: proposed V2 delivery contract
Date: 2026-07-26

## Active target

The active prospective import is `factory-mvp-wave-one/v2`. Its only delivery
source is `capture-first-run-readiness`; a fresh source-built dry-run must map
it to **prospective** `ORC-T-0051` in **prospective** `W-0005`, using
`integration/W-0005`. Nothing has been imported, armed, enabled, dispatched,
or run by writing this contract.

After separately approved cutover steps, the daemon may dispatch exactly one
Terra-medium `implementation-terra` attempt. The worker writes
`docs/reports/factory-mvp-first-run.md`, then submits to the configured
`reviewer-terra` profile: Terra high, read-only, and capped at three review
cycles. The deterministic control plane—not the worker or reviewer—owns
authoritative staging completion. A coordinator observes the actual review
and completion result afterward. `main` does not move. The legacy conductor
remains authoritative throughout this MVP; transfer is a separate later human
approval, never an outcome of this run.

## Requirements

| ID | Outcome |
| --- | --- |
| R1 | One Terra-medium implementation attempt records the two focused operations/control tests in the durable report. |
| R2 | The report records the exact approved activation and isolation tuple, including the two automation toggles and staging configuration. |
| R3 | Only bounded Terra-high read-only review follows; the coordinator, after review, observes authoritative staging completion. No default-ref promotion occurs. |

## Binding boundaries

- Planning, context, doctor, review, dry-run, and import are inert. They never
  enable a project, alter the global registry, arm a wave, start/install a
  daemon, dispatch a worker, move a ref, release, spend, configure credentials,
  or push.
- The task is one low-risk node, concurrency one. The worker does not perform
  enable/disable, arm/disarm, daemon/service, binary, registry, import, or
  completion/promotion configuration mutations. It only observes the approved
  state and writes its report.
- `main` is snapshot-checked during worker work and must not change.
  `integration/W-0005` snapshots are optional worker context; a post-review
  integration advance is deterministic-control-plane evidence, never worker
  proof or self-attestation.
- No `all_eligible` dispatch, other project, provider credential, provider or
  release work, paid triage, extra implementation attempt, default-ref
  promotion, or scheduled promotion is in scope. Scheduled promotion remains
  disabled.
- The root coordinator must compute a new context fingerprint after this
  contract is merged into the registered root repository and after the approved
  activation configuration is in place. The worktree fingerprint below is only
  a structural authoring check; it is not activation authority.

## One-time cutover diagnostics and activation order

These are operator diagnostics, not a recurring product workflow.

1. Approve a durable binary update and Full Disk Access for the managed-service
   host. Apply the reviewed configuration commit that sets all three enable
   switches—`tusker.yaml` `automation.enabled: true`,
   `.tusker/WORKFLOW.md` `automation_enabled: true`, and registry project
   enablement—and exactly `automation.completion_reactor.mode: authoritative`.
   That reviewed commit also keeps scheduled promotion disabled. Only then run
   `make install-bin`; before then, use `go run ./cmd/tusker ...`.
2. Record the enabled registry set. Before starting the global daemon,
   temporarily disable exactly these unrelated enabled projects and later
   restore precisely that recorded set: CarelessWhisper
   (`01KXFGVD3NQY780QTDCVX933JN`), backend
   (`01KX5WD37K47F1C19EC08T94PN`), cinta
   (`01KXJXEAAHP5VGTN9ANZ5KE72M`), and rznapp
   (`01KXJPZPC9VTEPR66NFYT9JAAZ`). No other registry mutation is authorized.
3. Read back all three switches and `completion_reactor.mode: authoritative`,
   keep `dispatch_scope: armed_waves`, and retain zero implicit delivery units
   (`! rg -n '^delivery_unit:' .tusker/work/waves`). Derive the runtime store
   and require zero rows for this project in both `review_results` and
   `completion_transactions`. This is stronger than a V3-only check: no prior
   stored review or completion transaction is silently inherited.
4. Source-built operations and automation status must show every pre-existing
   wave `W-0001` through `W-0004` disarmed. With all waves disarmed, the human
   starts the service first, exactly:

   ```sh
   tusker daemon service install --allow-protected-projects --json
   tusker daemon service status --json
   tusker daemon status --json
   ```

   Verify it is alive and reconciling before the V2 plan is re-fingerprinted,
   reviewed, imported, or started. Interactive agents may inspect state and
   implement direct work, but cannot start the daemon service or dispatch the
   factory.
5. Only after the service is healthy, the root coordinator recomputes the root
   context fingerprint, patches the V2 plan, then runs doctor/review and inert
   import. A human separately approves exactly one Terra-medium implementation
   attempt and at most three Terra-high read-only review cycles, naming the
   launch-count and token ceiling; no additional implementation, triage, or
   provider attempt is authorized.
6. The human issues the one fingerprint-bound `tusker delivery start` action
   only after the preceding review/import. It is the first point at which the
   imported singleton may arm.

## Worker verification

The task report records source-built `setup doctor`, strict skill doctor,
`validate` (including a nonzero exit as a blocker, never false green), projects,
factory operations, automation status/queue, daemon/service status, and the
prospective wave brief/preflight. It must prove a fresh inert dry-run maps
`capture-first-run-readiness -> ORC-T-0051` and `W-0005`, execute:

```sh
go test ./cmd/tusker -run '^TestFactoryOperationsProjection$' -count=1
go test ./cmd/tusker -run '^TestFactoryExecutionControl$' -count=1
```

It records before/after run and queue state, confirms `armed_waves`, and proves
no `all_eligible` widening. The `main` snapshot is mandatory. The integration
snapshot is optional and cannot be used by the worker to claim staging
completion.

## Coordinator post-review check

After the worker submits and the configured review cycles finish, the
coordinator performs one read-only check: actual reviewer count/verdict,
deterministic completion state, any normal `integration/W-0005` advance, and
an unchanged `main`. It does not edit the worker report, create another model
attempt, change registry/configuration, arm/enable/start anything, promote,
release, spend, or configure a provider.

## Return to baseline

Whether the post-review audit succeeds or rolls back, return in this order:
disarm `W-0005`; stop and uninstall the managed service; disable the Tusker
registry project; apply a reviewed rollback commit setting both repository
automation switches false, completion reactor disabled, and scheduled promotion
disabled; verify zero active runs; only then restore the exact four-project
registry ledger captured before isolation. The installed binary may remain.

## Historical v1 cancellation (not an active target)

The following generated block is preserved as durable evidence only. The v1
contract was cancelled before any attempt, review, completion, or ref movement;
its one task was cancelled and its wave remains disarmed. It must not be used
for activation or as an alias for the V2 target above.

<!-- tusker:delivery-import:91b2fb97ca8e3122:begin -->

- `[[ORC-T-0050]]` implemented historical delivery source
  `capture-first-run-readiness`; it was cancelled before any attempt.

- `[[W-0004]]` is the disarmed, cancelled-before-run historical delivery wave.

<!-- tusker:delivery-import:91b2fb97ca8e3122:end -->
