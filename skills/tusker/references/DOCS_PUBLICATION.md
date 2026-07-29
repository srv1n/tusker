# Documentation publication

Use this guide for public docs, developer references, guides, runbooks, release
notes, or agent-readable project canon.

## Capability boundary

Start with `tusker capabilities --json`. In the V7 CLI, the live docs helpers
are `docs find`, `docs new`, and `docs map`; project-skill export is
`publish skill --v7`. Removed docs/knowledge/publication commands appear under
typed deprecations. Do not invoke or reconstruct them.

Tusker does not own the repository's site build. Use the build/preview command
declared by that repo after inspecting its manifest, package scripts, or
contributor instructions.

## Select the authority

| Need | Source of truth |
|---|---|
| Product/system behavior | Canonical system doc plus executable code/tests |
| Repo-native spec, guide, or README | Tracked file registered by `docs/publication.yaml` when published |
| Agent project knowledge | `.tusker/knowledge/domains/<domain>/INDEX.md` and `CANON.md` |
| Task-specific proof | Task verification/evidence; never publication prose by default |
| Generated site output | Build artifact only; edit its registered source |

If sources disagree, report the conflict. Do not silently prefer a generated
manifest over newer executable truth or treat historical tasks as canon.

## Authoring contract

Choose one primary audience and one primary mode before writing:

| Audience | Include | Exclude by default |
|---|---|---|
| User | Outcome, steps, expected result, recovery | Internal IDs, work logs, implementation paths |
| Developer | Contracts, architecture, edge cases, verification | Raw transcripts and redundant task history |
| Agent | Exact constraints, source paths, stale triggers | Marketing and tutorial padding |
| Internal/operator | Decisions, authority, migration and recovery | Basic onboarding prose |

Modes are tutorial, how-to, reference, or explanation. A page gets one dominant
mode: tutorials teach safely, how-tos complete a task, references state exact
facts, and explanations clarify design and tradeoffs.

## Workflow

1. Inspect the task capsule and acceptance. If it names `doc_nodes`, resolve
   them with `tusker docs map [DOC-NODE]`; do not invent node IDs.
2. Read only the owning canon and changed implementation/tests.
3. Record a concise knowledge delta for high-risk or doc-targeted work.
4. Edit the canonical source. Keep task records, attempts, events, evidence
   logs, packet caches, runtime state, and secrets out of reader-facing prose.
5. Register repo-native published sources in `docs/publication.yaml`; do not
   make a site crawler discover arbitrary Markdown.
6. Run the repository-defined validation and publication pipeline. Record the
   smallest command + PASS/FAIL proof mapped to acceptance.
7. If a contractually required subjective docs review remains, create or honor
   the exact human gate and stop. Objective correctness belongs to agent review.

## Project knowledge is separate

The repo `.tusker/SKILL.md` is a generated router, not a docs dump. Domain
creation/validation keeps its routes aligned with domain `INDEX.md` and
`CANON.md`. When a portable project-skill package is explicitly required, use:

```bash
tusker publish skill --v7 --out <generated-directory>
```

Never hand-edit generated operator-skill installs or exported project-skill
packages. Patch their canonical sources, then sync or regenerate.
