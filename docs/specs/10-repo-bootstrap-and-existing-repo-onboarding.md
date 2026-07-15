---
capsule:
  what: "Repo bootstrap and existing-repo onboarding spec for Tusker installs."
  use_when:
    - "Work changes init/update/install or repo onboarding behavior."
  skip_when:
    - "The task only changes runtime dispatch or proof validation."
---

# 10 - Repo Bootstrap and Existing-Repo Onboarding

Date: 2026-06-05

Status: implementation spec

Audience: Tusker CLI, installer, skill, and ChatGPT handoff maintainers

## Summary

Existing repository onboarding is three separate products:

```text
repo setup -> curated repo packet -> conservative onboarding import
```

Do not solve this with one giant prompt. The prompt is a handoff contract for a
remote model. Tusker owns setup, packet generation, import, validation, state
metadata, and promotion.

## Product Boundaries

| Layer | Owner | Purpose | Must not do |
|---|---|---|---|
| Repo setup | Tusker CLI | Install repo-local skills, initialize `.tusker`, sync repo contract files, update pointers, validate. | Mutate a repo during ambient global install. |
| Repo packet | Tusker CLI | Build a redacted, curated codebase digest for a long-context model. | Upload the whole repo by default. |
| External analysis | ChatGPT handoff / human | Produce an onboarding plan from the packet. | Generate final V7 markdown with fake IDs or `state_rev`. |
| Import | Tusker CLI | Convert plan files into draft domains, proposed decisions, held backlog tasks, and gates. | Create dispatchable tasks by default. |

## Current Repo Facts

- `tusker install --repo <path>` installs repo-local skills under `.agents/skills/tusker` and `.claude/skills/tusker`, refreshes managed `AGENTS.md` / `CLAUDE.md` pointer blocks, and creates feedback guidance. Repo-local installs default to symlinks to canonical source; pass `--skill-mode copy` when a repo must be self-contained.
- `tusker skill sync --repo . --mode symlink|copy` is the explicit repo-local skill refresh surface.
- `tusker skill bundle --repo . --out .tusker/_generated/skill-bundle` materializes portable copies for cloud/handoff packets.
- `tusker init --yes` initializes the V7 vault under `.tusker`, writes `tusker.yaml`, creates project knowledge, and can sync repo-contract files.
- `make install-repo REPO=/abs/path` currently calls `tusker install --repo ...`; it does not initialize `.tusker`.
- `make codebasezip` creates a broad reviewable archive. It is useful for patch/review handoff, but it is not a curated onboarding packet.
- V7 work objects require valid frontmatter, IDs, paths, and `state_rev`; a remote model should output an import plan, not final committed records.

## Repo Setup

Add one explicit orchestration command:

```bash
tusker setup --repo <path> \
  [--profile app|library|cli|infra|v7] \
  [--yes] \
  [--force] \
  [--dry-run] \
  [--json] \
  [--no-bin] \
  [--no-skills] \
  [--no-vault] \
  [--no-contract] \
  [--no-pointers] \
  [--no-gitignore]
```

Also support this convenience alias:

```bash
tusker install --repo <path> --bootstrap
```

`make install-repo REPO=/abs/path` should call setup, not skill-only install.

### Setup Behavior

For a repo path, setup is idempotent:

1. Resolve the repo root.
2. Install repo-local skill bundles:
   - `.agents/skills/tusker/**`
   - `.claude/skills/tusker/**`
   - default local mode is symlink; portable mode is generated copy
3. Initialize or refresh V7 vault baseline:
   - `.tusker/SKILL.md`
   - `.tusker/WORKFLOW.md`
   - `.tusker/knowledge/domains/project/{INDEX.md,CANON.md,glossary.md}`
   - `.tusker/work/{epics,tasks,gates,decisions,inbox,closeouts,archive}`
   - `.tusker/evidence`, `.tusker/events`, `.tusker/attempts`, `.tusker/dashboards`
   - `.tusker/_generated/{indexes,packets,bases}`
4. Write `tusker.yaml` if missing.
5. Sync repo contract files when enabled:
   - `.github/ISSUE_TEMPLATE/**`
   - `.github/pull_request_template.md`
   - `docs/agent-workflow.md`
   - `docs/ai-contribution-policy.md`
   - `AGENTS.workflow-snippet.md`
6. Upsert only the managed Tusker blocks in `AGENTS.md` and `CLAUDE.md`.
7. Add `.gitignore` rules for Tusker runtime/generated noise.
8. Run `tusker reindex` and `tusker validate` unless dry-run or explicitly skipped.

### Setup Non-Negotiables

- Global install must ask before installing global user skills.
- Global install must not mutate the current repo unless a repo target is explicit.
- Repo setup should first detect whether global user skills are installed and report that state, but repo setup must still install repo-local skills when requested.
- Preserve existing `.tusker` content.
- Preserve non-managed `AGENTS.md` / `CLAUDE.md` content.
- Only mutate text between Tusker-managed markers.
- Do not overwrite human-authored repo docs unless `--force` is passed and the file belongs to the managed repo-contract bundle.
- JSON output must include `created`, `updated`, `skipped`, `warnings`, and `next_steps`.
- Generated skill install copies are not editable source. Generic skill improvements patch canonical skill source; project memory patches go to `.tusker/**`, `.chatgpt-handoff.json`, or `.chatgpt-handoff/profile.md`.

### Internal Shape

Use one shared setup function:

```go
type RepoSetupOptions struct {
    RepoPath        string
    Profile         string
    InstallBin      bool
    InstallSkills   bool
    InitVault       bool
    SyncContract    bool
    UpdatePointers  bool
    UpdateGitignore bool
    Force           bool
    DryRun          bool
}

func setupRepo(opts RepoSetupOptions) (RepoSetupReport, error)
```

