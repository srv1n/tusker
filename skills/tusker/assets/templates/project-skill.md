---
schema: tusker.project-skill/v7
name: project-knowledge
kind: project_skill
description: "Find or update this repository's domain canon when a task needs repository-specific facts."
capsule:
  what: ""
  use_when: ""
  skip_when: ""
source_of_truth: [knowledge/domains]
---

# Project knowledge skill

Use this route for repository-specific facts. The Tusker operator skill owns
task lifecycle, proof, gates, and CLI procedures.

## Find the relevant canon

For tracked implementation, read `tusker packet <TASK-ID> --for agent` and its
governing sections; retain the complete acceptance and ownership contract.
An exact document reference can be read directly. Otherwise choose the
narrowest domain below: its `INDEX.md` locates the owning `CANON.md` section
or leaf. Expand only when the request or an unresolved fact needs more context.

## Domains

<!-- tusker:domains:begin -->
| Domain | Read when | Read first | Canon |
|---|---|---|---|
<!-- tusker:domains:end -->

## Repo Command Policy

Keep repository-specific validation, build-lock/status commands, token/noise
wrappers, and expensive-probe restrictions here or in the owning runbook.
Root `AGENTS.md` and `CLAUDE.md` use managed Tusker bootstrap pointers alongside
user-owned instructions; keep detailed procedures in their owning documents.

## Updating Canon

Update the owning canon or leaf when durable behavior, policy, or interfaces
change. Preserve decisions and source links; separate documented contracts,
inspected implementation, observed behavior, and unresolved facts.

## Forbidden Source Truth

Project canon stays under `knowledge/domains/`; raw external input belongs in
the owning domain's `sources/`. Root `docs/` can hold current system guides.
Task records, proof logs, attempts, events, generated packets, runtime state,
and machine-local absolute paths are not project-skill source truth. Keep
`work/**`, `epics/**`, `evidence/**`, `attempts/**`, `events/**`, `_generated/**`,
`_system/**`, `dashboards/**`, and `.tusker-*` out of published canon.

## Validation

- `tusker skill doctor --strict --json` checks project skill routes and package hygiene.
- `tusker validate --json` checks the domain layout and task-domain coverage.
