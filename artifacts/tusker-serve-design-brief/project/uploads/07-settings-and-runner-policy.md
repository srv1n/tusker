---
schema: tusker.design-note/v1
kind: spec
status: proposed
authority: normative
date: 2026-07-28
parent: "[[00-index]]"
related:
  - "[[04-plan-and-authorization]]"
  - "[[08-daemon-diagnostics-and-recovery]]"
  - "[[09-api-and-state-contracts]]"
  - "[[10-guardrails-authority-and-confirmations]]"
tags:
  - tusker/ux
  - tusker/settings
  - tusker/runners
---

# Settings and runner policy

## Design goal

Settings should answer “what will Tusker do for this project?” before exposing
“which YAML field implements it?”

The target structure is:

```text
Project Settings
├── Basic
│   ├── Automation
│   ├── Capacity
│   ├── Model roles
│   └── Notifications
└── Advanced
    ├── Project & repository
    ├── Runner profiles and routing
    ├── Permissions
    ├── Workspace
    ├── Review and completion
    ├── Integration and promotion
    ├── Release
    ├── Budgets and retries
    └── Configuration sources
```

App Settings uses the same grammar for global defaults, machine runner
discovery, app behavior, and notification delivery.

## Settings interaction contract

Every row must provide:

- plain-language title;
- one-sentence consequence;
- effective value;
- source/provenance when inherited or discovered;
- reset to inherited when overridden;
- validation before persistence;
- impact preview for authority or runtime changes;
- machine-readable readback after save.

### Provenance

| Source | Meaning |
|---|---|
| Built in | Safe Tusker default |
| App default | User’s machine-wide preference |
| Project | Shared project configuration |
| This Mac | Machine-only local override |
| Discovered | Read-only runner/repository fact |
| Policy | Locked by a higher authority or release profile |

Editing inherited configuration creates the narrowest appropriate override.
The UI never edits a shared file while claiming the change is local.

### Persistence

- Cosmetic local preferences save immediately.
- Low-risk project settings validate and save immediately with undo.
- Automation, permission, promotion, release, and budget authority changes use
  a staged review sheet.
- A mutation returns the effective value and provenance; optimistic UI alone is
  insufficient.

## Project Settings — Basic

### Automation

| Setting | Default | PM copy and behavior |
|---|---|---|
| Background work | Off on fresh registration | “Let Tusker pick up authorized work for this project.” Registration never changes it. |
| Authorized scope | Armed deliveries only | Choices: Authorized deliveries only; All eligible tasks (Advanced warning). |
| Completion handling | Deterministic when enabled by policy | Choices exposed only when supported: Off, Observe, Complete automatically. |
| Pause | Not paused | Stops new claims/departures; explains behavior of live work. |

The Background work toggle does not start a second daemon when TuskerBar owns
the resident runtime. It changes project eligibility only.

### Capacity

| Setting | Default | Behavior |
|---|---:|---|
| Concurrent tasks | Project default, recommended 2 | Number of task workspaces this project may run concurrently. |
| Live workspaces cap | Off unless measured | Hard per-project disk-safety cap; Advanced if unset. |
| Fair-share priority | Normal | Advanced options only; never imply dedicated capacity. |

Show the effective global ceiling as read-only context.

### Model roles

Present four semantic roles, not a generic priority list:

| Role | Purpose | Suggested default behavior |
|---|---|---|
| Plan | Turn approved intent into a delivery DAG | Strong planning model, higher reasoning |
| Build routine | Small, well-scoped implementation | Efficient capable model |
| Build hard | Ambiguous, cross-cutting, or high-complexity implementation | Stronger model/effort |
| Independent review | Read-only objective review | Distinct reviewer profile; strong enough for risk |

Each role row shows:

- selected profile/model display name;
- harness;
- effort;
- permissions summary;
- why it was selected: user-configured, generated default, or fallback.

Actions:

- **Use recommended setup** — generated from installed capability discovery;
- **Customize** — opens exact profile picker;
- **Preview routing** — enter/select a representative task and see role,
  profile, policy source, and precedence.

Tusker may classify task complexity from the task contract and route to Routine
or Hard. Planning models may recommend complexity, but the classification is
visible and overrideable in the task/plan. No provider is assumed permanent.

