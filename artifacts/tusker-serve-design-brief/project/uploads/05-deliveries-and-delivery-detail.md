---
schema: tusker.design-note/v1
kind: interface
status: proposed
authority: normative
date: 2026-07-28
parent: "[[00-index]]"
related:
  - "[[03-shell-and-today]]"
  - "[[04-plan-and-authorization]]"
  - "[[08-daemon-diagnostics-and-recovery]]"
  - "[[10-guardrails-authority-and-confirmations]]"
tags:
  - tusker/ux
  - tusker/delivery
---

# Deliveries and delivery detail

## Delivery list

### Default groups

1. **Active**
2. **Needs attention** — only when non-empty
3. **Delivered**

Filters are hidden behind Filter and persist per project:

- time range;
- phase/state;
- plan/spec;
- promotion/release outcome;
- contains human override;
- technical ID.

### Delivery row

- outcome/title;
- current phase or delivered time;
- concise progress;
- next automatic action;
- attention reason if any;
- best artifact thumbnail for completed work;
- source plan/spec secondary.

A delivery is represented once. Do not render individual attempts in this
list.

## Delivery detail

The page is an artifact-first release brief while work is active and after it
finishes.

### Header

- outcome/title;
- state and one-sentence explanation;
- delivery phase strip;
- primary contextual action:
  - Resolve decision
  - View repair
  - Pause
  - Resume
  - View result
- overflow: technical details, audit receipt, copy link.

### Group 1: Outcome

Show:

- intended outcome;
- requirement completion summary;
- material exclusions;
- current consequence:
  - “Tusker is checking all changes together.”
  - “Delivered to main yesterday at 02:14.”
  - “Release failed; main is green and production is unchanged.”

### Group 2: See it

As artifacts become available:

- screenshots/recordings;
- request-response examples;
- benchmark deltas;
- traces/timelines;
- reliability/security summaries;
- rendered documentation.

Group artifacts by requirement, not by task. An unavailable artifact says when
it is expected; it does not render a blank card.

### Group 3: Progress or result

During work:

- requirements verified / total;
- current workstreams in product language;
- review/rework summary;
- next frontier;
- automatic repair and bounded retry, when active.

After work:

- landed outcomes;
- integration/full-gate result;
- promotion/release result;
- documentation/knowledge changed;
- unresolved follow-up, if any.

### Secondary disclosures

- Requirements
- Task DAG
- Proof and review
- Integration and promotion
- Audit and exact details

## Delivery phase behavior

| Phase | Default explanation | Detail |
|---|---|---|
| Starting | Preparing isolated work and validating runners | Preflight results |
| Building | N outcomes are being implemented | Active task outcomes |
| Checking | Independent review and focused proof are running | Review verdicts |
| Integrating | Changes are being validated together | Candidate and gate summary |
| Scheduled | Full-green candidate waits for configured window | Window and hold policy |
| Promoting | Exact validated candidate is moving to default branch | Drift revalidation |
| Releasing | Named release profile is executing | Release target and receipt |
| Repairing | Tusker isolated a failure and is retrying/reworking | Failure classification |
| Waiting | A named human action blocks an affected branch | Action and closure |
| Paused | Operator paused new work | Existing safe work behavior |
| Delivered | Required outcome reached its policy boundary | Artifacts and revisions |

## Task detail

Task detail is reached from the DAG or requirement, not primary navigation.

Order:

1. Task outcome and why it exists.
2. Requirement(s) implemented.
3. Current state and next action.
4. Acceptance and proof coverage.
5. Artifact.
6. Independent review result/findings.
7. Dependencies and affected closure.
8. Technical: task ID, state revision, workspace, branch, attempts, commands,
   evidence rows, receipts.

Actions are contextual and policy-backed:

- Request rework
- Retry infrastructure failure
- Pause this work
- Discard task with dependency preview
- Human override, if authorized

Never offer direct “set done.”

## Proof and artifact viewer

### Default

Show an acceptance matrix:

| Outcome | Proof | Result | Artifact |
|---|---|---|---|
| Login resumes after restart | Focused test + reviewer | Verified | Timeline |

### Technical disclosure

- exact command;
- source/candidate/toolchain fingerprint;
- timestamp and actor;
- bounded stdout/stderr excerpt;
- full artifact link;
- evidence record ID.

No raw log is loaded until requested. Large outputs are bounded and downloadable.

## Review and rework

A review result is typed:

- Pass
- Changes requested
- Infrastructure failure
- Invalid/incomplete result

For changes requested:

- group findings by acceptance outcome and severity;
- state automatic next action;
- show iteration count and retry budget;
- continue independent DAG branches.

The PM is not asked to approve a passing objective review.

## Failure and repair detail

### Classification

- infrastructure;
- known flaky check;
- isolated task defect;
- ambiguous integration failure;
- external/human boundary;
- policy/authority refusal.

### Default view

- what failed;
- what Tusker concluded;
- what it is doing next;
- affected outcomes;
- retry/stall deadline;
- whether the user must act.

### Red promotion packet

Technical detail includes exact candidate, tree, gate commands, environment,
last green, bisection result, task/path set, reproduction, runtime signals, and
bounded defects. Optional model triage is visible as an explicit configured
step, never as magic.

## Promotion and release

Promotion and release are separate timeline events.

### Promotion

- current policy: disabled, shadow, stage, promote;
- candidate readiness;
- scheduled window;
- holds;
- full-gate result;
- expected/current default revision;
- drift refusal/recompute.

### Release

- named profile;
- exact promoted revision;
- target/environment;
- authority receipt;
- result and rollback/compensation status.

Never display “Delivered” ambiguously. Qualify:

- implemented;
- integrated;
- promoted to main;
- released to production.

The plan’s configured terminal boundary determines which one closes the
delivery.

## Singleton delivery

A lone accepted task is automatically represented as a one-outcome delivery
when scheduled staging/promotion policy applies. The UI says:

- Staged
- Scheduled for promotion
- Delivered

It never asks the user to create or manage a one-task wave.

## Controls and confirmations

| Action | Confirmation |
|---|---|
| Pause new work | One click with optional reason; reversible |
| Resume | One click after fresh health readback |
| Retry infrastructure | One click if same exact attempt policy permits |
| Request rework | Short reason; creates typed revision |
| Discard | Dry-run dependency impact, typed title only for large cascade |
| Promote now | Only when policy permits; show exact candidate and full-green proof |
| Release | Separate named authority confirmation |
| Human override | Dedicated high-friction flow and permanent receipt |

## Edge states

- A crashed/replayed completion shows one durable receipt.
- A stale browser projection revalidates before mutation.
- If the default branch moved, promotion returns to checking; it never pushes
  optimistically.
- A task can be infrastructure-blocked while the delivery continues on
  independent branches.
- An overdue intentional wait becomes a typed escalation, not healthy waiting.
- An acknowledged failed run leaves history but clears user attention.
- A paused delivery distinguishes new dispatch pause from live work cleanup.

## Acceptance

- The default detail is useful without task, run, or Git knowledge.
- Artifacts and acceptance precede logs.
- Failure state always names automatic next action or exact human remedy.
- Promotion and release authority are visibly separate.
- Technical evidence remains reachable and auditable.
- Healthy automation is quiet.

See [[08-daemon-diagnostics-and-recovery]] for machine/runtime views and
[[10-guardrails-authority-and-confirmations]] for mutation policy.
