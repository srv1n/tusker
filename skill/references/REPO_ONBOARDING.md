# Existing Repository Onboarding

Use this reference when a repository already exists and Tusker needs to be
installed, bootstrapped, or pre-populated with candidate knowledge and backlog.

## Core Rule

Do not use one giant prompt as the system. Use this chain:

```text
deterministic setup
  -> curated repo packet
  -> long-context onboarding plan
  -> deterministic Tusker import
  -> human promotion
```

The remote model proposes. Tusker writes valid state.

## Setup vs Onboarding

| Concern | Meaning | Owner |
|---|---|---|
| Setup | Install repo-local skills, initialize `.tusker`, write config, sync repo contract, update pointers. | Tusker CLI |
| Onboarding | Analyze an existing repo and propose domains, epics, backlog tasks, gates, and decisions. | Packet + remote model + Tusker import |
| Promotion | Decide which proposals become trusted canon or runnable work. | Human/reviewer |

Global install may install user-level skills and the binary. It must not mutate
the current repo unless the user passes an explicit repo target.

## Storage Boundary

Machine-shared runtime and portable project truth have different owners:

| Location | Contents |
|---|---|
| `~/Library/Application Support/tusker/` on macOS | Daemon database, project registry, global limits, logs, runtime workspaces, and the managed daemon binary. |
| `<repo>/.tusker/` | `WORKFLOW.md`, task contracts, gates, curated evidence, project knowledge, and generated project views. |
| `<repo>/tusker.yaml` | Repo-level agent and runner configuration where present. |

Do not copy a project's `WORKFLOW.md` into the shared state root. The daemon,
agents, worktrees, and reviewers must all observe the same repo-local contract.

On macOS, prefer `~/Developer`, `~/Code`, or `~/Projects` for repositories.
LaunchAgents may not inherit Terminal access to `Desktop`, `Documents`,
`Downloads`, or iCloud Drive. Project add and enable warn for those known
protected roots. `tusker daemon service install|start` refuses before launch
unless the project is moved, or the operator grants Full Disk Access to the
reported service executable and explicitly passes `--allow-protected-projects`.
Other cloud, network, and removable volumes may also require access; the
service startup health check remains authoritative for those environments.

## Target Setup Command

Planned:

```bash
tusker setup --repo . --profile app --yes
```

Compatibility alias:

```bash
tusker install --repo . --bootstrap
```

Until `setup` exists, use:

```bash
tusker purge --repo . --only-tusker-state
tusker install --repo . --no-bin
tusker init --yes
tusker validate --vault .tusker
tusker skill doctor --strict
```

For a deliberately clean reset of old Tusker state, run the purge plan first,
then apply it and initialize:

```bash
tusker purge --repo . --only-tusker-state --yes
tusker init --yes --fresh --purge-state
tusker skill sync --repo . --mode symlink --source /path/to/tusker
```

`purge` is scoped to generated Tusker state and managed Tusker pointers. It
must not remove product code. `--source` keeps repo-local skill symlinks pointed
at the canonical Tusker checkout even when the command is run from another repo.

## Existing Repo Packet

Do not upload the whole repository by default. Build a packet that contains
facts, summaries, selected excerpts, and a skipped-files report.

Target command:

```bash
tusker onboard pack --repo . \
  --out .tusker/scratch/onboarding/repo-packet.zip \
  --budget-tokens 120000 \
  --profile app \
  --redact \
  --json
```

Packet should include:

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
selected-excerpts/
skipped-files.md
redaction-report.md
```

Include exact content for small/high-signal files: README, contributing docs,
agent guidance, package manifests, build files, CI, schemas, examples, routes,
CLI maps, public APIs, and representative tests.

Exclude generated output, dependency caches, secrets, raw logs, binary/media
files, and Tusker runtime/generated paths.

## External Model Contract

Use `assets/templates/onboard-prompt.md` as the prompt source for the remote
model.

The model returns:

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

Hard rules:

- Use only packet facts.
- Mark missing facts as assumptions or open questions.
- Do not output code patches.
- Do not output final V7 markdown.
- Do not compute `state_rev`.
- Do not create dispatchable implementation work.
- Tasks default to `status: backlog` and held readiness.
- Decisions default to `proposed`.
- Domains default to `draft`.
- Gates exist only for missing facts that block safe planning or execution.

## Import Rules

Target command:

```bash
tusker onboard import --repo . \
  --plan onboarding-plan.zip \
  --as-proposals \
  --json
```

Import must:

- validate the plan before mutation;
- create records through Tusker constructors/writers;
- assign IDs and frontmatter locally;
- compute or reconcile metadata locally;
- preserve the model report under `work/inbox` or project sources;
- run `tusker validate` after mutation.

No imported task may become runnable unless a human explicitly promotes it and
verification is concrete.

## ChatGPT Handoff Usage

The ChatGPT handoff skill is transport. Use it to submit and collect:

```bash
chatgpt-handoff doctor
chatgpt-handoff submit --kind architect --brief-file prompt.md --zip repo-packet.zip
chatgpt-handoff collect <job-id> --force
chatgpt-handoff fetch <job-id> --out-dir .tusker/scratch/onboarding/chatgpt-<job-id>
```

Do not let ChatGPT handoff write Tusker state. Feed the returned plan to Tusker
import.

If `chatgpt-handoff doctor` reports stale browser workflows, refresh the
installed workflow catalog from the canonical browser checkout:

```bash
rzn-browser workflow pull --repo-root /path/to/rzn-browser
```

## Human Review

After import, review in this order:

1. `report.md` for broad wrong assumptions.
2. Domain canon for unsupported claims.
3. Gates and open questions for true human blockers.
4. Tasks for acceptance/proof quality.
5. Epics for whether the backlog shape is useful.

Promote only the records that survive review.