### Notifications

Basic shows:

- Human decision reminders: on/off;
- promotion red: on;
- release failed: on;
- spending hold: on;
- delivery method: macOS, in-app, or both.

Green delivery remains quiet by default. Stalled infrastructure escalation may
be enabled in Advanced.

## Project Settings — Advanced

### Project and repository

| Key | Control | Notes |
|---|---|---|
| Display name | text | UI-only display; project identity remains stable |
| Project ID/key | read-only | Canonical identifier |
| Local repository path | read-only/change registration | Derived from registry |
| Vault/control root | read-only/change registration | Derived and validated |
| Origin remote | read-only | Derived from Git |
| Default branch | branch picker | Used as work/integration base; changing requires impact check |
| Configuration schema/version | read-only | Migration action if unsupported |
| Knowledge roots | directory list | Repo-relative allowlist |

### Runner discovery

Read-only catalog of installed harnesses and capabilities:

- provider/harness;
- executable and source;
- version/build;
- available models;
- supported effort levels;
- permission/sandbox modes;
- subagent support and limits;
- health;
- observed time and confidence;
- fallback source.

Discovery never dispatches work and never silently changes an authorized
executable. “Use this executable” is an explicit local policy change.

### Runner profiles

Each profile binds:

| Field | Meaning |
|---|---|
| Name | Stable profile identifier |
| Harness/provider | Installed runner adapter |
| Model | Provider model identifier |
| Effort | Supported reasoning/compute level |
| Permission preset | Exact workspace/network/approval boundary |
| Subagent policy | Disabled or maximum allowed |
| Command/executable | Resolved machine policy; exact path in technical detail |
| Environment/PATH policy | Explicit allowlisted additions, no shell-profile assumption |
| Timeout/read/stall/max turns | Bounded runtime behavior |
| Cost/launch class | Quota class; not fake dollar precision |
| Role eligibility | Plan, routine, hard, review, triage |
| Health requirements | Required capabilities/version |

Built-in/generated profiles are cloneable. User profiles are editable. A
profile referenced by an active authorization is versioned rather than mutated
in place.

### Routing

Precedence is visible and deterministic:

1. exact task contract/profile;
2. semantic role from explicit task complexity/lane;
3. ordered project routing rules;
4. project role/profile mapping;
5. app default;
6. bundled fallback only when truthful and supported.

Rule matchers may include:

- lane: plan, execute, review, repair, triage;
- complexity;
- risk;
- epic/workstream;
- labels/keywords;
- artifact/toolchain needs;
- required capabilities.

Every rule shows examples and conflicts. A read-only route preview must explain
the selected role/profile and source without claiming or dispatching.

### Permissions

Presets:

| Preset | Contract |
|---|---|
| Workspace only | Writes confined to execution workspace; network separately controlled |
| Guarded full access | Broad filesystem/network under immutable and user deny rules |
| Full access | No sandbox/approval where the runner supports it; high-risk warning |

Settings:

- network access;
- approval policy;
- filesystem roots;
- protected paths;
- secret/credential denial;
- built-in denylist, read-only;
- custom denylist;
- external connector/tool allowlist;
- destructive command policy;
- production/environment access;
- permission expiry.

Repository configuration cannot weaken immutable machine safety or grant
release authority by itself.

### Workspace

| Setting | Meaning |
|---|---|
| Strategy | Shared or isolated Git worktree; automation should default to isolated |
| Workspace root | Machine-local root |
| Maximum live worktrees | Measured per-project hard cap |
| Setup hooks | Named, reviewed commands after workspace creation |
| Cleanup hooks | Named, reviewed commands before removal |
| Local files to copy | Explicit patterns; secret rules still apply |
| Port allocation | Base/range or provider |
| Retention | Delete on landing, retain failures for bounded time, manual |
| Dirty-tree policy | Refuse or explicit controlled exception |
| Registration policy | Canonical write-through; never invisible branch-local records |

Arbitrary task frontmatter cannot inject hooks. Hook changes are policy changes
and show command/permission impact.

### Review and completion

