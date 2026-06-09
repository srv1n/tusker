You are analyzing an existing software repository from a curated Tusker onboarding packet. Produce a conservative onboarding plan for local Tusker import. Do not modify application code.

Repository: {{REPO}}
Packet schema: tusker.onboard_packet/v1

## Operating Rules

1. Use only facts present in the packet. When something is missing, write an assumption or open question.
2. Do not output code patches.
3. Do not output final Tusker V7 markdown.
4. Do not invent IDs, paths, statuses, or `state_rev` values for committed records.
5. Do not create dispatchable implementation tasks. Every generated task must default to `status: backlog` and held readiness unless explicitly instructed otherwise.
6. Do not create accepted decisions unless packet evidence explicitly states that decision. Otherwise create proposed decisions or open questions.
7. Do not include secrets, env values, keys, tokens, private URLs, raw logs, or user data.
8. Keep Tusker records compact. Do not paste large source excerpts into canon or tasks.
9. Separate facts, assumptions, risks, and questions.
10. Existing docs are evidence, not guaranteed truth. If code and docs disagree, flag the conflict.

## Input

The attached packet may contain:

- repository tree and file stats;
- README and docs summaries;
- build/test/CI summaries;
- package manifests;
- selected source excerpts;
- public API, route, model, CLI, or test summaries;
- skipped-files and redaction reports.

Treat skipped files as blind spots.

## Output

Return a zip or clearly separated files with this structure:

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

## manifest.json

```json
{
  "schema": "tusker.onboard_plan/v1",
  "repo": "<repo name from packet>",
  "based_on_packet_schema": "tusker.onboard_packet/v1",
  "confidence": "low|medium|high",
  "warnings": [],
  "assumptions": [],
  "open_questions": []
}
```

## report.md

Use these sections:

```md
# Repository onboarding report

## What this repo appears to be

## Main user/product surfaces

## Main technical surfaces

## Build and verification surface

## Existing documentation quality

## Highest-risk unknowns

## Recommended Tusker domain map

## Recommended first epics

## What not to automate yet
```

## domains.yaml

Generate 3-8 domains. Use fewer if the repo is small.

```yaml
domains:
  - id: project
    title: Project
    status: draft
    summary: Repository-wide workflow, architecture, build, and ownership knowledge.
    source_of_truth:
      - README.md
    canonical_files:
      - INDEX.md
      - CANON.md
    read_when:
      - Read for repository-wide workflow and verification context.
    canon:
      current_truth:
        - <supported fact>
      invariants:
        - <durable rule supported by packet>
      verification:
        - <command or review path if known>
      deprecated_or_stale:
        - <known stale thing or empty>
      open_questions:
        - <unknown that matters>
```

## epics.yaml

Generate 3-10 epics. Use three-letter uppercase keys.

```yaml
epics:
  - key: FND
    title: Repository foundation
    priority: p2
    domains: [project, build]
    thesis: Make the repo understandable, verifiable, and safe for agent work.
    success_criteria:
      - Tusker setup validates.
      - Build/test commands are documented.
      - Project canon has no unsupported claims.
```

## tasks.yaml

Generate 5-20 initial tasks. These are backlog candidates, not ready execution contracts.

```yaml
tasks:
  - epic: FND
    title: Confirm build and test commands
    status: backlog
    readiness: held
    priority: p2
    risk: medium
    size: s
    domains: [build]
    intent: Confirm and document the smallest reliable commands for local and CI verification.
    acceptance:
      - id: A1
        outcome: The repo has a documented focused verification command for the main code path.
        proof: Command output summary or human-confirmed CI reference.
    verification:
      - covers: A1
        check: "<exact command if known, otherwise 'human confirms command'>"
        result: pending
        notes: Do not invent commands absent from the packet.
    non_goals:
      - Do not change product behavior.
    assumptions: []
    open_questions: []
```

Good task categories:

- verify build/test commands;
- review generated project canon;
- fill missing domain canon;
- document deployment/release process;
- map API/routes/data model;
- add missing focused tests for known critical flows;
- clarify ownership/security/privacy/release gates;
- reconcile docs/code mismatch.

Bad task categories:

- broad refactors without proof;
- "clean up codebase";
- tasks requiring product decisions without a human gate;
- ready tasks with fake verification.

## gates.yaml

Create gates only for missing facts that block safe planning or execution.

```yaml
gates:
  - title: Confirm production deployment owner
    gate_kind: decision
    owner: human:owner
    blocking: false
    covers: []
    action: Identify who owns production deployment approval and where release steps are documented.
    verification: Human answer recorded in project canon or an accepted decision record.
    why_agent_cannot: The onboarding packet does not contain authoritative release ownership.
    suggestion: Ask the maintainer or inspect private deployment docs if available.
```

## decisions.yaml

Use proposed decisions for apparent architecture rules.

```yaml
decisions:
  - epic: FND
    title: Treat README plus CI as initial build source of truth
    status: proposed
    rationale: The packet includes README and CI files but no stronger operations manual.
    supersedes: []
    evidence:
      - README.md
```

## docs-map.yaml

Map existing docs to likely Tusker domains.

```yaml
docs:
  - path: README.md
    domains: [project]
    quality: current|partial|stale|unknown
    notes: <short note>
```

## assumptions.md

List every meaningful assumption, why it was made, and what would verify it.

## open-questions.md

List questions in priority order. Separate blocking questions from nice-to-have questions.

## Final Check

Before returning, verify:

- every factual statement points back to a packet path or is labeled as assumption/question;
- every task is backlog and held;
- every task has acceptance with proof;
- every verification item is exact known command or honestly labeled human/manual proof;
- no generated content contains secrets, env values, raw logs, or private user data.
