# Complete task handoff

One bounded outcome is one task; one atomic wave request is one batch. The
author owns discovery, settled design, and the handoff text; the implementer
owns execution inside it. Every new agent task explicitly selects
`work_level`: `light`, `standard`, or `demanding`. Review inherits the work
level; write `review_level` plus a task-specific `review_reason` only for a
reasoned override. Choose the level by the worker judgment that remains, not
by ticket length. Configured profiles own model/harness choices; do not
author `runner_profile`, `execute_profile`, or `review_profile`.

| Work level | Choose for |
|---|---|
| Light | A bounded change with settled interfaces and a prescribed check. |
| Standard | Ordinary cross-file implementation, integration, or a settled test procedure. |
| Demanding | Technical ambiguity, shared-state races, security, or recovery design. |

If a human must supply intent, credentials, or a decision, use a named human
gate — an execution condition, not a task. An architect may implement its own
task through an explicit authorized work claim that records the actual
conversation and scope, while retaining author provenance and independent
review; an architect contact alone does not imply self-implementation. Reuse an
existing unchanged approval request and gate: an approved decision is durable
authority for that bound material, not a reason to author another gate.
Unresolved product decisions return to planning; interviewing is outside
this skill.

## Ordered steps

1. **Inspect before authoring.** Read the current source flow and callers,
   the task's existing packet (`tusker packet <ID> --for agent` for held
   work), and any governing spec section the change touches. Verify that
   named paths, functions, and types exist today; label anything proposed
   rather than verified.
2. **Write one complete contract.** For a single task use
   `tusker new task --title <title> --work-level <level> --body-file <path|->`.
   For a batch use `tusker wave create --file <request.yaml> --request-key
   <stable-key> --json` with one `tusker.wave-authoring/v1` request. The task
   body is the implementation contract: current problem, intended change and
   why, verified source/type seams, locked decisions versus proposals, edge
   and failure/recovery cases, upstream/downstream/shared ownership, and
   exact acceptance IDs each mapped to an exact check. There is no mandatory
   heading count, word quota, or keyword rubric — semantic completeness is
   judged by a reader, not a validator.
3. **Add only meaningful metadata.** Optional relationships (`dependencies`),
   ownership (`owned_paths`, `generated_outputs`), gates, and contacts
   (`architect`, `origin`, `peer_contacts`) belong only when they carry real
   content. `spec_refs` and `epic` are optional; every supplied ref must
   resolve. Normal authoring omits priority, size, risk, factory, and
   context metadata.
4. **Inspect the fresh packet and route facts.** Re-read
   `tusker packet <ID> --for agent` and `tusker show <ID> --capsule` as a
   cold reader; `tusker wave review <WAVE-ID> --json` shows member
   eligibility, frontiers, and blockers. Creation stays inert — nothing
   runs, and no readiness, arming, or scheduling step is required before
   Start.
5. **Start only on explicit authority.** `tusker task start <ID> --mode
   interactive --by <agent> --current-workspace --json` claims one task in
   this workspace; `--mode background` leaves a task-scoped run directive
   for the configured runtime. `tusker wave start <WAVE-ID> --mode
   background --by human:<name>|operator:<name> --json` authorizes the exact
   current material once; daemon polling then advances each dependency
   frontier automatically. `tusker wave pause|resume <WAVE-ID> --by
   human:<name>|operator:<name>` control admission; an explicit task Start
   inside a paused wave stays task-scoped and leaves the wave paused.
   Independent review and closeout remain required: a
   self-implementation claim never substitutes for the review lane.

## Ad hoc example

```sh
tusker new task --title "Fix retry backoff" --work-level light --body-file - <<'EOF'
# Fix retry backoff

## Intent

`cmd/worker/retry.go:nextDelay` caps backoff at 5s; raise the cap to 30s so
providers stop rate-limiting the queue.

## Acceptance

| ID | Outcome |
| --- | --- |
| A1 | The fourth retry waits at least 30s. |

## Verification

| Covers | Check | Result | Notes |
| --- | --- | --- | --- |
| A1 | command: go test ./cmd/worker -run Retry -count=1 | pending | |
EOF
```

## Substantial DAG example

An atomic wave request authors the whole graph in one transaction:

