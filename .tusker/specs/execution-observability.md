---
title: "Execution observability: names, lineage, and truthful multi-agent tracking"
subject: execution-observability
keywords: [executions, agents, subagents, names, lineage, timelines, direct work]
part_of: software-factory
status: canonical
created: 2026-07-29
read_when: "Planning, implementing, or reviewing execution identity, direct Codex or Claude registration, child-agent tracking, run timelines, or the multi-agent operator UI."
skip_when: "Working on task lifecycle or daemon dispatch with no change to execution identity, relationships, provider events, or operator visibility."
decisions_locked: true
updates:
  - docs/system/00-overview.md
  - docs/system/cli.md
  - docs/system/orchestration.md
  - docs/system/serve-ui.md
sources:
  - "[[software-factory]] and its standing rule that work registers in Tusker no matter who drives"
  - "Operator grill session 2026-07-29 — [[2026-07-29-execution-observability-grill]]"
  - "Tusker/Paseo architecture comparison against getpaseo/paseo commit 504b687f8952a0a7ec5b5fdc772b946ddf903a18"
---

# Execution observability

## Why we are building this

Tusker can safely claim a task, start a worker, survive daemon replacement, and
stop the correct process. The operator still loses the plot when one wave
starts several workers or a Codex or Claude session creates children of its
own. The machinery is safer than the screen describing it.

That visibility gap pushes the operator back to manual orchestration. Direct
Codex, Codex Cloud, and Claude work is useful and must remain possible, but it
cannot become invisible work that only exists in a terminal, provider page, or
raw log.

The outcome is one truthful place to answer:

- What did I start?
- What human name did I give it?
- Which ticket and wave does it belong to?
- What did it spawn?
- Which pieces can Tusker control?
- What is alive, waiting, stopping, finished, or lost?
- Can I reconnect without silently missing history?

## The customer story

An operator can open a named wave or execution and see every top-level worker,
retry, managed child, and provider-native child as a related strand of work.
The view remains useful when the parent turn finishes, the daemon restarts, or
the work began directly outside the daemon.

The operator can use plain names such as `Lease recovery audit`, while Tusker
keeps immutable identifiers and ticket bindings underneath. A display name is
for recognition; it is never the authority used for ownership, resume,
cancellation, proof, or landing.

## Locked decisions

1. **Every execution has a Tusker-generated immutable ID.** Provider IDs are
   stored as correlation facts, not used as Tusker identity.
2. **Every execution may have a human display name.** The UI combines ticket
   and name for readability but stores them separately.
3. **Task and wave bindings are optional at registration.** Direct exploratory
   work may begin unbound and appears in an explicit unbound inbox.
4. **Unbound work has no delivery authority.** It cannot contribute task proof,
   request task review, land a branch, or close a ticket until it is attached
   through an audited conflict-checked operation.
5. **Work registers regardless of who drives.** The resident daemon remains the
   default dispatcher, while direct Codex, Codex Cloud, and Claude executions
   use the same observation model without pretending the daemon launched them.
6. **Names never substitute for IDs or relationships.** A reusable agent name
   such as `reviewer`, an instance name such as `Lease audit`, a ticket ID, a
   provider session ID, and a child ID remain distinct fields.
7. **The resident daemon remains the only background dispatcher.** A worker may
   report provider-native children or request separately managed work, but it
   does not recursively launch another Tusker runner.

These decisions refine the existing [[software-factory]] decision that all work
must register in Tusker.

## Plain-language model

### A wave is the batch, not one giant process

A wave already has a stable ID and title. Its member tasks may become runnable
at different times and may each have retries. The wave is the operator's batch
view and authorization boundary; it is not itself a Codex or Claude process.

### An execution is one observable strand of work

An execution is a durable Tusker record for one top-level worker, direct
provider session, independently managed child, or provider-native child.
Executions may be grouped under a task and wave without collapsing those
different concepts.

Each direct invocation creates one execution root. Each wave authorization
generation also creates one logical root, even though that root is a batch
anchor rather than a model process. Wave-member task attempts are independently
leased managed children below that root. A later authorization generation does
not silently reuse the previous graph root.

### Three relationships must never be conflated

