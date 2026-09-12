---
subject: agent-coordination
title: "Agent contacts, clarification and autonomous wave continuation"
keywords: [architect, messaging, threads, sessions, parent, sibling, clarification, wakeup, autonomy, tokens]
part_of: execution-observability
describes: [cmd/tusker, internal/runner, internal/v7schema, internal/serve/ui]
status: canonical
created: 2026-09-10
read_when: "Implementing durable agent contacts, messages, targeted wakeups or architect continuation across waves."
skip_when: "Looking up already shipped execution controls; use the current execution and orchestration system documents."
sources:
  - .tusker/specs/decisions/2026-09-10-agent-coordination-grill.md
  - .tusker/specs/execution-observability.md
  - .tusker/specs/planning-handoff-and-agent-entry.md
  - .tusker/specs/runner-execution-boundary.md
  - .tusker/specs/completion-and-integrated-acceptance.md
updates:
  - docs/system/execution-observability.md
  - docs/system/orchestration.md
  - docs/system/runners-and-acp.md
  - docs/system/delivery-and-waves.md
  - docs/system/cli.md
  - docs/system/serve-ui.md
decisions_locked: false
capsule:
  what: "Durable architect and peer contacts, correlated messages and event-driven continuation."
  use_when: "Building clarification, agent wakeups and successive waves without manual relaying."
  skip_when: "Looking up existing execution controls or treating this specification as live-provider proof."
---

# Agent coordination

## Why

The operator currently copies questions, results and follow-up instructions between agent conversations. Using an expensive agent to poll workers or relay those messages adds cost without adding judgment. Tusker must perform that mechanical coordination in code, calling an agent only when there is an actionable question, result or instruction.

The architect remains a persistent design partner across waves. Workers can ask it questions during a wave, contact a relevant sibling, and receive replies after yielding or resuming. The architect can evaluate a completed or blocked wave and propose the next work. The current pause on unattended execution is a temporary rollout setting; it does not defer building these mechanisms.

This is an implementation specification, not a claim of shipped behavior. Product requirements below are confirmed from the September 10 discussion. Field names and decomposition are implementation recommendations, subject to bounded source qualification; do not represent them as separately approved operator decisions.

## Required customer journeys

1. An architect proposes tasks one through five. Task four encounters an ambiguity and asks the architect through Tusker. It saves its progress and yields when the answer is required. The other independent tasks continue. Tusker delivers the question once to an available architect turn; the reply is tied to the question and reaches the current owner of task four.
2. Task four needs a contract detail from task two. It can address task two's owner using its durable contact. The message does not create a scheduling dependency or let either worker edit the other's owned files. An actual prerequisite still belongs in the task dependency graph.
3. Four tasks finish and the remaining task is blocked. Tusker reports that exact state to the architect; it does not call the wave successful or wait forever for all tasks to succeed. A stalled wave and a successful wave are different inputs to the same continuation mechanism.
4. A wave passes its required verification, review and landing. Tusker sends its result and outstanding requirements to the original architect conversation. The architect proposes a subsequent wave, a repair, an explicit user decision or completion of the objective. Tusker validates and applies the proposal through the existing delivery machinery.
5. A daemon restarts, a worker retries or an architect session is replaced. The task retains whom to ask; messages and replies remain linked; a message accepted before the crash is not lost and a repeated notification does not create a duplicate wave.
6. The operator can register an existing external conversation or have Tusker start one through a supported installed harness. Unsupported control is visible. The same contact and message model supports both without promising access to every application's private session store.

## Requirements

| ID | Required result |
| --- | --- |
| C1 | Task contracts and packets retain architect, origin and useful peer contacts across attempts, with stable typed identities and visible inheritance. |
| C2 | Messages and correlated replies are durable, attributable, bounded and inspectable through CLI and UI. |
| C3 | A blocked worker can ask an architect or peer, yield without polling, and continue from an applicable reply; independent tasks remain runnable. |
| C4 | Completed and stalled waves trigger distinct evidence-backed architect reports and validated subsequent work. |
| C5 | Scheduling, routing, batching, retries and status aggregation use code and zero model turns. Only substantive recipient work wakes a model. |
| C6 | Each installed harness and endpoint exposes qualified capabilities; external attachment, idle resume, active messaging and reply capture are tested separately. |
| C7 | Restarts, duplicate notifications, uncertain delivery, busy recipients, session replacement and stale replies cannot silently lose work or apply it twice. |
| C8 | The existing autonomy pause does not constrain the implemented feature set; configuration controls wakeups and continuation without bypassing execution ownership or acceptance. |
| C9 | The operator sees relationships, conversations, delivery state, wait reasons and controls without becoming the courier. |
| C10 | Scope, sender identity, permissions, current revisions and ownership are validated at every message or lifecycle mutation. |
| C11 | Qualification proves both coordination behavior and model-call counts, separating offline fixtures, installed protocol probes and live model evidence. |

