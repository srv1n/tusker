---
capsule:
  what: "Give a PM a small, provenance-aware runner setup flow: choose semantic roles first, inspect exact model overrides only when needed, and keep automation off until separately enabled."
  use_when:
    - "Replacing the current mock-only Serve profile and routing settings with a real, safe project setup flow."
  skip_when:
    - "Changing daemon lifecycle, dispatch policy, scheduled promotion, release, spending, or the factory-operations surface."
---

# PM-first runner setup

Status: proposed implementation contract
Date: 2026-07-27

## Product outcome

A PM can open one registered project in Serve and answer the useful question:
"when I eventually enable this project, what kind of agent will plan, implement,
review, and repair work?" They see what this machine can actually run, choose a
small semantic policy, and can inspect or deliberately override the exact
harness/model/effort behind a role.

This configures *policy*, not authority. A project remains unable to auto-spawn
until the existing explicit **Enable automation** control is used. Runner setup
must never turn it on, start a daemon, arm a wave, dispatch a run, move a ref,
or spend money.

## The PM flow

1. **Setup** opens with a plain-language readiness summary: catalog freshness,
   currently configured roles, and whether automation is off or on. It does not
   bury the user in raw runner inventory.
2. **Choose roles** is the normal surface. Four cards are enough: Plan,
   Build routine work, Build hard work, and Independent review. Each card
   selects an existing semantic profile and shows its short consequence
   (harness, model, effort, permission posture, and provenance).
3. **Advanced** is collapsed by default. It permits an explicit per-role
   profile definition and a bounded ordered routing-rule table. It shows the
   chosen exact model only after the user asks for that detail; it does not
   invent a generic cross-provider "model priority" or price-ranking control.
4. **Preview a route** is read-only. Given an existing task and lane, it shows
   the same precedence chain as `tusker runner route`, including the winning
   role/profile, model/effort, source, and any validation blocker. It cannot
   claim or queue the task.
5. The existing **Auto-spawn eligible tasks** switch remains visibly separate
   and retains its current confirmation/authority semantics. Setup success must
   say "policy saved; automation unchanged" rather than implying that work has
   started.

## Requirements

| ID | Outcome |
| --- | --- |
| R1 | Serve has one real read model for a registered project: resolved semantic profiles, routing rules, provenance/inheritance, local harness catalog observations, automation state, and explicit profile/routing validation errors. |
| R2 | Serve has narrowly allowlisted project-local write operations for semantic role bindings, complete profile definitions, and routing rules. Every accepted write validates and persists in `tusker.local.yaml`, preserves unrelated configuration, returns resolved readback/provenance, and proves `automation.enabled` was unchanged. |
| R3 | The normal Setup UI is progressive: semantic role choices and their consequences first; catalog/provenance and exact per-role model details are inspectable; raw profile editing and bounded routing rules sit under Advanced. Existing mock profile/routing rows are removed from this path. |
| R4 | Route preview calls a read-only API equivalent to the CLI decision. It surfaces the whole precedence chain and validation blockers, and test snapshots prove it does not mutate task, runtime, Git, wave, or automation state. |
| R5 | A registered disposable project proves the complete no-authority journey: observed catalog, semantic setup write/readback, advanced override/routing validation, route preview, and unchanged automation/daemon/runs/refs. |

## API contract

All endpoints below are project-scoped. They resolve a registered project by
ID and use its normal config layering. They are not global runner-management
APIs.

| Endpoint | Meaning | Authority |
| --- | --- | --- |
| `GET /api/projects/:id/runner-setup` | Catalog observations plus the resolved role/profile/routing policy, field provenance, automation state, and validation diagnostics. | Read-only. Catalog failures are isolated per harness. |
| `POST /api/projects/:id/runner-setup` | Atomically replace only allowlisted setup policy (`roles`, profile definitions, ordered routing rules), validate the resolved configuration, write project-local override state, then return the same read model. | Cannot accept `automation`, daemon, wave, release, budget, credential, or arbitrary config keys. |
| `GET /api/projects/:id/runner-setup/route?task=:task&lane=:lane` | Return the exact route-preview result and configuration provenance. | Read-only; delegates to the same resolver as `tusker runner route`. |

The role bindings deliberately map to existing deterministic policy:

| PM role | Existing policy target |
| --- | --- |
| Plan | `automation.lane_profiles.planning` when supported, otherwise an explicit documented setup default; never task mutation. |
| Build routine work | `execute-fast` semantic profile. |
| Build hard work | `execute-complex` semantic profile. |
| Independent review | `review-independent` semantic profile. |

Before implementation, the API task must resolve the one current schema detail
that is not yet a durable configuration field: whether the Plan card maps to a
new validated lane binding or an existing default/profile binding. The exposed
read model and write payload must use the resolved canonical representation;
the UI must not paper over it with its own mapping.

## UX rules

- Names lead; provider/model/effort are supporting detail. A PM should be able
  to set a sensible default without knowing model-family branding.
- Every value says where it came from: built-in, user-global, committed project,
  or this machine's local override. Reset removes only the local override.
- A catalog entry reports its observation source and confidence (`live`,
  `bundled`, `declared`, or failed). It never claims provider availability that
  this machine did not observe.
- Advanced routing is an ordered, explainable exception list with a preview,
  not a general-purpose policy language or a pricing optimizer.
- Errors remain inline and actionable. A missing executable belongs in runner
  health/setup feedback, not a confusing empty model selector.

## Non-goals

- No generic model-priority, cheapest-model, provider-ranking, or cost-control
  UI.
- No new Plan 21 factory-operations states, no Ops redesign, and no merge-train
  work.
- No automation enablement, daemon management, runner health remediation,
  dispatch, wave start, review dispatch, landing, promotion, release, budget
  authorization, credential setup, or Git mutation.
- No rewriting committed `tusker.yaml`; this slice writes only project-local
  setup overrides and makes their provenance obvious.
- No task authoring or task-contract model fields beyond consuming an existing
  task for read-only route preview.

## Verification

Focused Go tests cover: catalog/config/provenance response shape; rejected
write payloads; atomic local profile/routing updates; unchanged automation;
route-preview equivalence and state inertness; per-harness catalog failure.

Focused Serve UI tests cover: semantic-role-first rendering, advanced collapse,
provenance/readback, the explicitly separate automation control, route-preview
results, and absence of the legacy mock profile list/model-priority language.

The final disposable-project proof records no daemon launch, no run, no wave
arm, no ref movement, and `automation.enabled: false` both before and after
setup changes.
