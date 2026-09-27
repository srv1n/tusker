---
kind: proposal
subject: task-states
keywords: [status, state, run state, labels, needs you, blocked, linear]
part_of: real-testing-campaign
status: accepted
code_conformance: matches
last_verified: 2026-09-27
created: 2026-09-27
read_when: "Showing, computing or acting on where a task stands, in the UI, CLI or API."
skip_when: "Changing lease, attempt or retry internals that never reach a person."
updates: [serve-ui, tasks-and-proof, orchestration]
sources: [real-testing-campaign.md]
decisions_locked: true
---

# Task states

Every task shows exactly **one state**. People and programs read the same
state. Details go in a short **reason** under the state, never in more states.

## Why

Before this change, Tusker had five overlapping state vocabularies: task
status (8 values), readiness (10), lease state (8), attempt outcome (14) and
the operator state plus UI labels. One paused run showed "Waiting", "Queued"
and "Needs you" at the same time, and none said what to do.

## The states

The model follows Linear: a few states, each in one category.

| Category | State | Who acts next | Meaning | Example reasons |
| --- | --- | --- | --- | --- |
| Not started | **Backlog** | Architect | Written or being written. Not planned. | draft · not armed |
| Not started | **Planned** | Daemon | Will run on its own when its turn comes. | queued · waiting on ALP-T-0001 · no free slot |
| Active | **Working** | Worker agent | An agent is working on it now. | first attempt · rework · attempt 3 of 6 |
| Active | **Needs input** | Architect, then you | The worker asked a question and is waiting. | the question itself |
| Active | **Blocked** | You | It cannot continue without help. | crashed · outside problem · not allowed · paused by you |
| Active | **In review** | Reviewer agent | A reviewer is checking it, or it is landing. | reviewing · landing · merge conflict |
| Closed | **Done** | Nobody | Landed and closed. | |
| Closed | **Canceled** | Nobody | Dropped or superseded. | superseded by ALP-T-0009 |

### Blocked reasons

Blocked always carries one reason kind. Each kind names a fix.

| Reason kind | Meaning | Next action shown |
| --- | --- | --- |
| `crashed` | The harness failed on all 6 attempts. | Open the log, then Retry |
| `outside_problem` | Disk full, API down, login expired. | Fix the named problem, then Retry |
| `not_allowed` | A sandbox or permission denied a needed action. | Change the profile or the task, then Retry |
| `paused` | The owner pressed Stop. | Continue |

## Rules

1. One Go function computes the state from the task file and the daemon's
   records. The UI, CLI and API only display its result. No screen maps
   internal values on its own.
2. The state record has these fields: `state`, `category`, `next_actor`,
   `reason_code`, `reason` (one line for a person) and `next_action`.
   `tusker show --json`, `tusker next --json` and every Serve task response
   return them.
3. Leases, attempts, retry counts and runner outcomes stay internal. The run
   page shows them for debugging only.
4. There is no Failed state. A failure a person cannot act on is Blocked with
   reason `crashed`.
5. A wave shows the most urgent state among its tasks, in this order: Blocked,
   Needs input, Working, In review, Planned, Backlog, Done. Its reason counts
   the tasks: "2 working, 1 blocked".
6. Stored task statuses stay as they are in the Markdown files. The displayed
   state is computed, so no file migration is needed.

## Decisions (2026-09-27)

| Question | Decision |
| --- | --- |
| One state or task plus run state? | One state. |
| After the reviewer passes? | The daemon lands and closes by itself. The owner spot-checks at the wave boundary. In review shows `landing` or `merge conflict` while that happens. |
| Retries after a crash? | 5 retries, so 6 attempts, then Blocked `crashed`. |
| Who gets a worker's question first? | The architect session. It forwards to the owner only product or taste calls. |

## Dependencies

- Auto-land: done in Phase 2 (S4). A passing review from any harness lands
  and closes the task. In review shows `merge conflict` or `landing` when the
  landing needs the owner. `completion_reactor.mode: disabled` keeps landing
  manual.
- Architect-first questions need the mailbox to reach the architect session
  (campaign item V6). Until then, Needs input goes to the owner.

## Acceptance

| ID | Outcome | Proof |
| --- | --- | --- |
| S1 | One Go function returns the state record for every task, with table tests covering each state and each Blocked reason. | `go test ./cmd/tusker -run TestTaskState -count=1` |
| S2 | `tusker show --json` and `tusker next --json` include the state record. | same suite |
| S3 | Serve task, wave and inbox views show only the state label and reason from the record. No UI file maps lease, outcome or readiness values to labels. | `cd internal/serve/ui && bun test` plus a screenshot of a wave with a Needs input task |
| S4 | A paused worker question shows the question and a reply box on the task, not a disabled Queued button. | screenshot |
| S5 | Crash retries stop after 6 attempts and show Blocked `crashed`. | focused Go test |