## Existing foundations and gaps

Source inspected September 10, with a dirty checkout preserved:

- ExecutionRecord and ExecutionEdge in cmd/tusker/execution_ledger.go already hold immutable identities, parent/root relationships, task bindings and native session correlation. Reuse them.
- Execution registration and attachments already support direct work. The existing execution inbox means unbound executions; it is not a message inbox.
- Runtime sessions and resolveResumeSession in cmd/tusker/daemon.go support same-task continuity. Their project, record, revision and workspace checks remain valid for implementation attempts; do not weaken them to create a cross-wave architect.
- internal/runner contains installed-harness admission, structured events, session receipts and resume argument compilation. Existing CLI and ACP adapters are the starting point.
- Task dependencies, delivery validation/import, leases, independent review, proof and landing remain their current authorities.
- There is no task-level architect contact or durable routed message/reply contract in the inspected schema. Execution lineage is observation; it does not yet provide the desired conversation service.

## Identity and task contracts

Reuse a registered execution root as a durable conversation address. A task address resolves to the current legitimate owner of that task. Keep native session IDs, host, profile and transport in runtime endpoint bindings rather than copying transient provider IDs into every ticket.

Recommended optional authored coordination fields, finalized by the identity implementation:

    coordination:
      architect: {kind: execution, id: <registered-root-id>}
      origin: {kind: execution, id: <planning-conversation-id>}
      contacts:
        - role: contract-owner
          target: {kind: task, id: <task-id>}

Architect is a design role. Origin identifies the conversation that authored the work; it need not be the architect. Existing execution parentage identifies spawning, and existing dependencies identify required outputs. These relationships are distinct. If a parent task is needed, represent it explicitly rather than overloading origin or dependency.

The wave or objective may supply an inherited architect; a task override is explicit. Plan import resolves and validates contacts, stores their provenance and exposes the effective values in task packets. A new wave in the same objective inherits its architect only through that authored continuation context. Unrelated legacy tasks remain valid without coordination fields.

A provider child is addressable only when the adapter proves it has a separate reachable endpoint. Otherwise expose its recorded relationship and unsupported control; do not silently substitute the parent.

A conversation address can have successive endpoint bindings. Record replacement lineage and generation, preserve old messages and revalidate queued messages before sending to a successor. External sessions require a supported attach/ownership check; observing a session ID alone is not write authority. A task address selects a unique current owner; an execution address targets that exact conversation, including after task completion. Ambiguous ownership never selects the newest-looking session.

## Durable messages and replies

Use the existing runtime SQLite store, not a separate broker or always-running model. Persist each envelope before acknowledging it:

- Immutable message ID and caller idempotency key, project, sender and typed recipient.
- Origin task/wave/attempt/execution, applicable work/spec revision and route generation.
- Kind: question, answer, instruction, wave result or notice. Plain-language body plus bounded artifact/spec references.
- Correlation/reply-to ID, whether a reply is required, whether the sender must yield, creation time and optional expiry.
- Separate transport receipt, recipient consumption, answer and application facts. Accepted by a transport is not read, answered or applied.

Initial public surface should follow existing CLI and Serve conventions: send/ask, list/show, reply and resolve recipient. Exact verbs and endpoint paths are finalized in the contract task and exposed through capabilities. The worker packet carries a self-contained command example, effective contacts and outstanding question IDs. Use structured arguments or body files; never shell-interpolate message contents.

Duplicate send requests return the existing message. Reply correlation remains intact through retries. Mutating a task, applying a plan or selecting a new wave uses a stable deduplication key and revision check. Retrying a transport with an uncertain outcome requires provider reconciliation first. If the provider cannot prove delivery or support deduplication, expose delivery-unknown and retain the payload rather than claiming exactly-once delivery or blindly resending it.

The receiving agent can reply through the same durable surface. Supported structured outputs may carry replies too; validate their correlation and sender identity. Do not parse arbitrary prose for lifecycle commands, route messages by guessed names, or ask a model to retype an envelope.

## Wakeups and waiting

Tusker's resident daemon owns wakeup decisions and process launches. A CLI/API message write is cheap and durable. It does not need an intermediary model.

| Input | Deterministic behavior |
| --- | --- |
| Blocking clarification | Queue a prompt wakeup for its recipient; do not defer it to the end of the wave. |
| Non-blocking notice | Store for the next relevant turn unless explicitly requested as actionable. |
| Several pending questions | Coalesce available questions into one recipient turn while preserving individual IDs and replies. Blocking questions bypass any batching delay. |
| Recipient already working | Use verified safe active delivery when available; otherwise queue for the next safe turn. Never create a concurrent writer to the same conversation. |
| Answer arrives | Recheck the waiting task, question, revision and endpoint; schedule continuation if still applicable. |
| All current runnable work stops | Emit one stalled-wave report for the changed material state, including the blockers and pending questions. |
| Required wave acceptance passes | Emit one completed-wave report bound to accepted material. |
| Unchanged state or repeated notification | Update observation without another architect turn. |

