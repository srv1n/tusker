---
schema: "tusker.domain-canon/v7"
kind: "domain_canon"
id: "project/canon"
project: "tusker"
domain: "project"
title: "Project canon"
status: "current"
summary: "Current facts about the Tusker repository."
capsule:
  skip_when: "Skip when the task needs only proof or a generated view."
  use_when: "Read before changing repository behavior or project documentation."
  what: "Source layout, runtime boundaries, and project rules."
source_of_truth:
  - "knowledge/domains/project/CANON.md"
created_at: "2026-08-23T10:56:31Z"
updated_at: "2026-08-23T15:48:02Z"
state_rev: "sha256:82fbda3f3ac55bd8ccc5e75f1c8e5210c9e834c43bae8f71cf70afd170a795d6"
---

# Project canon

## Current truth

Tusker is a Go CLI and local work tracker. `cmd/tusker/` contains the CLI,
daemon, runtime commands, and Serve handlers.

The main source areas are:

- `internal/v7schema/` for current record and field rules;
- `internal/v7policy/` for close policy;
- `internal/docgraph/` for system document maps and freshness;
- `internal/serve/` for embedded Serve files;
- `internal/acp/` for ACP transport;
- `apps/mac/TuskerBar/` for the macOS shell;
- `skills/tusker/` for the operator skill; and
- `docs/system/` for the current public explanation.

## Canonical rules

- The repository vault is `.tusker/`.
- The CLI owns tracker mutations.
- The task contract owns product scope.
- Proof must link to acceptance rows.
- A gate names one fact that needs a human or an external system.
- Automation stays off until an operator enables it.
- Source code and schemas are the authority for current behavior.
- Repository state and shared runtime state are separate authorities.
- Format suffixes such as `/v7` identify stored data. They do not identify a
  product generation.

## Stable interfaces

- CLI commands and their JSON output.
- Current task, gate, evidence, proof, wave, and decision records.
- The project skill route in `.tusker/SKILL.md`.
- The system documentation map in `docs/system/`.

## Execution contract

`docs/system/execution-observability.md` explains the execution ledger. The
source authority is `cmd/tusker/execution_ledger.go`,
`cmd/tusker/execution_graph.go`, and `cmd/tusker/serve_execution_timeline.go`.

## Constraints

- Keep system guides in simple technical English.
- Do not edit generated maps by hand.
- Do not read raw tracker state unless the task requires it.
- Do not change another project while working in this repository.
- Do not start nested runners from an interactive session.

## Known current limits

- `RuntimeStore.RemoveProject` does not clear every project-keyed runtime table.
- A local build is not a signed public release.
- The embedded Serve `dist/` must be rebuilt after TypeScript source changes.
