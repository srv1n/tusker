# Workflow and docs sync

## Core rule

A task is not done when code lands.

A task is done when:
1. acceptance criteria are satisfied,
2. required deliverables exist,
3. docs impact is resolved or explicitly waived.

## Recommended close flow

```text
Task ready_for_review
    ↓
code/tests/evidence complete
    ↓
docs-impact hook runs
    ↓
doc update is generated or a waiver is recorded
    ↓
human reviews evidence packet
    ↓
task can close
```

## Hook vs sweeper

Use both.

### 1) Close-time hook (primary)

Run when a task moves to review or done.

Inputs:
- task markdown
- git diff
- changed files
- acceptance criteria
- docs-map.yaml
- existing target docs

Output:
- suggested doc patches
- updated `doc_nodes` if the task was underspecified
- a waiver request if no doc update is needed

This is the main guardrail.

### 2) Periodic sweeper (backup)

Run daily or on CI.

Checks:
- code changed in areas with mapped doc nodes but no linked task/doc update
- docs pages older than the latest source task/commit
- broken examples, dead links, missing screenshots, stale API/config snippets

This is not the main mechanism. It is the janitor.

## Multi-agent rule

Do not let every implementation agent freestyle edits into the docs corpus.

Use one of these patterns:
- same task agent can patch docs if only one target page is affected
- dedicated docs agent handles updates when multiple target pages are involved
- serialize work per doc node to avoid merge garbage

A simple lock is enough:

```text
docs-lock/<node-id>
```

If locked, enqueue a follow-up docs run instead of editing concurrently.

## Waivers

Sometimes a task truly needs no docs update. That should be explicit.

Store a short waiver note in the task:

```yaml
action: none
targets: []
notes:
  - internal refactor only; no user/operator/developer-facing behavior changed
```

## Freshness model

Every doc page should be able to answer:

- what node am I?
- which tasks changed me?
- when was I last verified?
- what source tasks or commits am I based on?

That is better than a random `updated_at` date with no provenance.