Then wire existing entrypoints into it:

- `tusker setup --repo ...`
- `tusker install --repo ... --bootstrap`
- `make install-repo REPO=...`

Reuse `syncRepoContract`, `upsertRepoTuskerPointers`, `bootstrapV7Profile`, and
`installSkillPayload`. Do not fork their behavior.

## Existing-Repo Onboarding

Onboarding creates candidate knowledge and backlog. It is not setup, and it is
not task dispatch.

Default imports:

| Record | Default state |
|---|---|
| Domains | `draft` |
| Epics | `draft` or planning `ready` |
| Tasks | `backlog` plus held readiness |
| Gates | open, non-blocking unless a specific generated task depends on them |
| Decisions | `proposed` |

No generated onboarding task should be dispatchable unless a human explicitly
promotes it and the verification contract is concrete.

### Target Commands

```bash
tusker onboard pack --repo . \
  --out .tusker/scratch/onboarding/repo-packet.zip \
  --budget-tokens 120000 \
  --profile app \
  --redact \
  --json
```

```bash
tusker onboard prompt --repo . \
  --packet .tusker/scratch/onboarding/repo-packet.zip \
  > .tusker/scratch/onboarding/prompt.md
```

```bash
tusker onboard import --repo . \
  --plan onboarding-plan.zip \
  --as-proposals \
  --json
```

The import path must use Tusker's real writers so IDs, paths, frontmatter, and
`state_rev` are valid.

## Packet Contents

The packet should contain enough evidence to infer architecture, product shape,
build/test commands, and planning surfaces without shipping the full repo:

```text
repo-profile.json
file-tree.txt
file-stats.json
language-summary.json
entrypoints.md
build-and-test.md
ci-summary.md
public-api-summary.md
routes-summary.md
data-model-summary.md
test-surface-summary.md
docs-summary.md
existing-agent-guidance.md
skipped-files.md
redaction-report.md
selected-excerpts/
```

Include exact contents for small/high-signal files:

- `README.md`, `CONTRIBUTING.md`, `CHANGELOG.md`, `LICENSE`
- `AGENTS.md`, `CLAUDE.md`
- package manifests and build files
- CI files
- config schemas and examples with secrets removed
- route maps, CLI command maps, public API signatures
- test names and representative focused tests

Exclude by default:

- `.git/**`
- dependency caches and virtual environments
- generated build, coverage, and distribution output
- binary/media files
- `.env*`, credentials, certs, keys, tokens
- raw logs, crash dumps, telemetry dumps
- Tusker runtime/generated paths: `.tusker/events`, `.tusker/attempts`, `.tusker/_generated`, `.tusker/scratch`, `.tusker/evidence/**/artifacts`, `.tusker-runtime`, `.tusker-state`, `.tusker-worktrees`

## Model Output Contract

The long-context model returns this plan structure:

```text
onboarding-plan/
  manifest.json
  report.md
  domains.yaml
  epics.yaml
  tasks.yaml
  gates.yaml
  decisions.yaml
  docs-map.yaml
  assumptions.md
  open-questions.md
```

Every factual statement must either cite packet evidence or be labeled as an
assumption/open question. Every task must include acceptance and proof. Every
task defaults to backlog/held.

## Skill Integration

Do not create a separate global Tusker onboarding skill. Add a Tusker operator
reference:

```text
skills/tusker/references/REPO_ONBOARDING.md
skills/tusker/assets/templates/onboard-prompt.md
```

The installed operator skill is the source of truth. The external prompt is a
generated artifact from that source of truth.

The ChatGPT handoff package at `/Users/sarav/Downloads/side/gpt-arch` remains
transport:

```bash
chatgpt-handoff submit --kind architect --brief-file <prompt.md> --zip <repo-packet.zip>
```

Tusker defines the packet and import contracts; ChatGPT handoff submits and
collects.

## Implementation Backlog

### Epic RBS - Repository Bootstrap

Thesis: make repo setup one explicit idempotent operation that installs local
skills, initializes V7 `.tusker`, writes config, syncs repo contract, and
validates.

Tasks:

1. Add `tusker setup --repo <path>`.
2. Add shared `setupRepo` function and report type.
3. Wire `install --repo --bootstrap` to setup.
4. Update `make install-repo` to call setup.
5. Add `--dry-run` and `--json` report tests.
6. Add idempotence tests.
7. Add `AGENTS.md` / `CLAUDE.md` preservation tests.
8. Add setup docs/help output.

### Epic ONB - Existing Repository Onboarding

Thesis: create candidate Tusker knowledge and backlog from a curated repo packet
without uploading the whole repo.

Tasks:

1. Add `tusker onboard pack`.
2. Add redaction and exclusion policy.
3. Add selected-excerpt generators.
4. Add `tusker onboard prompt`.
5. Define `tusker.onboard_plan/v1`.
6. Add `tusker onboard import`.
7. Import domains as draft.
8. Import tasks as backlog/held.
9. Add no-ready-by-default guardrail.
10. Add fixtures for Go, Node, Python, and mixed repos.

## Done Criteria

Repo setup is done when this works on an existing repo without clobbering human
content:

```bash
tusker setup --repo . --profile app --yes
tusker validate --vault .tusker
tusker skill doctor --strict
```

Onboarding is done when this works without uploading the full repo:

```bash
tusker onboard pack --repo . --out packet.zip --redact
tusker onboard prompt --repo . --packet packet.zip > prompt.md
# upload packet.zip + prompt.md through ChatGPT handoff or manually
tusker onboard import --repo . --plan onboarding-plan.zip --as-proposals
tusker validate --branch-policy
```