| Relationship | Meaning | Independently owned? |
|---|---|---|
| Retry or resume | A later attempt continues the same logical work | No new concurrent child |
| Managed child | Tusker separately schedules and owns another execution | Yes: its own claim, lease, workspace, controls, and outcome |
| Provider-native child | Codex or Claude creates work inside its own runtime | Usually no: visible, but provider-owned unless capability proves otherwise |

Retries do not count as active children. Provider-native children do not
receive fake leases. Managed children do not disappear merely because a parent
session finishes.

## Naming and binding

Each execution records:

- an immutable Tusker execution ID;
- an optional display name;
- zero or one task binding;
- zero or one wave binding, which must agree with the task's canonical wave
  when a task is attached;
- its source (`daemon`, `direct_codex`, `direct_claude`, or `codex_cloud`);
- provider, provider session/task ID, reusable agent type, and provider child
  ID where available;
- parent and root execution IDs;
- who registered or attached it and when.

The default display falls back in this order:

1. operator-provided execution name;
2. provider task name or spawn label;
3. reusable agent type plus a short immutable ID;
4. provider name plus a short immutable ID.

Changing a display name is audited. Attaching or moving a task/wave binding is
more restrictive: it refuses a conflicting live owner, preserves the old
binding in history, and re-evaluates what the execution may prove or control.

## Work started outside the daemon

Direct work uses the same identity envelope but a different authority path.

- An interactive session still uses the canonical work-session protocol before
  editing a tracked task.
- A direct execution can register before launch or attach after a provider
  session/task ID becomes available.
- Local launchers may report process identity and heartbeats.
- Cloud executions report provider status and provider timestamps; they never
  invent a local PID or OS-death proof.
- Registration and observation never arm a wave, enable automation, dispatch a
  daemon worker, or grant proof/landing authority.
- Hooks and JSON event streams are untrusted adapter input. They may append
  idempotent observations but cannot grant ownership, bind a task, or finalize
  delivery.

The intended operator surface is a small `tusker execution` command family for
registering, launching where appropriate, attaching, naming, inspecting, and
finishing direct work. Exact provider commands remain provider-specific. A
launcher refuses when invoked from a dispatched worker or another model
session that is forbidden to create nested runners.

## Child discovery

Tusker consumes explicit provider events rather than scraping prose:

- Codex top-level thread events and subagent start/stop hooks;
- Claude session metadata and SubagentStart/SubagentStop hooks;
- Codex Cloud task identifiers and status polling;
- future adapters through the same typed event contract.

A provider event includes a source event ID or sequence, parent provider
session, child provider ID, reusable agent type, optional display label,
status, timestamp, and capability facts. Replaying the same event is harmless.
Nested child IDs never overwrite the parent's resumable session.

Provider-native children remain visible after daemon restart by persisting
their descriptors and last source cursor. If an adapter can deterministically
enumerate and replay child history, Tusker may rehydrate from the provider;
otherwise synchronous persistence is required.

## Truthful lifecycle

One overloaded status cannot describe task state, process reality, provider
turn state, and child activity. Tusker exposes separate dimensions:

| Dimension | Examples |
|---|---|
| Delivery | unbound, bound, proof-ineligible, review, landed |
| Admission | registered, claimed, start requested, starting |
| Process | absent, wrapper alive, child alive, stopping, dead, unknown |
| Provider | unknown, starting, running, interrupt requested, acknowledged, terminal |
| Outcome | none, succeeded, failed, canceled, interrupted, lost, abandoned |
| Session | attached, resumable detached, non-resumable closed |
| Children | active, failed, needs attention |

A concise phase may be derived for the UI, but the evidence dimensions remain
inspectable.

Cancellation records provider acknowledgment and operating-system settlement
separately. A provider acknowledgment does not prove a local process is dead,
and a dead process without provider acknowledgment is not reported as the same
outcome. Existing owner/generation and PID/PGID/start-time fencing remains
authoritative.

## Timeline contract

Live notifications make the UI quick; authoritative fetch makes it correct.

Each durable source timeline has an epoch and monotonic sequence. Fetch
supports tail, before, and after cursors and returns explicit `reset`, `gap`,
`stale cursor`, `has older`, and `has newer` facts. A client advances its
checkpoint only from authoritative fetch results and keeps fetching until it
can prove it reached the committed tail.

A wave or root-execution timeline may project rows from several source
executions, but every projected row retains its source execution, source epoch,
and source sequence. Restart, subscriber eviction, retention, or log
replacement must never create a silent permanent hole.

