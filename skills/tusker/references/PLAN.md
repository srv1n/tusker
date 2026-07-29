# Plan

Use this guide for requirements, decomposition, delivery review, held import,
and fingerprint-bound Start.

## Intake

Ask only for product facts: desired outcomes, observable acceptance, important
tests and failure cases, constraints, priorities, non-goals, and genuine
unresolved decisions. Tusker and the agent own IDs, dependency syntax, waves,
frontiers, runners, workspaces, proof modes, retries, review, and integration.

One genuinely bounded implementation outcome may use a direct task. Multiple
independently provable outcomes, parallel lanes, or unattended delivery require
a versioned `tusker.delivery-plan/v2` DAG with a stable semantic scope. Use
source keys; Tusker allocates durable epic, task, gate, wave, revision, and
event IDs at import.

An epic groups a product outcome; an epic is never executable authority. A
wave is separate, fingerprint-bound authorization over exact tasks, gates,
dependencies, context, and policy. A lone task may be a wave of one.

## Build the review

```bash
tusker delivery context --spec <SPEC> --scope <STABLE-SCOPE> --json
tusker delivery doctor --plan <PLAN.yaml> --json
tusker delivery review --plan <PLAN.yaml> --json
tusker delivery import --plan <PLAN.yaml> --dry-run
tusker delivery import --plan <PLAN.yaml>
```

Context and doctor bound the current product/spec/canon inputs. Doctor must
pass before import. Review is a read-only product projection; it reports
`planValid`, `importReady`, and `startReady` independently.

Review and held import do not require project automation, daemon liveness,
runner availability, a clean live integration lane, or an armed wave. Import
validates contract, context, stable scope, held lineage, and atomic write
safety, then creates or reconciles held disarmed records. Invalid plans or
unsafe import material fail with contract/import blockers. Start-only blockers
remain visible but do not make a valid review fail.

Never hand-create an arbitrary multi-task series, caller-assign final Tusker
IDs, or hand-edit imported lifecycle fields. Dependencies unlock frontiers;
handwritten progress lists do not.

## Start unattended delivery

Start is the only delivery transaction that checks unattended authority:

```bash
tusker delivery start --plan <PLAN.yaml> \
  --confirm <PLAN-FINGERPRINT> \
  --by human:<name>
```

Run it only after the human explicitly chooses that exact action. Start
revalidates plan and context fingerprints, imported lineage, project opt-in,
runner and approval policy, daemon liveness, workspace isolation, integration
cleanliness, and exact authorization material. It atomically reconciles held
records and arms only the exact resulting wave. It does not create missing
infrastructure or wider authority.

New automation remains opt-in and uses
`automation.dispatch_scope: armed_waves`. A stale, paused, or disarmed wave
cannot produce new daemon claims.

## Plan proof

Map each task acceptance item to the smallest deterministic check and explicit
artifact. Give shared generated files, migrations, lockfiles, and integration
ownership to one lane. Use dependencies for order and a serialized integration
task for convergence. Run `tusker delivery doctor` again after material plan
changes; a changed fingerprint requires a new review and exact confirmation.

Planning never enables automation, starts or installs a daemon, dispatches a
model, calls a provider, reads secrets, moves refs, lands, releases, or spends.
