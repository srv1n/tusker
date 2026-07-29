---
schema: tusker.design-note/v1
kind: interface
status: proposed
authority: normative
date: 2026-07-28
parent: "[[00-index]]"
related:
  - "[[01-product-model-and-information-architecture]]"
  - "[[05-deliveries-and-delivery-detail]]"
  - "[[07-settings-and-runner-policy]]"
  - "[[10-guardrails-authority-and-confirmations]]"
tags:
  - tusker/ux
  - tusker/planning
  - tusker/authorization
---

# Plan and authorization

## Outcome

A PM can discover, understand, and authorize a proposed delivery without
pasting a YAML path, copying a fingerprint, learning wave mechanics, or
approving routine engineering operations.

The system still binds authorization to the exact plan identity, spec revision,
task set, and policy. The backend performs that ceremony; the UI communicates
its consequence.

## Plan sources

A plan may come from:

- a planning agent using the installed Tusker skill;
- `tusker delivery plan`;
- an imported reviewed plan;
- a future structured requirements composer;
- a repaired/revised version of a previous plan.

Tusker indexes valid plans from approved project locations and returns a plan
inbox. The user never types a filesystem path in the default interface.

## Plan inbox

### Default groups

1. **Ready for review**
2. **Changed since review**
3. **Drafts** — collapsed
4. **Started and archived** — link to history, not a visible group

Each row shows:

- plan title and intended outcome;
- source spec;
- requirement count;
- estimated parallel shape: e.g. “7 tasks, up to 3 at once”;
- affected product areas;
- validation state;
- author/source and last changed time;
- status.

Primary action: **Review plan**.

### Invalid plans

Invalid plans appear as one compact “2 drafts need repair” item. Detail groups
defects by:

- unresolved spec/decision;
- cycle or dangling dependency;
- placeholder acceptance/proof;
- missing artifact contract;
- unsupported installed capability;
- unsafe policy conflict.

The PM sees “Ask planner to repair” or “Open source.” Exact schema defects are
technical detail.

## Create plan

The first version may be a launcher rather than an in-app spec composer:

1. choose a canonical spec or enter the outcome;
2. choose optional constraints: target date, maximum concurrency, excluded
   areas, required human acceptance;
3. choose “Draft with planning agent”;
4. show progress and deposit the result in the Plan inbox.

Creating a plan does not import tasks, enable automation, or authorize work.

## Plan review screen

### Header

- title;
- status: Ready to start / Needs repair / Changed since review / Already
  started;
- source spec and changed time;
- primary action: **Start delivery** only when valid and current.

### Visible group 1: What will be delivered

An editorial requirement list. Each requirement shows:

- outcome statement;
- why it matters or relevant decision;
- observable acceptance;
- best artifact the user will receive.

Requirements are numbered for conversation, not tied to task IDs.

### Visible group 2: How Tusker will prove it

Summarize proof by outcome:

- focused checks during implementation;
- independent review policy;
- integration/full gate;
- artifact types;
- subjective human acceptance, if explicitly required.

Commands remain behind “Exact verification.”

### Visible group 3: How work will flow

Show a concise orchestration summary:

- “7 tasks”
- “up to 3 may run at once”
- “2 independent branches”
- “1 final integration check”
- “No human checkpoints expected” or the exact planned human boundaries.

Primary disclosure: **View task DAG**.

### Not included

A short, high-signal exclusions block. Do not print a paragraph of every
untouched subsystem. Show the material boundaries that prevent expectation
drift:

- no release;
- no production data mutation;
- no change to model policy;
- no scheduled promotion;
- named excluded product scope.

## Requirement detail

Opens inline or in a side sheet:

- outcome and rationale;
- acceptance criteria;
- proof and artifact contract;
- linked decisions/knowledge;
- tasks implementing it;
- risk and human authority boundaries;
- exact commands and file ownership in Technical.

## Task DAG view

The DAG is a drill-down, not the plan’s primary representation.

### Layout

- graph canvas on the left/top;
- synchronized task list on the right/bottom;
- group by outcome/workstream;
- show dependencies as directed edges;
- indicate runnable frontier, integration node, and human gate;
- collapse completed or purely mechanical groups.

