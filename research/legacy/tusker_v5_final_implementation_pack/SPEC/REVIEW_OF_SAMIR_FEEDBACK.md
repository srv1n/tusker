# Review of Samir's feedback

## Adopt

1. **Use Diátaxis, but do not worship it.** It is an authoring discipline, not a work hierarchy.
2. **Keep Markdown sacred.** Canonical source files stay plain Markdown + frontmatter.
3. **Make agent docs first-class.** `For agents` needs real recipes, runbooks, permissions, manual intervention rules, contracts, and evals.
4. **Put mode/audience/agent_layer on docs-map nodes and doc pages.** Tasks only point to `doc_nodes`.
5. **Use Markdoc only as optional publishing/rendering.** Start with Markdoc nodes; keep custom tags tiny and restricted.
6. **Pair video with Markdown companions.** Transcript, chapters, step list, expected outputs, manual intervention points, claims, source task.
7. **Patch the existing v4 architecture instead of reopening it.** The base direction is correct.

## Modify

1. Samir's `last_verified_at` idea is useful for readability, but verification events must remain the audit trail.
2. Markdoc should be optional. First ship plain Markdown docs-map validation, media-map, and docs evals.
3. The agent docs layer should feed `SKILL.md`, `AGENTS.md`, `llms.txt`, and docs evals. It is not just a public docs folder.

## Reject

1. Do not make Markdoc canonical.
2. Do not put Markdoc tags in task contracts or agent runbooks.
3. Do not use raw Diátaxis labels as the main public navigation.
4. Do not let videos or promo be source of truth.
5. Do not add per-task sidecar JSON mirrors.
6. Do not reintroduce a `docs:` enum on tasks. Use `doc_nodes` plus update/waiver events.

## Final patch

```text
Tusker v4.2
+ Diátaxis metadata
+ first-class agent docs
+ media/transcript contract
+ docs evals
+ optional Markdoc publishing
= Tusker v5
```
