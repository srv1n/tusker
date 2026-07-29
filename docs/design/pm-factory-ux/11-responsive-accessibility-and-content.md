---
schema: tusker.design-note/v1
kind: interface
status: proposed
authority: normative
date: 2026-07-28
parent: "[[00-index]]"
related:
  - "[[02-swiss-design-system-and-attention]]"
  - "[[03-shell-and-today]]"
  - "[[04-plan-and-authorization]]"
tags:
  - tusker/ux
  - tusker/accessibility
  - tusker/content
---

# Responsive, accessibility, and content

## Breakpoint behavior

| Width | Layout |
|---|---|
| ≥ 1440 px | Expanded shell, 12-column content, optional persistent context rail |
| 1024–1439 px | Expanded/compact shell, 12-column content, side sheets |
| 768–1023 px | Compact shell, single primary column, details as overlays/pages |
| < 768 px | Decision/monitoring companion: Today, attention, delivery status; complex DAG/settings editing may be read-only or simplified |

Responsive design preserves hierarchy; it does not stack every desktop panel
into a 10,000 px page.

## Per-screen responsive rules

- Today: three groups become one vertical editorial feed.
- Plan review: requirements/proof/flow remain sequential; authorization is a
  bottom sheet.
- DAG: list becomes default on narrow screens; graph is optional full-screen.
- Delivery detail: phase strip scrolls or becomes current/previous/next text.
- Knowledge: context rail becomes Backlinks/Related tabs below content.
- Settings: Basic/Advanced become a section index; each group is a page.
- Diagnostics: failed checks lead; technical tables become labeled rows.

## Accessibility baseline

- WCAG 2.2 AA minimum.
- Full keyboard operation.
- Visible focus with 3:1 contrast.
- 44×44 px minimum target for primary touch controls; 32 px compact pointer
  controls with adequate spacing.
- Semantic headings and landmarks.
- Tables use headers and captions.
- Dialog focus traps and returns to invoker.
- Live updates use restrained `aria-live`; routine progress is not announced
  continuously.
- Color is never sole state encoding.
- Reduced motion and increased contrast are respected.
- Zoom to 200% without loss of action or horizontal page scrolling, except
  intentionally scrollable DAG/table canvases.

## Keyboard model

- `⌘K` / `Ctrl+K`: search.
- `g t`: Today; `g p`: Plan; `g d`: Deliveries; `g k`: Knowledge, only when not
  typing and with a shortcut guide.
- `Esc`: close topmost sheet/dialog.
- `Enter`: open selected row.
- Arrow navigation only inside components that declare the pattern.
- Destructive confirmation never relies on a shortcut alone.

## Screen reader content

- Announce state changes as complete phrases: “Delivery moved to Checking.”
- Progress has label/value, not only visual segments.
- DAG exposes an equivalent ordered task/dependency list.
- Artifact thumbnails have outcome-oriented alt text.
- Status icons are hidden when adjacent text is equivalent.
- Technical IDs remain copyable but do not contaminate accessible names.

## Content hierarchy

Write in this order:

1. Outcome.
2. State and consequence.
3. Next action.
4. Supporting reason/evidence.
5. Technical identity.

### Voice

- direct;
- calm;
- specific;
- accountable;
- no false reassurance;
- no control-plane jargon in the default path;
- no anthropomorphic claims that obscure actual authority.

### Preferred patterns

| Avoid | Use |
|---|---|
| “3 runs active” | “Two delivery outcomes are being built.” |
| “parked_no_progress” | “Work stopped making progress.” |
| “Gate failed” | “The full integration check failed.” |
| “Invalid transition” | “This task changed; reload its current state.” |
| “Delivery plan does not exist” | “This plan was moved or deleted. Choose another plan.” |
| “Null is not an object” | “Tusker could not load this detail. Your work is unchanged.” plus support code |
| “Autospawn eligible” | “Background work” |
| “Arm exact fingerprint” | “Start this reviewed delivery” |

## Error anatomy

Every user-visible error includes:

1. what failed;
2. what did not change;
3. whether Tusker will retry;
4. recommended action;
5. support/technical detail disclosure.

Example:

> **The Build routine runner is unavailable.**
> No task was claimed. Tusker found Codex in another location but will not
> switch executables without your approval. **Review runner**

Never replace the application with a raw exception screen. A component failure
is contained to its route/region where possible.

## Loading

- First load: skeleton matching final hierarchy.
- Refresh: keep existing content and show a subtle progress state.
- Mutation: disable only the affected action and keep context.
- Long operation: show durable phase and permit navigation away.
- Reconnect: preserve last-known content and state age.

## Dates and numbers

- Relative dates for recent state with exact timestamp on hover/detail.
- Local time explicitly labeled for promotion windows.
- Durations use human scale; technical milliseconds in exact detail.
- Counts describe objects: “3 requirements,” not naked `3`.
- Costs label source/confidence and never imply hard enforcement falsely.

## Internationalization readiness

- Do not concatenate sentence fragments.
- Allow 30–50% text expansion.
- Separate message keys from raw backend codes.
- Timezone/locale formatting occurs at UI boundary.
- Internal enum labels are mapped, not title-cased automatically.

## Accessibility acceptance

- Complete core flows using keyboard only.
- Complete them at 200% zoom.
- Screen reader can understand Today, plan authorization, decision response,
  and failed-run repair.
- High contrast preserves semantic hierarchy.
- Live daemon events do not create announcement spam.
- Narrow layout keeps the primary action and consequence adjacent.
