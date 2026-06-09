---
schema: "tusker.domain/v7"
kind: "domain"
id: "gates"
project: "tusker"
title: "gates"
status: "current"
summary: "Human, reviewer, CI, and external gates."
source_of_truth:
  - "knowledge/domains/gates/CANON.md"
canonical_files:
  - "INDEX.md"
  - "CANON.md"
created_at: "2026-05-19T05:18:02Z"
updated_at: "2026-05-29T14:00:10Z"
state_rev: "sha256:63a1b9c998e5698f55ca42cb7a5ffc1e1df5df390bb4047eefa5d3621b609002"
---

# gates

## Summary

Human, reviewer, CI, and external gates. Human/external gates require a concrete capability boundary; reviewer-capable work must not be punted to humans.

## Read This When

- You need current source-of-truth context for gates.
- You are changing behavior owned by this domain.

## Canonical Files

- CANON.md - current durable truth.
- INDEX.md - domain map and routing hints.

## Runbooks

- _None yet._

## Interfaces

- _No stable interfaces declared yet._

## Invariants

- Keep durable truth in CANON.md.
- Put procedural guidance in runbooks/.
- Human/external blockers need owner, action, verification, blocked task, and `why_agent_cannot`.
- Spec/API conflict gates use `gate_kind: decision` and include a suggested resolution.

## Sources

- Raw external input belongs in sources/. Do not treat root docs/ or site output as canonical V7 knowledge.

## Glossary

- See glossary.md.

## Current Work

- _No current work linked._
