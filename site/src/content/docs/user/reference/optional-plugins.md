---
title: "Optional plugins policy"
description: "Core rule:"
tusker:
  audience: "user"
  publish_path: "user/reference/optional-plugins"
  publish_section_title: "Reference"
  route: "/user/reference/optional-plugins/"
  source_kind: "repo_doc"
  source_path: "skill/references/OPTIONAL_PLUGINS.md"
  summary: "Core rule:"
  tags:
    - "reference"
  updated: "2026-04-21"
---

# Optional plugins policy

Core rule:

```text
Plugins are overlays.
Markdown notes are the system.
```

This bundle intentionally uses only core Obsidian features for the critical path.

## Why

Community plugins are useful, but they also add failure modes:

- schema drift
- broken updates
- opaque data formats
- harder automation on headless systems
- more maintenance burden for contributors

If you want the system to stay boring and durable, the source of truth must stay in the Markdown files and frontmatter.

## Good optional overlay categories

### Kanban boards
Useful when you want a visual board for a subset of work.

Recommended use:
- build a board from change status
- keep the change notes themselves as canonical
- do not let the board become the only place where status exists

### Task overlays
Useful for personal checklists and recurring tasks.

Recommended use:
- keep important project state in the note frontmatter and body
- use task plugins for convenience, not for canonical workflow state

### Git UX plugins
Useful if you want commit history or diff visibility from inside Obsidian.

Recommended use:
- nice-to-have only
- never rely on plugin state for automation

### Visual whiteboarding
Useful for design exploration, demos, or planning.

Recommended use:
- link the canvas back to the relevant change or decision note
- keep the decision itself in a Markdown note

## What not to do

- do not make a plugin-specific file format your source of truth
- do not scatter essential workflow logic across five plugins
- do not assume every contributor has the same plugin stack

## Safe default

Start with:

- Properties
- Templates
- Bases
- Backlinks
- Search

Only add overlays after the core workflow feels stable.