A waiting worker yields at a safe checkpoint. Persist the question and wait condition before settling its turn, preserve its workspace and progress, and release the model execution slot only when the process is safely settled. Keep required workspace/resource ownership without holding unrelated capacity. Resolve the same question in a later attempt rather than starting a retry storm or blocking a tool call indefinitely.

Guard reply-before-yield, stop-during-send and restart-between-delivery-and-receipt races. If the reply is already present at yield, proceed without parking. A late answer to changed work is visible for reconsideration, not silently applied. Resume may include updated evidence under a new authorized attempt; existing same-task revision checks must not be disabled.

A parked worker must not consume all capacity needed to wake its architect. Detect cycles among explicit required-reply waits and surface a coordination deadlock; do not burn turns on repeated reciprocal messages. Use bounded infrastructure retries and recorded timeouts. A genuine missing user decision escalates once with the question and context.

## Architect continuation and autonomy

The architect receives a concise deterministic report: intended outcome, accepted results/revisions, verification and review, failed or waiting tasks, unresolved questions, spec changes and remaining requirements. Attach links and machine-generated facts rather than full worker transcripts. The architect can read deeper evidence when its judgment needs it.

Its structured result is one of answer, next-wave proposal, repair proposal, needs-user or objective-complete proposal. Existing delivery doctor/import and task/review/landing rules validate mutations. A response saying complete cannot manufacture acceptance or mark a blocked wave successful.

Persist the continuation trigger, architect turn, resulting proposal and applied wave mapping. Bind the proposal to the objective, triggering report and current context; a repeated response cannot import or dispatch a second copy. If material changes before application, report the conflict with a bounded validation delta rather than blindly executing a stale plan.

Implement automatic successive-wave continuation now. Use existing settings machinery to represent whether messages may wake agents and whether valid proposed waves may start automatically. The operator's current paused configuration is preserved during development; it is not hard-coded into the feature or a prerequisite that postpones this work. In paused mode messages/proposals remain durable and visible; enabling the configured mode resumes them without reauthoring tickets.

Continuation operates within an explicitly configured objective scope and run/spend limits, plus existing project/workspace permissions. Planning role authority covers answers and proposals, not arbitrary code changes or bypasses of required review. A user stop prevents new turns and new wave dispatch; retain pending work with its stop reason. The role uses the configured demanding architect profile, independently of cheaper workers; transport and status code never choose a model to perform bookkeeping.

## Harness capabilities and current source research

Track capabilities per exact installed harness/version/profile/host/endpoint and their evidence: supported, unsupported or unknown; last checked; documented/probed/live-qualified. Distinguish starting a session, attaching an external session, resuming idle, delivering to an active turn, observing turn state, capturing a reply and reconciling uncertain delivery. A generic resume=true flag is insufficient.

| Route | Primary-source finding on September 10 | Tusker consequence |
| --- | --- | --- |
| Codex app server | Official lifecycle documents thread start/resume, turn start, active steering and completion events. | Candidate for active delivery and external attachment, subject to installed conformance and ownership checks; current CLI route stays explicit. |
| Codex CLI | Current Tusker source compiles explicit-session resume and reads structured session receipts. | Qualify that installed route independently for idle wakeups. Do not assume active injection or access to an app-owned session. |
| Muse profile | Current Tusker routes Muse through a configured Codex CLI profile. | Probe the actual profile and account context; record observed capabilities, with no fabricated native Muse API parity. |
| Claude Code | Official documentation describes explicit-session resume and native cross-session messaging. Native messages can reach an active turn or wake an idle recipient; one-shot idle notifications also exist. | Native in-agent tools do not by themselves establish a direct API Tusker can call without an intermediary model. Qualify the accessible CLI/SDK/transport separately, including held/refused delivery. |
| ACP | Session loading and resume are negotiated capabilities; prompt sending is session-scoped. | Use only capabilities reported by the actual installed endpoint and verified through conformance. |