| Setting | Choices |
|---|---|
| Independent review | Off; configured role/profile; required by risk/contract |
| Reviewer edits | Always off for independent review |
| Review iterations | Bounded maximum |
| Objective close | Deterministic after proof/gates/review |
| Completion reactor | Disabled, shadow, authoritative |
| Retry findings | Bounded rework iterations |
| Human gate policy | Explicit capability/authority/intent/subjective only |
| Human override authorities | Named local actors/groups; dedicated receipt |

### Integration and promotion

#### Integration

- isolated task landing;
- integration branch policy;
- serialized landing concurrency;
- auto-land after green review;
- conflict assistance on/off and profile;
- after-landing branch/workspace retention;
- batch/full-gate profile;
- shared resource locks;
- overlap protection, read-only if automatic.

#### Scheduled promotion

Monotone mode:

| Mode | Observe | Stage | Promote main | Release |
|---|---:|---:|---:|---:|
| Disabled | No | No | No | No |
| Shadow | Yes | No | No | No |
| Stage | Yes | Yes | No | No |
| Promote | Yes | Yes | Yes after full gate | Separately authorized |

Settings:

- mode, default Disabled;
- local daily windows and period fallback;
- hold/pause;
- full promotion gate profile;
- isolation provider;
- known infrastructure/flake patterns and flake action;
- optional paid ambiguous-failure triage, default Off;
- shadow parity/cutover status;
- singleton handling, informational and automatic.

Changing to Stage or Promote uses an impact review. Promote cannot be selected
without a valid full gate. Missing/malformed config resolves to Disabled.

### Release

- release enabled: separate, default Off;
- named locked release profile;
- target/environment;
- allowed revision: exact successfully promoted candidate;
- credential binding: host-owned reference only;
- approval authority;
- release windows/holds;
- success/failure verification;
- rollback/compensation profile;
- notification policy.

No arbitrary deploy command textbox. Models never receive release secrets.

### Budgets, limits, and liveness

| Setting | Contract |
|---|---|
| Max active runs | Positive global and project ceilings |
| Max attempts | Bounded task/runtime retry |
| Backoff | Explicit schedule |
| Max continuation retries | Bounded continuation |
| Launch quota | Honest count by profile/period |
| Paid triage quota | Separate count and hold behavior |
| Token/dollar display | Informational unless provider accounting is trustworthy |
| Lease TTL | Advanced machine policy |
| Progress/stall deadline | Distinguishes live from no-progress |
| Intentional wait deadline | Requires next wake and escalation |
| Retention | Attempts/events/artifacts by class |
| Disk floor | Measured gate/workspace safety requirement |

Exhaustion parks/escalates once. It never retries forever.

### Runtime service

Read-only in normal TuskerBar usage:

- app-owned daemon status;
- runtime version/build;
- service ownership;
- port/address in exact detail;
- event freshness;
- registry path;
- update/restart actions.

Manual service installation is an advanced CLI/server deployment path, not part
of the normal Mac preview workflow.

## App Settings

### General

- appearance: theme and density;
- startup: open at login, reopen last project;
- global default capacity;
- default workspace root and measured machine workspace ceiling;
- event/artifact retention;
- update channel;
- telemetry, if any, explicit and off unless policy says otherwise.

### Default model roles

Same four-role model as project Basic. Projects inherit and may override.
App setup can generate defaults from discovered harnesses, preserving explicit
profiles.

### Machine runner catalog and profiles

Manage discovered executables, explicit PATH additions, profiles, health,
versions, and fallbacks. Project screens reference these by stable profile
identity/version.

### Machine permissions

Immutable safety rules, global presets, built-in/custom denylist, network and
connector policy. Project policy may narrow but not silently broaden a locked
machine rule.

### Notifications

- delivery method;
- macOS permission status;
- in-app retention;
- human decision;
- promotion red;
- release failed;
- spending hold;
- infrastructure stalled;
- quiet hours/reminder delay for noncritical decisions.

### Project registry

- registered projects;
- repository/vault paths;
- enabled/disabled automation state;
- compatibility/migration;
- remove registration without deleting repository data.

## Impact review examples

### Enable automation

> Tusker may pick up tasks only from authorized deliveries in this project.
> It may use up to 2 workspaces. Promotion and release remain off.

