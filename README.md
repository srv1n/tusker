# Tusker

Tusker is a repo-local, markdown-first work tracker for small teams and coding
agents. The repository owns the work state; Obsidian is a reading and editing
surface.

V7 is the default model. Work, gates, evidence, attempts, proposals, events, and
project knowledge are first-class files under `tusker/`.

```text
your-repo/
├── src/
└── tusker/
    ├── SKILL.md
    ├── work/
    │   ├── epics/
    │   ├── tasks/
    │   ├── gates/
    │   ├── decisions/
    │   └── inbox/
    ├── knowledge/
    │   └── domains/
    ├── evidence/
    ├── attempts/
    ├── events/
    ├── reviews/
    ├── dashboards/
    └── _generated/
```

![Tusker keeps the tracker in git while Obsidian stays a reading and editing surface.](docs/diagrams/01-two-surfaces.png)

## Why This Exists

Linear and Jira live outside the repo. GitHub Issues are thin. Beads gets the
git-native idea right, but Tusker is built around agent execution contracts:
tasks say what must be true, evidence proves it, gates represent real blockers,
and finish paths make completed work visible to review.

|                                       | Linear / Jira | GitHub Issues | Beads | Tusker |
| ------------------------------------- | :-----------: | :-----------: | :---: | :----: |
| Lives in your repo                    |       no      |    partial    |  yes  |   yes  |
| Plain markdown                        |       no      |      yes      | partial |  yes  |
| Obsidian-readable                     |       no      |       no      |   no  |   yes  |
| Gates and evidence as durable objects |    partial    |       no      |   no  |   yes  |
| Branch-safe proposals                 |       no      |       no      |   no  |   yes  |
| Installable agent skill               |       no      |       no      |   no  |   yes  |

Tusker is not for a 30-engineer sprint bureaucracy. It is for the smaller setup
where the repo is the source of truth and agents do real implementation work.

## V7 Model

```text
Epic      = durable workstream boundary
Task      = executable change contract
Gate      = blocker with owner, action, and verification
Evidence  = durable proof mapped to acceptance items
Attempt   = runtime execution record
Proposal  = branch-safe request to mutate protected control state
Knowledge = repo-local domain canon for future agents and humans
```

The task contract keeps five things separate:

| Section | Job |
|---|---|
| Acceptance | What must be true |
| Verification | How truth is checked |
| Evidence | Proof produced after execution |
| Gates | External blockers or human approvals |
| Knowledge delta | Durable understanding that changed |

The lifecycle is intentionally strict:

```text
ready -> active/rework -> review -> done
          |                ^
          v                |
        gate/proposal -----+
```

Implementation is not finished when an attempt is handed off. A worker must
either request review, create/propose a blocking gate, or mark the attempt
failed with a useful summary.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/srv1n/tusker/main/scripts/install.sh | sh
```

Install the agent skill for Claude Code and Codex:

```sh
curl -fsSL https://raw.githubusercontent.com/srv1n/tusker/main/scripts/install.sh | sh -s -- --codex-user --claude-user
```

Refresh the installed binary link and skill bundle after pulling or rebuilding:

```sh
tusker update
```

Initialize V7 in a repo:

```sh
cd ~/code/my-app
tusker init --yes
```

Legacy V5/V6 scaffolds are explicit:

```sh
tusker legacy init --yes
tusker init --migrate-v5 --vault ./tusker --yes
```

## Agent Workflow

For new work, an agent should:

1. Run `tusker list --type epic`.
2. Search for duplicates with `tusker search "<duplicate clue>" --type task`.
3. Read only the likely task or epic with `tusker show <ID> --capsule`.
4. Create the narrowest task, gate, or decision needed.
5. Attach acceptance-linked evidence.
6. Finish with `tusker finish <TASK-ID> --summary "<what changed and proof>"`.

Common V7 commands:

```sh
tusker new epic --acronym APP --title "App foundation"
tusker new task --epic APP --title "Add provider smoke harness"
tusker new gate --blocks APP-T-0001 --kind auth --owner human:sarav
tusker evidence add APP-T-0001 --kind automated_test --covers A1,A2 --summary "Focused tests passed."
tusker finish APP-T-0001 --summary "Implemented harness and attached proof."
tusker proposal accept APP-P-0001 --by human:sarav
tusker proposal apply APP-P-0001 --by human:sarav
tusker status APP-T-0001 review
tusker close APP-T-0001 --by human:sarav
tusker validate
```

`close` requires `review` unless forced. `propose status --status done` is
rejected; use `propose close` or `close`.

## Knowledge And Docs

V7 canonical knowledge lives under `tusker/knowledge/domains/**`. Start with the
repo-local `tusker/SKILL.md`, then load the narrowest domain `INDEX.md`, then
`CANON.md` only when needed.

Human-facing docs and sites are synthesized outputs from canonical sources. Do
not publish task records, evidence logs, attempt logs, generated manifests, or
agent-only instructions as user/developer prose.

The old V5 docs publication system remains available only through legacy
commands:

```sh
tusker legacy docs map
tusker legacy docs check <TASK-ID>
tusker legacy docs export --site ./site
tusker legacy docs build --site ./site
```

## Design Principles

- Markdown is the source of truth. Generated JSON and dashboards are caches.
- Protected control state changes through CLI control commands or proposals.
- Evidence is artifacts and accepted proof, not promises.
- Agents implement; humans or independent reviewers gate acceptance.
- Gates model real blockers instead of burying them in status text.
- V7 bootstrap is clean. Legacy scaffolds require explicit legacy commands.
- Progressive disclosure matters: read the project skill and narrow domain canon
  before broad vault spelunking.

## Read Next

- [`skill/SKILL.md`](skill/SKILL.md) - operator skill
- [`skill/references/COMMANDS.md`](skill/references/COMMANDS.md) - CLI surface
- [`skill/references/WORKFLOW.md`](skill/references/WORKFLOW.md) - lifecycle
- [`skill/references/RISK_AND_EVIDENCE.md`](skill/references/RISK_AND_EVIDENCE.md) - proof policy
- [`skill/references/ENGINEERING_DISCIPLINE.md`](skill/references/ENGINEERING_DISCIPLINE.md) - test and debugging discipline
- [`tusker/docs/spec/tusker-v7-repo-local-work-tracker-spec.md`](tusker/docs/spec/tusker-v7-repo-local-work-tracker-spec.md) - V7 spec