Primary sources: [Codex lifecycle](https://learn.chatgpt.com/docs/app-server#lifecycle-overview), [Claude programmatic sessions](https://code.claude.com/docs/en/headless#continue-conversations), [Claude cross-session messaging](https://code.claude.com/docs/en/cross-session-messaging), and [ACP session setup](https://agentclientprotocol.com/protocol/v1/session-setup).

These are documentation and source findings, not live installed-harness certification. V1 must support generic contacts and unsupported states while adding verified transports incrementally. External attachment is part of the model; broad multi-host forwarding and every provider's private UI protocol are not prerequisites. Never silently create a fresh conversation when the requested same-session continuation is unavailable.

## Operator and agent experience

Task details show architect, origin, relevant contacts, pending question, last answer and links to the associated execution conversations. A sibling contact and a scheduling dependency are visually distinct. The message timeline shows queued, held/unsupported, delivered, consumed, answered and applied facts only when their corresponding evidence exists. Expose unknown delivery explicitly.

The operator can message an agent, see why it is waiting, supply an answer, change a contact with audited revision checks, and pause/continue the objective. Reuse the task inspector, execution timeline, existing controls and SSE; do not build a separate chat product. The next architect turn must include user steering that arrived while the wave was running. Workers see the same durable facts through compact packets and CLI reads.

## Boundary checks

Validate sender identity from the authenticated/claimed invocation, not message text. Resolve recipients within the bound project; cross-project messaging requires an explicit authorized relationship. Verify artifact paths and sizes. Incoming text and attachments are context, not authority to override task scope, permissions or review requirements. A sibling answer cannot grant file ownership or make a failed check pass.

Do not expose credentials in endpoint bindings or reports. No new bundled agent runtime, hidden transport fallback, automatic model substitution or generic workflow language is required.

## Acceptance and delivery slices

| Slice | Scope | Depends on | Proof |
| --- | --- | --- | --- |
| Identity | Contracts, typed contacts, endpoint generations, CLI/packet projection | None | Restart/retry/attachment/ambiguous-owner tests and contract examples |
| Mailbox | Durable send/ask/reply, receipt states and revision-aware application | Identity | Deduplication, races, stale replies and failure matrix |
| Transports | Capability contract, direct adapters and installed route qualification | Identity | Protocol tests and route-by-route evidence; no relay model |
| Clarification | Event-driven wakeups, safe yield and targeted continuation | Mailbox, transports | Task four asks; other tasks progress; reply resumes correct work; idle poll model-call count zero |
| Wave continuation | Completed/stalled reports, architect outputs, idempotent next waves and rollout settings | Clarification | Two waves, partial failure, restart and stop scenarios with wake reason/call ledger |
| UI | Contacts and message controls in existing task/execution surfaces | Mailbox | Mocked contract browser proof; integrate live with clarification and wave slices |
| Qualification | Integrated scenario, live supported harnesses and current system docs | Wave continuation, UI | Before/after UI images, exact commands, model-call counts and evidence boundaries |

The implementation streams can progress alongside the operator's current execution testing. Dependency edges and owned files govern concurrency. Changes to shared schema/CLI/daemon integration land in the order above; provider internals and UI can progress independently once the contract is available.

Required integrated scenario: five tasks, task four asks its architect, one peer question is answered, independent tasks finish, a blocked-wave report remains truthful, the reply resumes task four, wave acceptance wakes the architect, the next wave is created once, and a restart at each delivery boundary loses nothing. Include busy recipients, unsupported transports, no available worker slot, duplicate notices, changed work, a reply arriving before yield, a user stop, a wait cycle and a missing session.

Count actual model turns by recipient and wake reason. Unchanged scheduler ticks, receipt retries and status projections must cause zero calls. A duplicate event must not add a call or a wave. Separate required clarification judgment, wave planning and implementation/review calls; do not claim a fixed total across models or savings without a measured baseline. Zero matched tests is a failed qualification. Live evidence must identify the installed version, profile and transport; offline tests alone cannot certify it.

## Deferred

- Universal peer discovery or all-to-all broadcasts; use explicit contacts and existing dependencies.
- A new message broker, resident orchestrator model or replacement scheduler.
- Remote host provisioning or native message APIs for every harness before the first supported route works.
- Automatic semantic routing, a classifier to choose whom to ask, or an extra model to summarize every status event.

## Continuation

Confirmed: remove manual relaying; task-to-architect and peer questions are required; preserve contact identity across continuations; build automatic wave continuation while rollout remains paused; use premium agents only for necessary judgment.

Open implementation facts: exact installed external-session attachment, active delivery and receipt reconciliation per harness. Resolve with source/protocol qualification, not another product permission question. Provider API availability does not block the generic identity/mailbox work.

<!-- tusker:delivery-import:54f0d0dc809d3614:begin -->

## Work streams

- `[[ACO-T-0004]]` implements delivery source `clarification`.
- `[[ACO-T-0001]]` implements delivery source `contacts`.
- `[[ACO-T-0006]]` implements delivery source `interface`.
- `[[ACO-T-0002]]` implements delivery source `mailbox`.
- `[[ACO-T-0007]]` implements delivery source `qualification`.
- `[[ACO-T-0003]]` implements delivery source `transports`.
- `[[ACO-T-0005]]` implements delivery source `waves`.

- `[[W-0017]]` is the imported delivery wave.

<!-- tusker:delivery-import:54f0d0dc809d3614:end -->
