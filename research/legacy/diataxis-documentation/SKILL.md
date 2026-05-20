---
name: diataxis-documentation
description: Plan, audit, structure, and write documentation using the Diátaxis framework. Use for tutorials, how-to guides, reference, explanations, docs information architecture, docs inventories, docs roadmaps, docs backlogs, agent-readable docs, human-facing docs, runbooks, API docs, product docs, onboarding docs, or documentation system design.
license: CC-BY-SA-4.0
compatibility: Works in any Agent Skills-compatible client. Optional scripts require Python 3.10+.
metadata:
  version: "0.1.0"
  source-framework: "Diátaxis by Daniele Procida"
  source-url: "https://diataxis.fr/"
---

# Diátaxis documentation skill

Use this skill when the user asks you to create, improve, audit, reorganize, or govern documentation. The skill works for individual documents and for whole documentation programs. It covers documentation for humans and documentation for agents.

## First principle

Every useful document answers one primary user need. Pick the need before writing.

```text
                         User relationship to the craft
                         acquisition/study       application/work
Content informs action   tutorial                how-to guide
Content informs cognition explanation             reference
```

Ask two questions:

1. Is the reader trying to **do** something, or **know/understand** something?
2. Is the reader **acquiring** skill, or **applying** skill they already have?

Then write the document that belongs in that quadrant. Do not blur the quadrants because the result becomes mush.

## Operating rules

- Do not create four empty folders and call it architecture. That is cosmetic work. Start from user needs and existing content.
- One document may link to other modes, but it should not become multiple modes at once.
- A tutorial teaches through a safe, guided experience. It is not a task runbook.
- A how-to guide helps a competent user accomplish a real task. It is not a lesson.
- Reference states facts about the machinery. It is not a place for argument, teaching, or workflow prose.
- Explanation gives context, rationale, trade-offs, and mental models. It is not a disguised procedure.
- Agent-facing documentation still follows Diátaxis. The difference is the reader: the agent needs explicit contracts, defaults, constraints, examples, and failure handling.
- When information is missing, state assumptions and proceed with the smallest useful artifact rather than stalling.

## Activation workflow

1. **Classify the request.** Decide whether the user wants an individual document, a rewrite, an audit, an information architecture, a backlog, a governance system, or agent-facing docs.
2. **Identify the reader.** Name the human or agent persona, their competence level, their goal, and what source of truth the document depends on.
3. **Use the compass.** Choose tutorial, how-to guide, reference, or explanation. If several needs exist, split the output or make a map of linked documents.
4. **Choose the correct working path.**
   - For a single document, use `references/document-types.md` and one template in `assets/templates/`.
   - For an audit or reorg, use `references/documentation-program.md` and `references/quality-gates.md`.
   - For docs meant for agents, use `references/human-and-agent-docs.md`.
5. **Draft or restructure.** Produce the document, IA, backlog, or review notes. Keep the artifact useful now, not theoretically perfect.
6. **Validate.** Apply the relevant checklist in `references/quality-gates.md`. Call out mixed modes, missing source-of-truth, stale-risk, and broken reader flow.

## Output patterns

### Individual document

Return:

1. `Classification`: chosen Diátaxis mode, reader, need, and why.
2. `Document`: the actual draft or rewrite.
3. `Links needed`: adjacent documents that should exist instead of bloating this one.
4. `Open assumptions`: only facts that materially affect correctness.

### Documentation program or reorg

Return:

1. `Documentation map`: user groups, document modes, and top-level navigation.
2. `Inventory model`: fields to track for every page.
3. `Backlog`: concrete work items grouped by mode and priority.
4. `Operating cadence`: how docs stay current during product changes.
5. `Quality gates`: what reviewers check before publishing.

### Human + agent documentation

Return both layers when useful:

- Human page: narrative, context, confidence, examples, flow.
- Agent capsule: compact contracts, commands, schemas, constraints, expected outputs, failure modes, and update triggers.

Use `assets/templates/agent-reference.md` or `assets/templates/agent-runbook.md` when the agent layer needs to stand alone.

## When to use bundled scripts

The scripts are optional helpers, not authority.

- `scripts/generate_doc_skeleton.py` creates a starter template for a chosen mode.
- `scripts/classify_doc.py` gives a rough Diátaxis classification for one Markdown file.
- `scripts/audit_docset.py` scans Markdown files and emits an inventory CSV or JSON.

Use them when the user has files or asks for an audit. Do not pretend their heuristic classifications are final; use judgment.

## References to load on demand

- `references/diataxis-model.md`: the decision model, boundaries, and mental map.
- `references/document-types.md`: detailed writing rules for each document type.
- `references/documentation-program.md`: structuring the ongoing documentation effort.
- `references/human-and-agent-docs.md`: writing for humans and agents together.
- `references/quality-gates.md`: review checks and failure modes.
- `references/source-attribution.md`: source pages and attribution.
