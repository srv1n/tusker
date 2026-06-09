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
- Tusker lookups should start with `tusker search`, `tusker list`, and exact task paths, not broad reads of attachments, generated indexes, or logs

## AGENTS.md guidance

Do not turn `AGENTS.md` into an encyclopedia.

Use it as:

- a short bootstrap pointer
- a repo-local override surface for rules that truly differ from Tusker defaults
- a pointer to the installed Tusker operator skill and repo `tusker/SKILL.md`

The installed Tusker skill carries tracker mechanics and can be refreshed centrally.
The repo `tusker/SKILL.md` carries project knowledge routing.
The vault carries durable task proof and history.

Put repo-specific validation commands, command wrappers, build-lock/status commands, and forbidden expensive probes in `tusker/SKILL.md` or routed runbooks. Keep root `AGENTS.md` focused on the managed bootstrap pointer.

Do not copy workflow prose, command tutorials, generated indexes, task history, event paths, evidence logs, or raw terminal output into `AGENTS.md`.

## Skill Source vs Project Memory

Use this split:

```text
canonical skill source
  = reusable behavior, scripts, prompts, parsers, contracts

repo-local config/state/profile
  = project IDs, thread IDs, model lanes, repo quirks, preferred workflows

generated installs
  = adapter symlinks or materialized copies for Codex, Claude, cloud, and handoff packets
```

Default local mode:

```bash
tusker skill sync --repo . --mode symlink
```

Portable mode:

```bash
tusker skill bundle --repo . --out .tusker/_generated/skill-bundle
```

Patch routing:

| Patch target | Correct destination |
| --- | --- |
| Generic Tusker/skill behavior | canonical `skill/**` source |
| Repo-specific workflow or domain knowledge | `.tusker/SKILL.md` or `.tusker/knowledge/domains/**` |
| ChatGPT Project id, model lane, repo handoff quirks | `.chatgpt-handoff.json` or `.chatgpt-handoff/profile.md` |
| Generated `.agents/skills/**` or `.claude/skills/**` copy | reject or rewrite to source |

Generated materialized skill copies are like `dist/`: useful for portability, bad as editable source.
