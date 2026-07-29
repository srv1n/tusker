---
schema: tusker.design-note/v1
kind: interface
status: proposed
authority: normative
date: 2026-07-28
parent: "[[00-index]]"
related:
  - "[[01-product-model-and-information-architecture]]"
  - "[[02-swiss-design-system-and-attention]]"
  - "[[05-deliveries-and-delivery-detail]]"
  - "[[08-daemon-diagnostics-and-recovery]]"
tags:
  - tusker/ux
  - tusker/today
---

# Shell and Today

## App shell

### Expanded desktop navigation

```text
┌──────────────────────┬──────────────────────────────────────────────┐
│ tusker               │ Project / page title                 Health │
│                      │                                              │
│ Today                │ Page content                                 │
│ Search               │                                              │
│                      │                                              │
│ PROJECTS             │                                              │
│ CarelessWhisperer    │                                              │
│ backend        • 1   │                                              │
│ tusker               │                                              │
│                      │                                              │
│ + Add project        │                                              │
│                      │                                              │
│ Settings             │                                              │
└──────────────────────┴──────────────────────────────────────────────┘
```

Project expansion shows Today, Plan, Deliveries, Knowledge. Settings and
Diagnostics live in the project overflow/menu, not the primary list.

### Shell header

- breadcrumb or project name;
- page title;
- optional one primary action;
- global health indicator;
- notification icon only when there are delivered notifications.

Remove persistent Refresh, Needs, Details, stream, host, and address controls.
Data refreshes from events with targeted polling fallback. Manual refresh
exists in the page overflow and on error.

## Global Today

### Purpose

Answer “where should I look?” across every registered project.

### Default layout

1. **Needs your attention** — omitted when empty.
2. **Working across projects** — one row per delivery, capped at six with “See
   all.”
3. **Recently delivered** — one row per delivery, capped at six.

When attention is empty, the page leads with:

> Nothing needs you. Three deliveries are moving across two projects.

### Attention item anatomy

- project;
- plain-language problem or decision;
- affected outcome;
- recommended action;
- age or deadline when meaningful;
- click opens exact project detail.

### Working item anatomy

- project and delivery title;
- current phase;
- compact progress: “4 of 7 outcomes verified”;
- current explanation: “Checking the work” or “Waiting for the 02:00 promotion”;
- no runner/attempt count by default.

### Delivered item anatomy

- outcome title;
- project;
- delivered time;
- best artifact preview;
- promotion/release result when applicable.

### Empty/global setup state

No projects:

- title: “Connect your first project”
- explanation: registration discovers Tusker safely and keeps automation off;
- primary: “Add project”
- secondary: “How project setup works”

## Project Today

### Purpose

Be the project’s release-manager briefing. This is the normal home screen.

### Header

- project name;
- one-sentence health/outcome summary;
- primary action chosen by state:
  - “Review plan” if a new valid plan exists;
  - “View delivery” if work is active;
  - “Create plan” if idle and no proposal exists;
- health indicator;
- overflow: Settings, Diagnostics, repository details.

### Default content

#### Needs attention

Only appears when non-empty. Group multiple consequences of one root cause into
one item. Examples:

- “Choose whether the onboarding flow may remove legacy accounts.”
- “Codex runner is unavailable; select the discovered executable.”
- “Release failed after promotion; production is unchanged.”
- “This delivery changed after your review; review the updated scope.”

Each item has one recommended action. Technical evidence is secondary.

#### Working now

One item per active delivery:

- outcome;
- current phase;
- requirements completed / total;
- automatic next action;
- estimated next event only if deterministic;
- pause action in detail, not on the summary card.

If no delivery is active:

> No delivery is running.

Then show the most relevant plan action, not an empty board.

#### Recently delivered

Show the last three deliveries:

- outcome;
- when;
- artifact thumbnail or proof summary;
- “View delivery.”

### Secondary insights

A single “More” section may expose:

- upcoming promotion;
- project capacity;
- latest knowledge change;
- delivery history.

It must not become a second Ops dashboard.

## Attention detail

Use a right side sheet on wide screens and full page on compact screens.

Order:

1. What needs a decision/action.
2. Why the factory stopped here.
3. What is affected.
4. Recommended action.
5. Alternatives and consequences.
6. Evidence.
7. Exact technical details.

For a genuine product choice, show structured options and allow a short reason.
For external action, show the exact action and a “I completed this” verification
path. For infrastructure, deep-link to the relevant Diagnostics repair.

## Notification center

Notifications are not a second inbox. They are delivery receipts for:

- promotion red;
- release failed;
- spending or paid-launch hold;
- optional explicitly configured human-decision reminders.

Each durable outcome produces at most one unread notification until its state
changes materially. Marking read does not acknowledge or resolve the underlying
problem.

## Search

Global command/search overlay:

- searches projects, plans, deliveries, requirements, tasks, and knowledge;
- ranks product objects first;
- displays object type and project;
- supports optional filters after typing;
- exact IDs still resolve;
- recent items appear before a query;
- commands are limited to safe navigation and clearly labeled actions.

Do not turn search into a hidden CLI.

## Add project

Three-step sheet:

1. **Choose repository** — native folder picker; validate Git and Tusker state.
2. **Review discovery** — project identity, default branch, available harnesses,
   detected existing configuration, migration needs.
3. **Finish setup** — project display name and whether to configure model roles
   now or later.

Registration never enables automation, promotion, release, or spending.
Existing configuration is preserved. A legacy/incompatible project gets a
non-destructive migration preview; reinitialize is never the first remedy.

## First-run onboarding

Teach three facts only:

1. Plans are reviewed once, then Tusker drains their dependency graph.
2. Automation is off until enabled for a project.
3. Tusker asks for humans only at explicit judgment or authority boundaries.

Runner/model setup is offered after project discovery, not before the product
has a repository context.

## State matrix

| State | Today behavior |
|---|---|
| Healthy and idle | Quiet summary plus Create/Review plan |
| Healthy and active | Working delivery leads |
| Human decision | Attention leads in red only if action is urgent; otherwise amber |
| Automatic repair | Working item says Repairing; no human attention unless bounded retries exhaust |
| Runner unavailable | Attention item plus one repair; other healthy deliveries remain visible |
| App runtime offline | Cached content remains; mutations disabled; reconnect action |
| Project automation off | Neutral badge in project header; not an error |
| Promotion off | Omitted from Today unless user opens delivery policy |
| Plan changed after review | Plan attention; previous authorization shown stale |

## Acceptance

- A user reaches every daily action from Today without opening Diagnostics.
- A healthy project has no red or amber decoration.
- Zero-value stat cards and empty workflow columns are absent.
- An active delivery is represented once regardless of task/run count.
- No path, SHA, PID, address, or YAML appears before technical disclosure.

See [[05-deliveries-and-delivery-detail]] for the detail reached from Today and
[[08-daemon-diagnostics-and-recovery]] for runtime repair.
