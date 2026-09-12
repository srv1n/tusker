# Complete task handoff

One bounded outcome is one task. The planner owns discovery, settled design and
the handoff; the implementer owns execution within it. A lighter work level is
not permission to omit context or ask the worker to rediscover product intent.
Use configured work/review levels rather than fixed model names. Interviewing
is outside this skill; unresolved product decisions return to planning.

Before authoring, inspect the current source flow and callers, governing spec
sections, and overlapping tasks. Then write the following in the existing task
body and supported metadata; do not add a second contract or a word-count quota:

| Location | Required content |
|---|---|
| Intent | Current problem, intended change, why it matters, and a concrete before/after example in plain language. |
| Implementation notes | Existing authority and entry points to extend; exact spec sections and decision links; locked behavior versus implementation suggestions. Verify existing paths and label proposed paths. |
| Surrounding work in implementation notes | Related task IDs, required upstream outputs, downstream consumers, shared-file ownership and integration responsibility. Explain dependency edges; explicitly say when there are none. |
| Non-goals | Scope exclusions and invariants the change must preserve, including applicable migration, compatibility and execution boundaries. |
| Acceptance | Stable IDs for observable outcomes, including relevant failure/recovery, migration, security and accessibility cases. A report's existence alone does not prove behavior. |
| Verification | For each acceptance ID: scenario/setup, expected result, exact check and evidence location/limits. Map only outcomes that the check actually exercises; zero matched tests is failure. |
| Contacts in implementation notes and metadata | Verified architect/origin and relevant peers, their responsibilities, supported reply route and what to do if unavailable. See below. |

Reference canonical specs instead of copying them, and use an exact heading
anchor in mandatory `spec_refs` when only part of a document governs the task.
A worker should not search a whole design pack to find its contract. Keep
task-specific direction visible;
do not bury it beneath repeated generic execution rules.

### Architect and peer contacts

Use existing `architect`, `origin` and `peer_contacts` task fields; delivery
plans use `architect`, `origin` and per-task `peers`. Verify supported CLI/schema
syntax before writing. An address is `task:<id>` or `execution:<id>`: resolve it
to the actual registered task/execution and its provider conversation/host.
Do not fabricate execution IDs from wave names or assume a chat UUID is a
registered Tusker execution. Record the verified native thread ID and host in
the implementation notes when needed to reach the architect through host tools.

Distinguish architect (design decisions), origin (request/result destination),
and named peers (specific shared interfaces or ownership). One contact may fill
multiple roles. Record whether the route was inspected or actually exercised;
an authored address or durable receipt does not prove live delivery or reply.
Check installed message help/capabilities before prescribing commands. If the
route is unavailable, state that and the explicit return-to-origin/operator
fallback. Do not invent a routable contact to make a ticket appear complete.

Tell the worker when to ask: conflicting requirements, missing upstream
contracts, ownership conflicts, or a change to a locked decision. Routine local
implementation choices remain with the worker. A question carries task and
acceptance IDs, observed facts, the exact decision needed and a recommendation.
Wait on dependent work; continue independent owned work. Send only through an
authorized supported channel, and capture accepted decisions in canonical
documentation and the affected task contract, not solely in chat history.

### Readiness review before handoff

Inspect the generated packet and its exact references as a fresh reader, with
no reliance on the planning conversation. For held work, `tusker packet <ID>
--for agent --force` is inspection only, never execution authorization.
Record a short readiness review in the implementation notes: how to start,
what must remain true, which surrounding work matters, how each outcome is
proved, and who resolves uncertainty. Name remaining gaps; do not mark the
handoff ready while an essential decision, dependency contract or ownership
boundary is missing. An unavailable live contact can use an explicit fallback
when no unresolved decision prevents work.

Run the existing import dry-run/preflight checks for structural validity.
Those reject malformed contracts and proof mappings; they do not judge whether
the design is complete or a command proves the claimed scenario. The planner's
readiness review supplies that semantic check. If an independent planning
review is already assigned, give that reviewer only the packet and references.
Do not start another agent or add a human approval gate just for this step.