```yaml
schema: tusker.wave-authoring/v1
request_key: sched-wave-v1
title: Scheduler split
outcome: Split frontier queueing out of the daemon loop.
shared_context: |
  `queueAuthorizedWaveFrontier` is the only queue seam. `run_directives`
  rows are read-only for members.
tasks:
  - key: frontier-helper
    title: Extract frontier queue helper
    work_level: standard
    body: |
      ## Intent
      Extract `queueAuthorizedWaveFrontier` from `cmd/tusker/daemon.go` into
      `cmd/tusker/direct_wave_authority.go`.
      ## Failure and edge cases
      - Cycle `A -> B -> A` is rejected at authoring with `DEPENDENCY_CYCLE`.
      - A dependency naming a missing key is rejected with
        `DEPENDENCY_DANGLING`; the edge is never silently dropped.
      - A member is ready only while every required dependency is done.
      - A failed member leaves dependents blocked and the wave Waiting.
      - `task update` on a done member reopens it as rework; stale proof
        never certifies new material.
      ## Decisions
      Locked: edge direction is dependency -> dependent. Proposed but
      unapproved: renaming `dagAdvance` to `walkFrontier`.
      ## Ownership
      Owns `direct_wave_authority.go`; `daemon-wiring` consumes the helper;
      `run_directives` schema is shared and unchanged.
      ## Acceptance
      | ID | Outcome |
      | --- | --- |
      | A1 | Helper queues only ready members up to concurrency. |
      ## Verification
      | Covers | Check | Result | Notes |
      | --- | --- | --- | --- |
      | A1 | command: go test ./cmd/tusker -run TestDirectWave -count=1 | pending | |
  - key: daemon-wiring
    title: Wire frontier advance into daemon poll
    work_level: standard
    dependencies:
      - task: frontier-helper
        kind: hard
    body: |
      ## Intent
      Call `advanceAuthorizedWaveFrontiers` once per project poll before
      dispatch candidate creation.
      ...
```

Edge direction: the dependency object `{task: frontier-helper, kind: hard}`
on `daemon-wiring` means daemon-wiring requires frontier-helper done first.
Cycles and missing keys
are refused at authoring, not discovered at dispatch. Readiness is computed
from durable task state; creation never marks anything ready. A failed or
reworked member blocks its dependents without aborting unrelated waves.
`task update --if-revision` amends mutable fields under the material lock and
preserves identity, history, and proof lineage. A spec link
(`--spec-refs`/`spec_refs`) supplements task-specific guidance; it never
replaces it.

## Architect and peer contacts

Use the `architect`, `origin`, and `peer_contacts` task fields with verified
`task:<id>` or `execution:<id>` addresses; resolve each to the actual
registered task/execution and its provider conversation or host. Do not
fabricate execution IDs or assume a chat UUID is a registered Tusker
execution; record the verified native thread/host in the body when needed.
If no route is verified, leave typed contact metadata empty and record the
native thread/host as **unbound provenance** with a return-to-origin or
operator action — never describe an authored address as a working automatic
route. Distinguish architect (design decisions), origin (request/result
destination), and peers (shared interfaces or ownership); record whether the
route was inspected or actually exercised.

Tell the worker when to ask: conflicting requirements, missing upstream
contracts, ownership conflicts, or a change to a locked decision. A question
carries task and acceptance IDs, observed facts, the exact decision needed,
and a recommendation. Capture accepted decisions in canonical documentation
and the affected task contract, not solely in chat history.

## Cold-reader review before handoff

For integration work, name the initiating operation, receiving consumer,
promised interface and integration owner. Put the provider's contract check
before the dependent task; put full product-flow acceptance after both. For
reuse, authorization or recovery work, supply concrete changed-input,
invalid-input or interleaving cases with expected and forbidden observations.
Keep these in the body beside acceptance; small edits need only their relevant
case. A queued job proves admission, not receiver execution.

For each critical acceptance item ask: what plausible wrong implementation
would the planned check reject? Derive expectations from the agreed contract,
not from the implementation. Define the observation needed, not merely a test
name. Rules and cases may have local IDs when cross-referenced; they are not
new mandatory front-matter fields.

Read the generated packet as a fresh reader with no reliance on the
authoring conversation. A packet that names no verified source seam, no
exact check, or no failure case is semantically insufficient — that judgment
comes from comparing the packet against the source it names, never from a
structural validator; a terse body stays terse in the packet. For held work,
packet inspection is never execution authorization. When the packet
preserves one bounded outcome, its ownership, proof map, level/review
selection, and contact or explicit unbound fallback, the handoff is complete;
otherwise leave the work and label the missing gate.
