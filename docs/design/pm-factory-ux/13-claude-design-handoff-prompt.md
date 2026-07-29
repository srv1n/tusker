---
schema: tusker.design-note/v1
kind: handoff
status: proposed
authority: informative
date: 2026-07-28
parent: "[[00-index]]"
related:
  - "[[02-swiss-design-system-and-attention]]"
  - "[[03-shell-and-today]]"
  - "[[04-plan-and-authorization]]"
  - "[[12-implementation-and-acceptance]]"
tags:
  - tusker/ux
  - tusker/handoff
---

# Claude Design handoff prompt

Copy the prompt below and attach or provide repository access to
`docs/design/pm-factory-ux/`.

---

You are designing the complete PM-first desktop experience for Tusker, a local
automatic software factory.

Read every Markdown document under:

`docs/design/pm-factory-ux/`

Begin with `00-index.md`. Treat the pack as the proposed normative product,
interaction, API, daemon, settings, and guardrail contract. Executable code and
accepted specs describe current behavior; do not assume every proposed screen
already exists.

The user is a founder, PM, designer, or technical product owner. They own
requirements, acceptance, subjective decisions, and explicit authority. They
must not need to manage task IDs, waves, worktrees, runners, leases, gates,
fingerprints, refs, YAML paths, or hashes during normal use.

The experience must use:

- a Swiss grid system;
- strict and scarce attention mechanisms;
- progressive disclosure;
- strong typography and alignment;
- clean, calm, editorial presentation;
- product language before control-plane language;
- at most three major visible groups on a normal screen;
- one obvious primary action;
- technical truth reachable without cluttering the default path.

The canonical primary navigation is:

1. Today
2. Plan
3. Deliveries
4. Knowledge

Settings and Diagnostics are utilities. Do not restore Overview, Work, Runs,
Delivery, Docs, and Ops as peer navigation.

Critical UX correction: a PM must never paste a delivery-plan YAML path or copy
a fingerprint/SHA to start work. Plans appear in an indexed inbox. The user
reviews outcomes, proof, exclusions, and flow, then chooses “Start delivery.”
The backend binds and revalidates the exact identity.

Do not weaken these guardrails:

- registration, project automation, delivery authorization, staging,
  promotion, release, paid triage, and spend are independent boundaries;
- fresh projects remain automation-off;
- objective review is independent and read-only;
- default branch moves only after exact full-green proof;
- release is separately authorized through a named locked profile;
- a human override is named, exact-revision-bound, reasoned, permanent, and
  never disguised as review proof;
- the normal Mac path is supervised by TuskerBar’s bundled resident daemon;
- routine runtime details belong in Diagnostics.

Deliver:

1. A concise interpretation of the product model and any contradictions you
   found. Do not silently resolve authority contradictions.
2. The final information architecture and route map.
3. End-to-end journey maps for:
   - first project setup;
   - plan discovery, review, and start;
   - active delivery monitoring;
   - artifact-first completion;
   - genuine human decision;
   - runner/infrastructure repair;
   - enabling automation and configuring model roles.
4. Low-fidelity wireflows for every screen in the index inventory.
5. High-fidelity desktop frames at 1440 px and compact desktop frames at
   1024 px.
6. Narrow/responsive behavior at 768 px and for a small notification/decision
   companion.
7. Component inventory with variants, states, and content rules.
8. Designed states for loading, refreshing, empty, stale, offline, partially
   degraded, permission-refused, invalid plan, failed mutation, and destructive
   confirmation.
9. Prototype flows for:
   - review and start a valid delivery;
   - a plan that changes during confirmation;
   - resolve a human decision;
   - repair a broken runner executable;
   - inspect a completed delivery’s artifacts and exact proof.
10. Accessibility annotation: focus order, keyboard behavior, screen-reader
    state copy, zoom/reflow, contrast, and reduced motion.
11. A mapping from every visible action to the API/authority contract in
    `09-api-and-state-contracts.md` and
    `10-guardrails-authority-and-confirmations.md`.
12. A short list of product decisions that genuinely require us before
    implementation.

Use realistic long titles, multiple projects, one healthy active delivery, one
runner failure, one changed plan, one human decision, and one completed
artifact set. Do not design only the happy path.

You may challenge layout, visual treatment, component choice, density, and
responsive behavior. Have a take. You may not invent a second source of truth,
hide real risk, or turn technical ceremony into a PM task.

Do not implement code in this engagement. Return the complete design system,
screen set, prototype behavior, and developer annotations needed for a
follow-up Tusker implementation plan.

---

## Expected follow-up

After design review, update [[12-implementation-and-acceptance]] with the
approved component/route decisions and convert each vertical phase into a
reviewed Tusker V2 dependency DAG. Do not convert unresolved design questions
into implementation guesses.
