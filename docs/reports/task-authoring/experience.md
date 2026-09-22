# Task authoring and execution experience

This report records the UI contract for task authoring, review and execution. It is an implementation handoff and source-level behavior matrix; it does not claim a built-browser or live-provider qualification.

## The contract an agent must be able to read

Before a start action is available, the product should make the following facts readable from the same projection that the start action will use:

| Fact | Task detail | Inspector | Wave detail | Meaning |
|---|---|---|---|---|
| Requested tier | `Tier` in **Routing** | `Tier` in **Routing** | Tier beside each member | The authored work level. Review inherits it unless explicitly overridden. |
| Worker route | `Will execute` | `Will execute` | `Will execute` per member | Effective profile, model, effort and harness selected by the route resolver. |
| Reviewer route | `Will review` | `Will review` | `Will review` per member | Effective review route, including a deliberate review override. |
| Route provenance | `Source`, reason and fallbacks | Same fields | Same fields per member | Whether the route came from an explicit override or inherited tier/configuration. |
| Admission blockers | Exact worker/reviewer blockers plus Settings link | Route warning plus exact route facts | Authorization, fingerprint, route and human-action blockers | A missing or disabled mapping cannot look runnable. |
| Intended work | Task intent and acceptance | Task intent and evidence disclosures | Expected outcome plus expandable full wave brief | The worker receives enough surrounding context to act without opening YAML. |
| Human responsibility | Human action card | Human action card | Named action and owner when supplied | Human-owned work has a control and never exposes an LLM Play affordance. |

The canonical authored task body is available as **Full task contract** on both task surfaces. It is open on full task detail and collapsed in the Inspector, so implementation notes, surrounding context and non-goals remain readable without replacing the structured acceptance and proof status views.

The shared route formatter lives in [`TaskScreens.tsx`](../../../internal/serve/ui/src/features/product/TaskScreens.tsx). Task detail and Inspector render both server-derived route facts, including source/reason/fallbacks and exact lane blockers; blocked routes link directly to project Settings. Wave detail reuses the same formatter and tier labels instead of maintaining a second model or tier mapping. `effectiveExecute` and `effectiveReview` are server-derived facts; profile eligibility in Settings is not a substitute for a selected route.

Work-tier input is `Unclassified` while no authored tier exists; the UI does not label that state as a default. A review tier that is explicitly different from the selected/effective worker tier reveals a reason field and cannot be newly saved blank. Existing historical overrides with no recorded reason remain saveable when their classification is untouched, so migration does not rewrite old task intent.

## Identity and coordination

Task detail and Inspector share the coordination summary. It shows:

- architect and origin provenance, or `Unbound provenance` when the source is not bound;
- the actual executor route for the latest attempt, or `Interactive implementation` for a hand-run attempt. A hand-run marker alone does not prove an architect self-claim;
- workspace mode, path and branch from the recorded attempt binding;
- typed architect/origin/peer contacts, their address kind/id, and the current binding state/reason when the identity projection is available;
- message state and transport state, including `sender yields` for a question that asks the sender to release until answered.

The UI must keep these facts separate. An authoring conversation is provenance, a contact is a possible route, a configured Play profile is a requested execution route, and a hand run is an actual executor claim. A missing binding stays visible as missing. The UI does not silently create a conversation or infer an executor from the current browser.

When a conversation should perform the implementation itself, both task detail and Inspector show a copyable `tusker work start <task-id> --by current-conversation --current-workspace` command. This is an explicit host-bound work claim and is separate from configured Play; the command must run inside the trusted current Codex or Claude session. The UI never invokes it from the browser.

## Human gates

The task projection supplies `humanAction`/`humanActions` and the gate projection supplies the named owner. Task detail renders the existing [`HumanActionCard`](../../../internal/serve/ui/src/features/human-action/HumanActionCard.tsx), including native receipt, rework and waive/obsolete controls. The task start action is suppressed while a human action is open. Wave detail lists the action and affected tasks, and suppresses wave Play while that action is outstanding.

Human controls remain separate from agent access approvals. An approval request is a permission decision for an agent attempt; a human gate is work that only the named human can perform.

## Verification matrix

| Scenario | Expected visible behavior | Evidence status |
|---|---|---|
| Configured task | Tier and both effective routes show profile/model/effort/harness, with source and access. | Source implementation; built-browser check remains with FLW-T-0041. |
| Empty or disabled route | Route says blocked, names the lane and blocker, links to Settings, and does not expose Execute once. | Focused UI test passes; built-browser check pending. |
| Stale or missing wave authorization | Wave Play is disabled and names the authorization/fingerprint action. | Source implementation; live refresh behavior pending. |
| Human action | Owner/action and affected work are visible; human control is available; LLM Play is absent. | Task owner derives from gate projection; wave owner is shown when the wave brief supplies it. |
| Standalone task | Task detail exposes the same route and gate checks without requiring a synthetic wave. | Source path exists; browser exercise pending. |
| Settings changes after a run | Latest attempt keeps recorded runner/profile/model/effort/harness and binding facts. | Runtime projection is consumed; refresh qualification pending. |
| Architect question | Contacts, recipient, reply path, queue/transport state and optional sender yield are visible. | Request field is wired to the existing `/messages` seam; resumable provider behavior remains a runtime qualification. |
| Narrow-screen/keyboard use | Details disclosures, selects, buttons and human controls remain reachable without a hidden required field. | Source structure and typecheck pass; browser proof pending. |

## Remaining contract boundaries

The UI deliberately leaves these decisions to their existing owners:

1. The backend must project final trusted authoring provenance and any current-conversation claim facts. The UI should consume those fields and keep unbound state explicit; it should not invent `threadId`, `isArchitect` or a browser-side self-claim.
2. A wave human action needs an owner in the wave brief if the wave surface is to display the owner without fetching every task detail. Until then, it uses the honest `Human owner` fallback and the task gate remains the detailed authority.
3. `Play` queues the admitted configured route through the existing daemon seam. A current-conversation implementation claim is a separate explicit work-session operation and must not be represented as configured Play.
4. Source tests can prove projection and rendering branches. They cannot prove native provider continuity, a live daemon receipt, or a human confirmation. Those remain separate installed/browser/runtime gates.

The integrated UI build, typecheck and six focused experience cases pass. The browser walkthrough remains a separate FLW-T-0041 qualification lane.