## Operator experience

The main view is a tree or track:

```text
W-0042 · Runtime hardening
├── ORC-T-0014 · Lease recovery audit
│   └── execute · Codex
│       ├── explorer · Map lease paths        provider-owned
│       └── reviewer · Check PID reuse        provider-owned
└── ORC-T-0015 · Cancellation evidence
    └── execute · Claude                      Tusker-managed
```

The operator can:

- search by execution name, ticket, wave, provider ID, or agent type;
- open parent, child, task, and wave links;
- distinguish retry/resume from concurrent child work;
- see whether a child is Tusker-controlled or provider-owned;
- inspect a convergent timeline;
- see active/failed/attention child counts;
- rename or attach allowed direct work;
- find all unbound direct work in one inbox.

Controls are capability-aware. Tusker never shows an independent Stop button
for a provider-native child unless the adapter proves that independent control
exists.

## Program contract

The implementation should converge on two normalized concepts rather than
adding more meanings to the existing attempt parent field.

### Execution record

The durable record contains:

- Tusker execution ID and root execution ID;
- node kind (`root`, `managed_attempt`, `provider_child`);
- display name and normalized search label;
- task, wave, attempt, session, and provider references where applicable;
- source and provider;
- reusable agent type and provider child handle;
- lifecycle dimensions and capability flags;
- creator identity, lease generation where applicable, timestamps, and last
  authoritative cursor.

### Typed relationship

Relationships include:

- `retry_of`;
- `resume_of`;
- `fork_of`;
- `managed_child_of`;
- `provider_child_of`.

Managed-child relation creation is transactional with independently owned
execution intent for a distinct ready or rework task. A parent worker may
submit that intent, but only the resident daemon admits and launches it.
Provider-child upsert is idempotent on parent execution, provider, and provider
child handle. Retry/resume edges never imply concurrent ownership.

Existing attempt relationship columns remain readable during migration. A
single migration/backfill owner decides whether they remain compatibility
projections or are removed only after every consumer uses the normalized
model.

## API and CLI contract

The planned surfaces are:

- list and inspect executions with filters for task, wave, parent, source,
  provider, binding, and lifecycle;
- register a direct execution and receive its immutable ID before provider
  launch;
- attach/detach or rename through audited conflict-checked operations;
- append provider observations through an internal typed adapter boundary;
- fetch relationship-complete execution graphs;
- fetch authoritative cursor timelines;
- expose the same read model to CLI, Serve, TuskerBar, streams, and logbook.

Exact command spelling and wire schemas are finalized in the first contract
slice and then treated as compatibility surfaces.

## Safety invariants

1. SQLite remains the sole execution-ownership authority.
2. Existing lease owner/generation and process-identity fences are not weakened.
3. Provider observations cannot claim a task, bind an execution, satisfy proof,
   finalize an outcome, arm a wave, or move a ref.
4. Managed children have independent ownership; provider children are marked
   provider-owned unless control capability is proven.
5. A direct unbound execution cannot influence delivery state.
6. Parent termination does not forge child termination.
7. Daemon restart does not reap wrapper-owned core work.
8. Timeline notification loss is repaired through authoritative fetch.
9. Every migration is restart-safe, idempotent, and covered by backfill and
   rollback/compatibility tests.

## Delivery shape

Implementation proceeds in dependency order:

1. identity, graph, storage, and compatibility contract;
2. direct registration and binding;
3. provider event ingestion and provider adapters;
4. relationship-complete CLI/API and cursor timelines;
5. lifecycle/cancellation evidence;
6. multi-agent UI and unbound inbox;
7. crash, restart, replay, migration, and cross-provider dogfood;
8. canonical system documentation and rollout checks.

The first slices expose truth without changing who may execute work. Runtime
authority changes, where unavoidable, land only after the read/projection
model and failure tests exist.

## Deferred (not now)

- Replacing SQLite with provider state, JSON files, or an in-memory graph.
- Allowing dispatched workers to recursively launch Tusker runners.
- Cross-agent chat or peer-to-peer agent messaging.
- Automatically turning every provider-native child into a task.
- Independent control of provider-native children without a proved provider
  capability.
- Making remote/cloud provider status equivalent to local process evidence.
- Supporting providers beyond Codex local/app/cloud and Claude Code in the
  first delivery.
- Productizing the execution graph for unrelated teams.
