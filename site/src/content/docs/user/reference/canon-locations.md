---
title: "Canon locations"
description: "Tusker supports three legitimate canon patterns. Pick one **per epic**, make it explicit, then create the story stack that executes it."
tusker:
  audience: "user"
  publish_path: "user/reference/canon-locations"
  publish_section_title: "Reference"
  route: "/user/reference/canon-locations/"
  source_kind: "repo_doc"
  source_path: "skill/references/CANON_LOCATIONS.md"
  summary: "Tusker supports three legitimate canon patterns. Pick one **per epic**, make it explicit, then create the story stack that executes it."
  tags:
    - "reference"
  updated: "2026-04-22"
---

# Canon locations

Tusker supports three legitimate canon patterns. Pick one **per epic**, make it explicit, then create the story stack that executes it.

## The rule

```text
Every active epic must declare canon and have at least one story.
```

If the epic is active and the implementation stack does not exist yet, the scoping is incomplete.

## The three canon patterns

| Canon lives in | Use when | What to set |
|---|---|---|
| epic `## Design` | the RFC is living and will evolve with the workstream | make `## Design` substantive |
| canonical D-note | the spec should be a frozen or separately reviewed design record | `tusker new-doc --audience developer --canon-for <EPIC>` and set epic `spec_source` to that note |
| repo file via `spec_source` | the contract ships with the codebase or external consumers read it there | set epic `spec_source` to the repo path |

## Pick the epic body when

- the workstream is still evolving
- product and technical decisions will keep moving during execution
- you want one living epic page that readers can open first

Good fit:

- rollout plans
- phased migrations
- subsystem shape that will change as stories land

## Pick a canonical D-note when

- the design needs focused review as its own artifact
- multiple stories will cite a shared frozen decision set
- the doc should outlive day-to-day edits to the epic

Create it like this:

```bash
tusker new-doc --epic <ACR> --title "<Spec title>" \
  --audience developer --canon-for <ACR>
```

Then wire the epic to it:

```yaml
spec_source: "[[<ACR>-D-0001]]"
docs:
  - "[[<ACR>-D-0001]]"
```

## Pick a repo file when

- the spec is part of a shipped artifact
- external consumers read it from the repository
- the file belongs beside code generation or protocol assets

Example:

```yaml
spec_source: "docs/specs/my_protocol.md"
```

## Companion docs are not canon by default

Use a companion doc when the content supports execution but is not the primary contract:

- design deep-dive
- alternatives analysis
- user guide
- release note
- research memo

Create it like this:

```bash
tusker new-doc --epic <ACR> --title "<Doc title>" \
  --audience developer --companion-to <ACR-S-0001>
```

## What not to do

- do not create a beautiful canonical spec with zero stories
- do not leave canon implicit
- do not make every developer doc canonical by accident
- do not copy spec prose into story bodies

## Story citation

Story `## Canon` should point at the chosen source in read order:

```markdown
## Canon

- Epic: [[<ACR>]]
- Spec: [[<ACR>-D-0001]] §§4, 7
- Contract: `docs/specs/my_protocol.md`
```
