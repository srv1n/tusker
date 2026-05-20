# Documentation program design

This file is for whole documentation efforts: information architecture, inventories, roadmaps, backlogs, governance, and remediating existing docs.

## The blunt rule

Do not start by building a perfect four-folder tree. That looks organized and often accomplishes nothing. Start with one existing or needed page, classify its user need, improve it, then repeat. The structure should emerge from useful work.

## Program workflow

```text
1. Inventory what exists.
2. Classify each item by user need and mode.
3. Identify missing source-of-truth and stale-risk.
4. Fix the most damaging pages first.
5. Split mixed-mode pages where the split improves use.
6. Build navigation around repeated real needs.
7. Add ownership and release/update triggers.
8. Re-run audits after product changes.
```

## Inventory fields

Use these fields for every page, including agent-facing docs:

```yaml
id: "stable-page-id"
title: ""
url_or_path: ""
mode: tutorial | how-to | reference | explanation | mixed | unknown
primary_reader: ""
reader_competence: beginner | competent | expert | agent | mixed
reader_need: learning | goal | information | understanding
source_of_truth: "code | API schema | product owner | design doc | policy | unknown"
owner: ""
last_reviewed: "YYYY-MM-DD"
stale_when:
  - "API changes"
  - "CLI flags change"
  - "workflow changes"
quality_status: good | needs-update | wrong-mode | duplicate | missing | obsolete
agent_layer: none | capsule | standalone
notes: ""
```

## Minimum useful documentation set

For a new product, do not try to document everything. Create a small but coherent set:

```text
Home
├── Tutorial: first successful experience
├── How-to guides: the 5-10 most common real tasks
├── Reference: API/CLI/config/schema facts, generated where possible
└── Explanation: the 2-3 concepts users misunderstand most
```

For an agent-enabled product, add:

```text
Agent docs
├── Agent reference: tools, APIs, schemas, permissions, limits
├── Agent runbooks: deterministic task procedures
├── Agent context: domain rules and invariants
└── Agent update policy: what invalidates these instructions
```

## Backlog structure

Create backlog items as concrete documentation work, not vibes.

Bad:

```text
Improve onboarding docs.
```

Good:

```text
Rewrite docs/getting-started.md as a tutorial for first-time integrators.
Remove API parameter tables into reference/authentication.md.
Add expected output after each setup step.
Test with a developer who has never used the SDK.
```

Use this shape:

```yaml
- title: ""
  mode: tutorial | how-to | reference | explanation
  user_need: ""
  reader: ""
  source_of_truth: ""
  change_type: create | rewrite | split | merge | delete | update
  priority_reason: "user impact | correctness risk | release blocker | agent failure"
  done_when:
    - ""
```

## Navigation patterns

### Simple product

```text
Home
├── Get started         -> Tutorials
├── How-to guides       -> Goal-oriented task docs
├── Reference           -> API/CLI/config/spec facts
└── Concepts            -> Explanation
```

The labels do not need to be literal Diátaxis labels. “Concepts” may be a better label than “Explanation.” “Recipes” may work for how-to guides. Names are less important than clean user needs.

### Complex product

Start with the user's world. Then apply Diátaxis inside it.

```text
Home
├── For app developers
│   ├── Get started
│   ├── How-to guides
│   ├── API reference
│   └── Concepts
├── For platform operators
│   ├── Tutorials
│   ├── Operations guides
│   ├── Configuration reference
│   └── Architecture
└── For agents
    ├── Tool contracts
    ├── Runbooks
    ├── Schemas
    └── Domain invariants
```

### Landing pages

A landing page is not a dump of links. It should orient the reader.

Good landing pages:

- State who the section is for.
- Explain what kind of help lives there.
- Group lists into small clusters.
- Keep long lists mechanically ordered or split them.
- Give a short description for each major group.

## Maintenance cadence

Use triggers, not calendar theater.

Review a page when:

- The source-of-truth changes.
- A release changes workflow, fields, defaults, errors, examples, or screenshots.
- Support tickets show repeated confusion.
- An agent fails a task because docs were ambiguous, stale, or not parseable.
- The page is touched by a related code/config/schema change.

Calendar reviews are a fallback for high-risk pages, not the main system.

## Governance without bureaucracy

For each document family, define:

```text
Owner: who is accountable for correctness.
Source of truth: what beats the documentation when they disagree.
Review trigger: what forces a re-check.
Quality gate: what must be true before publish.
Deletion rule: when to remove or redirect the page.
```

## Audit process

1. Create inventory.
2. Mark each page by mode.
3. Mark mixed-mode pages.
4. Identify high-traffic or high-risk stale pages.
5. Pick one page and improve it.
6. Commit/publish.
7. Repeat.

The point is not to admire the map. The point is to make the next useful change.
