---
title: "Repo contract"
description: "The vault is internal working memory. The repository is still where contributors land."
tusker:
  audience: "user"
  publish_path: "user/reference/repo-contract"
  publish_section_title: "Reference"
  route: "/user/reference/repo-contract/"
  source_kind: "repo_doc"
  source_path: "skill/references/REPO_CONTRACT.md"
  summary: "The vault is internal working memory. The repository is still where contributors land."
  tags:
    - "reference"
  updated: "2026-04-21"
---

# Repo contract

The vault is internal working memory. The repository is still where contributors land.

This bundle includes a repo scaffold that copies a minimal contributor contract into a code repository.

## What gets copied

- `.github/ISSUE_TEMPLATE/change.yml`
- `.github/ISSUE_TEMPLATE/bug.yml`
- `.github/ISSUE_TEMPLATE/config.yml`
- `.github/pull_request_template.md`
- `docs/agent-workflow.md`
- `docs/ai-contribution-policy.md`
- `AGENTS.workflow-snippet.md`

## Why this exists

You want the public repo to ask for:

- a clear problem statement
- scope and acceptance criteria
- contributor intent
- evidence, not vibes
- AI disclosure
- testing proof
- reviewer focus

But you do **not** want the repo templates to become a novel.

The vault handles richer internal detail.
The repo contract handles public intake and merge discipline.

## Suggested usage

### For a solo project with occasional contributors

- keep the public GitHub issue templates enabled
- keep the public PR template enabled
- use the vault privately for deeper planning and demo/doc generation
- link the public issue or PR URL into the relevant change note

### For a small team

- keep the repo templates
- add a short root `AGENTS.md`
- use the vault as shared working memory only if everyone accepts the convention
- otherwise keep the vault personal and use the repo as the team source of truth

## Merge philosophy

The repository templates are built around a simple rule:

```text
Review should start with evidence.
Code reading should be risk-proportional.
```

That means:

- low-risk changes can often be screened from the summary and proof first
- risky changes still deserve real code review
- raw transcripts are optional appendix material, not required reading

## AGENTS.md guidance

Do not turn `AGENTS.md` into an encyclopedia.

Use it as:

- a short map
- a rules index
- a pointer to deeper docs

The vault and repo docs should carry the detail.