### Enable promotion

> Tusker may move `main` only when the exact staged candidate passes the full
> gate. Release remains off. The next configured window is 02:00 local time.

### Change review model

> New reviews will use Opus 5 / high with read-only permissions. Two active
> reviews keep their versioned existing profile.

## Low-level configuration disposition

The UI must account for every effective workflow family even when a field is
read-only or Diagnostics-only. This table prevents “Advanced” from becoming a
euphemism for omitted behavior.

| Configuration family | Product surface | Default visibility |
|---|---|---|
| `workflow_version`, `tracker_schema_version` | Compatibility/migration | Diagnostics |
| tracker kind, dispatch/review/terminal states | Installed schema capability | Diagnostics, read-only |
| `automation.enabled` | Background work | Basic |
| dispatch scope | Authorized scope | Basic |
| completion reactor mode | Completion handling | Basic/Advanced |
| scheduled promotion version/mode | Integration and promotion | Advanced |
| release profile/authorized | Release | Advanced |
| paid model triage authorized | Integration failure handling | Advanced |
| enabled/default agents | Runner catalog/profile fallback | Advanced |
| global/by-state concurrency | Capacity and lane limits | Basic/Advanced |
| poll interval | Reconciliation cadence | Diagnostics, normally read-only |
| lease TTL | Runtime liveness | Advanced machine policy |
| project active-run cap | Concurrent tasks | Basic |
| continuation retry cap | Budgets and retries | Advanced |
| token budget fields | Diagnostic accounting only until trustworthy | Advanced, clearly non-enforcing |
| Serve enabled/address/docs roots | App runtime and knowledge roots | Advanced/Diagnostics |
| sentinel checks/heartbeat freshness | Runtime invariant protection | Diagnostics; expert edit only |
| workspace root/strategy/live cap | Workspace | Advanced; capacity summary in Basic |
| retry max attempts/backoff | Budgets and retries | Advanced |
| reviewer enabled/runner/actor/cycles | Review and completion | Advanced |
| reviewer auto-close/human-risk legacy lists | Review migration/policy | Diagnostics; normalize to objective-gate model |
| reviewer prompt | Installed runner instruction | Advanced technical |
| external-loop cycle/thread/wall-clock caps | External runner limits | Advanced |
| runner definitions | Machine runner catalog | App Advanced |
| runner profiles, sources, default | Model roles/profiles | Basic/Advanced |
| lane profiles and ordered routing | Model roles/routing | Advanced |
| runner deny rules | Permissions | Advanced |
| Codex/Claude command and timeouts | Runner profile exact details | App Advanced |
| Codex Cloud environment/apply/PR/collect | External runner profile | App Advanced |
| extension tools/MCP allowlists | Permissions/connectors | App Advanced |
| workspace create/remove hooks | Workspace | Advanced |
| fanout enabled/children/types/merge rule | Subagent policy | Advanced profile/orchestration |
| orchestration default branch | Repository | Advanced |
| branch age warning | Workspace/Diagnostics | Advanced |
| shared namespaces/lints | Integration conflict prevention | Advanced |
| batch gate enabled/period/windows/commands/profile/repairs | Integration validation | Advanced |
| gate profile/harvest/disk/locks/dirty policy | Integration validation | Advanced |
| defect parsing/excerpt limit | Gate diagnostics | Expert-only |
| infrastructure/flake patterns/action | Failure classification | Advanced |
| gate scopes | Selective proof mapping | Advanced |
| isolation provider | Full promotion gate | Advanced |

Unknown future configuration is preserved and shown under “Unsupported by this
app version,” with schema/capability guidance. The UI never drops fields during
a partial edit.

## API requirement

The UI needs typed read/write endpoints with provenance and impact preview;
current partial settings mutations and mock-only panels are insufficient. Exact
contracts are in [[09-api-and-state-contracts]].

## Acceptance

- Basic contains no YAML keys, runner command paths, or routing expressions.
- A PM can see and set role-based model policy.
- Current/inherited/source values are never ambiguous.
- Enabling automation does not enable promotion, release, triage, or spend.
- Advanced exposes every effective runtime policy with validation.
- Every authority change shows consequence and confirmed readback.
