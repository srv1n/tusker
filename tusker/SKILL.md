---
schema: "tusker.project-skill/v6"
name: "project-knowledge"
description: "Understand, modify, explain, or verify this repository using its domain canon, codebase map, task proof, and knowledge graph."
---

# Project knowledge skill

Use this file to route through this repository's Tusker knowledge graph.
Use the Tusker operator skill for task mechanics and CLI workflow.

## Routing rule

Start with the narrowest domain INDEX. Read CANON before task history.
Read task files only for proof, evidence, or implementation history.

## Answering rules

1. Prefer domain CANON.md over task history.
2. Prefer source code or API schemas over prose when exact behavior conflicts.
3. When code and canon disagree, trust code, mark canon stale, and report the conflict.
4. Do not read generated output by default.
5. Do not load full files when a capsule or section read is enough.
6. When suggesting a code change, include verification.
7. When production impact is possible, include rollback or safe-change checks.

## Domains

<!-- tusker:domains:begin -->
| Intent | Read first | Canon | Notes |
|---|---|---|---|
| Install, migrate, or roll out Tusker | [[adoption/INDEX]] | [[adoption/CANON]] | Install, migration, rollout, and consumer repo adoption. |
| Change or inspect CLI behavior | [[cli/INDEX]] | [[cli/CANON]] | Command surface, flags, help text, routing, and user-visible terminal behavior. |
| Change repository code safely | [[codebase/INDEX]] | [[codebase/CANON]] | Repository layout, implementation anchors, testing, and safe change rules. |
| Change knowledge graph, freshness, or publish | [[knowledge-system/INDEX]] | [[knowledge-system/CANON]] | Domain knowledge graph, indexer, freshness, routing, backrefs, and publication. |
| Change vault navigation or Obsidian views | [[obsidian/INDEX]] | [[obsidian/CANON]] | Vault layout, wikilinks, managed blocks, Bases views, and graph navigation. |
| Understand daemon, runner, leases, or reviewer lane | [[runtime/INDEX]] | [[runtime/CANON]] | Daemon dispatch, runner state, review lane, leases, attempts, sessions, and logs. |
| Change frontmatter, validation, or migration | [[schema/INDEX]] | [[schema/CANON]] | Note schemas, frontmatter, validation, templates, and migrations. |
| Change operator or project skill guidance | [[skill/INDEX]] | [[skill/CANON]] | Operator skill, project skill router, agent instructions, and bundled guidance. |
| Change lifecycle or close policy | [[workflow/INDEX]] | [[workflow/CANON]] | Task lifecycle, close gates, verification, evidence, and review policy. |
<!-- tusker:domains:end -->