### Node content

- short task outcome;
- state;
- role/difficulty summary;
- owned area;
- proof readiness;
- task ID only in secondary text.

### Controls

- filter by requirement, state, risk, or owner role;
- highlight dependency closure;
- explain why a node is not runnable;
- no drag-to-reorder for a dependency graph;
- editing task/dependency structure routes through an explicit plan revision,
  invalidating stale review.

## Start delivery

### Preconditions

The UI requests a fresh preflight. Start is enabled only when:

- plan/schema is valid;
- source spec and decisions resolve;
- dependency graph is acyclic;
- every task has acceptance, proof, and artifact contract;
- installed capabilities satisfy the plan;
- configured runner roles are resolvable and healthy;
- workspaces/branches are safe;
- project is registered;
- any expected human gates are visible;
- exact plan identity is unchanged.

Automation may still be off. The authorization sheet must distinguish:

- “Start and let Tusker run now” when project automation is enabled;
- “Authorize this delivery; it will wait until automation is enabled” when off.

Do not silently enable automation.

### Authorization sheet

Title: **Start this exact delivery?**

Show:

- outcome and requirement count;
- what Tusker may do: create isolated workspaces, run configured agents, record
  proof, independently review, integrate through the configured lane;
- what it may not do: expand scope, release, enable promotion, change model
  permissions, exceed limits;
- effective model roles and concurrency;
- expected human gates;
- promotion/release policy separately;
- material risk callouts.

The user confirms with one button: **Start delivery**.

The backend sends the reviewed `planIdentity` and fingerprint it already holds.
The user does not retype either. Stale identity returns the changed plan diff
and requires review again.

### Higher-risk confirmation

Use additional confirmation only when the plan includes an authority boundary
that is itself being granted, such as production mutation or release. Show the
specific authority and duration. Never use “type this SHA.”

## After start

The UI transitions to:

> Delivery started. Tusker will begin with 2 independent tasks.

Actions:

- **View delivery**
- optional **Pause**

The plan moves to history. A durable receipt retains actor, time, plan identity,
task-set fingerprint, and policy snapshot in technical detail.

## Changed-plan behavior

If a source spec, requirement, task set, dependency, proof contract, or material
policy changes:

- authorization becomes stale;
- no newly introduced work is dispatched;
- already safe in-flight work follows explicit backend policy;
- Today shows “Plan changed; review the update”;
- review displays a semantic diff:
  - outcomes added/removed/changed;
  - proof changed;
  - dependency/concurrency changed;
  - new authority or human gate;
  - technical identity change.

## Plan history

Show:

- version/revision timeline;
- review/start actor and time;
- semantic changes;
- delivery link;
- superseded/refused/invalid reason;
- exact receipts under Technical.

## Error and edge states

| Condition | UX |
|---|---|
| No plan files | Explain how plans are generated; Create plan |
| Plan source deleted | Historical metadata remains; source unavailable |
| Duplicate plan identity | One canonical row; duplicates listed in Diagnostics |
| Plan already started | View delivery; no duplicate start |
| Start request replayed | Return the existing delivery receipt |
| Plan changed during confirmation | Refuse safely; show semantic diff |
| Automation off | Authorization may be recorded, but clearly waits |
| Runner unhealthy | Start blocked with named runner remedy |
| Daemon offline | Start may import/authorize only if backend contract permits; never claim work is running |
| Partial import failure | Atomic refusal; no half-created delivery |

## Acceptance

- A valid plan is selectable without typing a path.
- No hash, fingerprint, YAML, or CLI command appears in the default review.
- The PM can explain requirements, proof, exclusions, concurrency, and human
  boundaries before starting.
- One start action authorizes only the exact reviewed boundary.
- Automation, promotion, release, and paid triage are never implicitly enabled.
- A wave of one behaves exactly like any other delivery without wave ceremony.

See [[10-guardrails-authority-and-confirmations]] for the durable authority
receipt and [[05-deliveries-and-delivery-detail]] for the post-start experience.
