# Documentation for humans and agents

Agents are readers, but they are not human readers. Treat them as competent but brittle operators: fast, literal, context-limited, and prone to inventing missing glue.

The right move is not to create a parallel universe of “AI docs” divorced from human docs. The right move is to make source-of-truth documentation clean enough for humans, then add agent-facing contracts where prose is too ambiguous.

## Human vs agent needs

| Need | Human documentation should provide | Agent documentation should provide |
|---|---|---|
| Learning | confidence, pacing, visible progress | reproducible sandbox path and expected state transitions |
| Task execution | flow, decision support, caveats | deterministic runbook, exact commands, preconditions, postconditions |
| Facts | readable specs and examples | parseable schemas, stable names, defaults, limits, error mappings |
| Understanding | mental models and trade-offs | invariants, decision rules, architectural constraints |

## Applying Diátaxis to agent docs

### Agent tutorial

Use sparingly. This is for a sandboxed, repeatable path that lets an agent establish competence with a tool or workflow.

Good for:

- “Set up a local test project and make one successful API call.”
- “Run a safe dry-run deployment in a fixture environment.”

Not good for:

- Production operations.
- Generic onboarding prose.

### Agent how-to guide / runbook

Use for tasks an agent should execute.

Must include:

```yaml
task: ""
preconditions: []
inputs: []
permissions_required: []
steps: []
validation: []
rollback: []
failure_modes: []
escalate_when: []
```

### Agent reference

Use for facts the agent must consult while working.

Must include:

```yaml
version: ""
source_of_truth: ""
interfaces: []
commands: []
schemas: []
defaults: []
limits: []
error_codes: []
examples: []
last_reviewed: ""
stale_when: []
```

### Agent explanation

Use for domain logic the agent needs to reason correctly.

Examples:

- Why refunds must be created before shipment cancellation.
- Why a migration requires a read-only window.
- How customer tenancy boundaries affect data access.

Keep it bounded. Agents need the decision rule more than the essay.

## Agent capsule pattern

For dual-use pages, add a compact section near the end or in frontmatter:

```markdown
## Agent capsule

mode: how-to
reader: agent
source_of_truth: path/to/schema-or-code
preconditions:
  - User has admin permission.
  - Account is not suspended.
inputs:
  - account_id
  - plan_id
steps:
  - Validate account state.
  - Call PATCH /accounts/{account_id}/plan.
  - Poll GET /accounts/{account_id} until plan_id matches.
validation:
  - HTTP 200 from PATCH.
  - GET response shows expected plan_id.
failure_modes:
  - 403: ask user for admin permission.
  - 409: account has pending plan change; do not retry blindly.
stale_when:
  - Billing API version changes.
  - Plan migration rules change.
```

## `AGENTS.md` pattern

Use an `AGENTS.md` file when agents need repository-level instructions.

Recommended sections:

```text
# Agent operating context

## Scope
## Project invariants
## Source-of-truth files
## Commands
## Test/validation policy
## Documentation policy
## Safe actions
## Actions requiring confirmation
## Failure handling
```

Avoid dumping every policy into `AGENTS.md`. Link to focused reference files. Context bloat is real.

## Machine-readable sidecars

For high-risk workflows, pair prose docs with YAML or JSON sidecars.

Example:

```yaml
id: rotate-api-key
mode: how-to
reader: agent
inputs:
  service: string
  environment: enum[dev, staging, prod]
requires_confirmation: true
preconditions:
  - current_key_exists
  - replacement_key_created
steps:
  - update_secret_store
  - deploy_service
  - verify_healthcheck
  - revoke_old_key
rollback:
  - restore_previous_secret_version
escalate_when:
  - healthcheck_fails_after_3_attempts
  - production_secret_missing
```

## Failure modes in agent docs

- Ambiguous verbs: “check,” “handle,” “make sure.” Replace with observable checks.
- Missing defaults: the agent chooses randomly or copies old behavior.
- Hidden permissions: the agent reaches for tools it cannot use.
- No validation: the agent completes steps without proving state changed.
- No stale trigger: the doc survives after the API changed.
- Human-only prose: the agent misses constraints buried in paragraphs.
- Agent-only contract with no explanation: humans cannot maintain it.

## Good dual-output pattern

```text
Human document
├── Purpose and reader context
├── Normal human-facing content
├── Examples
├── Links to adjacent Diátaxis modes
└── Agent capsule or sidecar link
```

```text
Agent sidecar
├── Stable ID
├── Mode
├── Source of truth
├── Preconditions
├── Inputs and outputs
├── Procedure or schema
├── Validation
├── Failure modes
└── Stale triggers
```

Do not let agent docs become a prompt graveyard. They are operational documentation and should be maintained with the same seriousness as API reference.
