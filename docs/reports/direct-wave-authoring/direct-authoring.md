# Direct task and wave authoring

Direct authoring creates canonical task, gate, and wave records without a
delivery plan. Every record is inert on creation: tasks land as
`status: backlog` / `readiness: held`, and waves land `authorization:
disarmed`. Nothing is claimed or dispatched.

## Standalone task IDs

`tusker new task` no longer requires `--epic`. Without one, the task is
allocated in the reserved standalone namespace `TSK-T-0001`, `TSK-T-0002`,
… using the same collision-safe allocation as epic task IDs, and no `epic`
field is written to the record. Supplying `--epic APP` keeps the existing
`APP-T-NNNN` allocation and `epic` frontmatter.

Newly authored records do not carry `priority`, `size`, `risk`, factory, or
context metadata unless the caller supplies an explicit value.

## Single-task authoring

```bash
tusker new task --title "Implement auth" --work-level standard --body-file task-body.md
tusker new task --title "Implement auth" --work-level light --body-file - < task-body.md
```

- `--work-level light|standard|demanding` is required. `review_level`
  inherits the work level; a differing `--review-level` override requires
  `--review-reason`.
- `--body-file` preserves the body bytes exactly, including pipes and
  Markdown escapes, and normalizes to exactly one terminal newline. An
  absent or whitespace-only body is refused with `MISSING_FIELD`.
- `--spec-refs` stays optional, but every supplied path and `#section`
  anchor must resolve before any record is written.

## Task update

```bash
tusker task update TSK-T-0001 --if-revision sha256:<state_rev> \
  --title "Implement auth v2" --body-file v2.md --by agent:builder --json
```

`--if-revision` is required and must equal the task's current `state_rev`;
a stale or missing value is refused without mutation. At least one mutable
field is required: `--body-file`, `--title`, `--work-level`,
`--review-level` (with `--review-reason`), `--spec-refs`, `--dependencies`,
`--owned-paths`, `--generated-outputs`. Identity, history, status, and proof
fields are immutable through this command.

The update runs under the common material epoch lock plus the task
document lock: the task record, the dependency index, cycle validation,
the write, and the update event are all read and committed together, and
the task file and `updated` event land in a single preimage-guarded
transaction — a failed commit leaves both unchanged. Concurrent dependency
edits cannot interleave into a cycle — exactly one side of a racing
`A -> B` / `B -> A` pair commits.

If the contract material (body, title, levels, refs, dependencies, or owned
paths) changes on a task whose status is `done` or `review`, the same
transaction performs a controlled rework: `status: rework`,
`readiness: ready`, `proof_status: pending`, and the acceptance and closure
fields (`accepted_by`, `accepted_at`, `closed_at`, `close_authority`,
`closeout_status`, `machine_status`, `human_status`, `agent_action`) are
cleared so the task no longer projects as completed. Historical proof and
attempt material is preserved but no longer certifies the new fingerprint;
a strict task reports `STRICT_PROOF_STALE` until reproven. The update event
records `lifecycle_transition: rework`. For `backlog`, `held`, or
already-`rework` tasks the lifecycle fields are preserved while the
fingerprint updates.

## Atomic wave authoring

```bash
tusker wave create --file wave.yaml --request-key auth-wave-v1 --json
```

`--file -` reads the request from stdin. The request is a strict
`tusker.wave-authoring/v1` document (YAML or JSON; unknown fields fail):

```yaml
schema: tusker.wave-authoring/v1
request_key: auth-wave-v1        # or pass --request-key
title: Auth wave
outcome: Users can sign in.
shared_context: Shared context is written once here, in the wave brief.
tasks:
  - key: root
    title: Session model
    work_level: standard
    body: |
      # Session model

      Owns the session record and expiry rules.
  - key: ui
    title: Sign-in form
    work_level: light
    body: |
      # Sign-in form
    dependencies:
      - task: root
        kind: hard
human_actions:
  - key: creds
    task: ui
    owner: human:operator
    action: Provision staging OAuth credentials.
    verification: The provider reports ready.
    why_agent_cannot: Human account access is required.
```

The whole request validates before anything is written: schema, title, and
outcome; at least one task; unique nonempty keys; explicit valid work and
review levels; substantive bodies; resolvable spec refs; dependency targets
and kinds; an acyclic graph; no owned-path collisions inside one frontier;
existing epics for every task `epic` reference (empty means standalone
`TSK`); and complete human-action fields.

Under one material lock, all task IDs (`<EPIC>-T-NNNN` when a task names an
epic, `TSK-T-NNNN` otherwise), gate IDs, and the wave ID are allocated,
temporary dependency labels are resolved to durable `ID:kind` edges, and the
full record set is published in one preimage-guarded transaction. Any error
or injected fault leaves no partial task, wave, gate, or event graph.

The wave frontmatter stores a durable operation receipt, separate from
executable material: `authoring_request_key`,
`authoring_request_fingerprint`, and an `authoring_receipt` block with
schema `tusker.wave-authoring-receipt/v1` carrying the exact task key →
durable ID and gate key → durable ID maps. Receipt keys are identity only;
they are never dependencies or runtime authority. Re-sending the same key
and fingerprint replays from the receipt and returns the original mappings
even if wave member or gate order has changed since creation; the same key
with changed content conflicts with `ALREADY_EXISTS`. If the receipt is
missing or corrupt, a mapped record is missing, or a mapped task or gate no
longer matches the authored wave association, replay refuses with
`AUTHORING_RECEIPT_DRIFT` rather than inferring mappings from current
ordering. Request bytes and the input file path are never stored;
temporary keys persist only inside the immutable idempotency receipt and
never become task dependencies or runtime authority.

The JSON response carries the durable wave ID, the task key → ID and path
mapping, gate IDs, computed frontiers, and expected concurrency.

## Errors

- `MISSING_FIELD` — absent or whitespace-only body.
- `AUTHORING_REQUEST_INVALID` — schema, classification, ref, epic, or
  human-action contract violation.
- `DEPENDENCY_DANGLING` / `DEPENDENCY_CYCLE` — unresolved or cyclic edges.
- `OWNED_PATH_FRONTIER_CONFLICT` — same-frontier ownership collision;
  serialize with a dependency or split ownership.
- `CAS_CONFLICT` — `task update` with a stale `state_rev`.
- `ALREADY_EXISTS` — a reused `request_key` with changed content.
- `AUTHORING_RECEIPT_DRIFT` — identical replay found the immutable receipt
  missing, corrupt, or pointing at records that no longer match the
  authored wave association.
