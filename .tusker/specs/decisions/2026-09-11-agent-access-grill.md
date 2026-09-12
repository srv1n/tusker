---
subject: 2026-09-11-agent-access-grill
title: "Agent access decisions — 11 September 2026"
keywords: [permissions, profiles, trust, private folders, native Muse, HIG]
part_of: agent-access
status: canonical
created: 2026-09-11
read_when: "Understanding the operator's permission goals and the reasoning behind the access specification."
skip_when: "Implementing the agreed scope; read agent-access."
decides_for: .tusker/specs/agent-access.md
---

# Agent access decisions

This records the actual discussion. The operator requested a specification and stories, and subsequently emphasized that other agents will build it. No product implementation, worker launch or automation change belongs to this session. The discussion already establishes the goals; proposed interface labels and field names are authored design choices, not separately approved operator answers.

## D1 — A common contract across installed agents

Question raised by the operator: How do CLI execution and later ACP share permissions when Codex, Claude Code, Hermes, Muse and future providers expose different controls? Should profiles own this?

Options discussed: A universal permissions ladder; a profile containing provider-specific controls; shared intent translated through a capability-aware adapter.

Operator intent: Adding Devin, Grok, OpenCode or another trusted harness should require onboarding methods and a few common tests, without recurring architectural changes.

Required result: Profiles remain the user-facing authority. A small shared access contract, exact-route capabilities and adapters expose the provider's actual coverage. Transport is independent of permission intent. The bounded schema and effective report are implementation recommendations.

## D2 — Fast project work with exceptional approvals

Operator example: Codex and Claude are often run with full access for low friction. In a project repository, the operator wants work and network access to proceed without constant questions. They want to avoid accidental damage elsewhere and drastic commands such as root-level recursive deletion.

Recommendation: Default new profiles to Work in projects; allow routine work/network; ask for recognized destructive project actions and deny catastrophic targets.

Required result: Normal coding work does not require repeated permission clicks. Existing full-access choices retain their current meaning unless deliberately changed. The exact two ordinary mode names and Ask/Block controls are design choices within this brief.

## D3 — Reference projects, temporary files and private folders

Operator example: Agents often need to consult another project's code and use a temporary folder. Some folders on the laptop should remain out of their reach.

Recommendation: Read-only references by default, explicit write grants, an attempt-owned temporary directory and shared private-folder exclusions.

Required result: Express these choices without per-task repetition and without silent permission widening. Report limits in native coverage.

Authored default: The private-folder list starts empty with an honest empty state. Do not guess that Downloads or Documents are entirely private; the current project lives under Downloads. Personal exclusions must be selected, and legacy bypass profiles that do not apply them are listed explicitly.

## D4 — Trusted native controls, no new OS sandbox platform

Earlier concern: The operator trusts Muse less than other harnesses and wants to keep personal data away from it.

Later clarification: They still have a baseline of trust in harnesses they onboard. They want to balance complexity and speed, and explicitly want to avoid building OS-level sandboxes.

Options discussed: Native rules/hooks; OS containment; advisory instructions alone.

Required result: Use native controls and state their scope honestly. Do not promise malicious-client isolation or universal script inspection. An advisory instruction does not satisfy an operative selected restriction. Missing required support is visible, not silently downgraded. No new OS containment infrastructure is part of this delivery.

## D5 — Native Muse is a separate route

Operator correction: Muse can also run through its own CLI; the Codex route is not its only possible route.

Evidence: The installed Muse client exposes direct exec and a serve endpoint using MSP. Current Tusker model discovery can use Muse directly, while its existing Muse execution profile routes through Codex.

Required result: Preserve the old route and add a distinguishable direct CLI adapter. Do not call MSP ACP. The new internal ID muse_cli and the display label Muse via Codex are proposed compatibility choices.

## D6 — HIG and a small product surface

Operator request: Take the complexity and make a simple interface and schema, wired end to end with very good defaults; write the spec and stories so other agents can build it.

Recommendation: Reuse Settings → Agents → Profiles, a short Access details disclosure, shared private-folder rows and existing task human-action cards. Save configuration independently of paid test execution. No new permissions navigation area, generic rule editor or wizard.

Required result: The spec includes normal/error states, accessible interaction, schema, native mappings, approval lifecycle, migration and buildable acceptance artifacts. No implementation or worker dispatch in this session.

## D7 — Deliberate implementation limits

These are authored scope choices, not claimed operator quotations: two ordinary access modes; preserve legacy unrestricted settings rather than inventing a new universal bypass; one-time approvals only; no model classifier; no terminal-prompt scraping; direct Muse CLI now, MSP/other providers later; provider-free qualification before explicitly authorized live usage.

Reason: Reuse current authorities and make the first delivery complete without speculating about an enterprise policy platform. Revisit a deferred item when an actual onboarded route or user journey requires it.

## D8 — Automatic work and visible protected folders

After rebuilding the evolving UI, the operator repeatedly reported confusing defaults, duplicated access layers and too much implementation language. Their absolute use case is to execute with as little friction as possible while excluding selected personal folders. They asked for a redesigned spec and Tusker handoff for a junior agent, then explicitly delegated the remaining defaults while stepping away.

Authored decision: new profiles run routine project work and internet automatically, with recognized destructive actions blocked without prompting. Advanced can select Ask before running. This supersedes D2's recommended ask-by-default only for newly created profiles; it does not authorize silent changes to existing profiles or automatic destructive execution. The optional question about destructive behavior received no selection; this is the planner's choice under the subsequent delegation, not a fabricated user answer.

The main form shows Agent, Model, Access, Protected folders, one compatibility state and Save/Cancel. Protected folders remain visible and editable; private paths use the existing shared/profile authority. Technical mechanisms, reasoning, reference roots, tiers, native presets and tests belong under Advanced. No permission dashboard, extra ladder, general regex editor or new OS sandbox.

Required behavior: draft changes apply only on explicit Save, survive errors, and are reversible with Cancel. Setup is bounded and local, computed from the exact draft, and never starts a model turn. Unsupported selected restrictions prevent execution without dropping the folder list. Unknown coverage is not shown as protection. Existing native route qualification remains a separate completion requirement; a polished UI is not proof that Codex, Claude or Muse can enforce this combination.

Architect/origin is the local Codex task “Design unified sandbox permissions”, native thread ID `01a08f12-a67e-7920-a511-9357daec477c`, host `local`, inspected through read_thread on 11 September 2026. No Tusker task/execution contact resolving this conversation was verified. Return unresolved design/ownership questions to Sarav through the implementation task for relay to this conversation; do not invent a routable execution ID. No message or live reply route was exercised.

## Follow-through

Canonical contract: [[agent-access]]. The adjacent delivery plan records the stories and real prerequisites. Current system documents are updated by the final implementation story to verified shipped behavior, not rewritten now as if the feature exists.
