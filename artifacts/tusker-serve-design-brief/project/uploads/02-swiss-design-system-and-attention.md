---
schema: tusker.design-note/v1
kind: interface
status: proposed
authority: normative
date: 2026-07-28
parent: "[[00-index]]"
related:
  - "[[01-product-model-and-information-architecture]]"
  - "[[11-responsive-accessibility-and-content]]"
tags:
  - tusker/ux
  - tusker/design-system
---

# Swiss design system and attention

## Design character

Tusker should feel precise, calm, editorial, and operationally trustworthy.
The visual reference is Swiss modernism applied to a live system: grid,
typographic contrast, disciplined whitespace, factual color, and minimal
ornament. It is not “terminal chic,” a glassy observability dashboard, or a
dense issue tracker.

## Grid

### Desktop

- 12 columns.
- 24 px gutters.
- 32 px outer margin at 1024–1279 px.
- 48 px outer margin at 1280–1599 px.
- 64 px outer margin at 1600 px and above.
- Main readable measure: 6–8 columns, maximum 760 px for prose.
- Side rail/detail: 3–4 columns, never narrower than 320 px.
- Shell navigation: fixed 240 px expanded, 72 px compact.

### Alignment

- Page title, primary content, cards, tables, and side rail share column lines.
- Cards do not create their own arbitrary grids.
- Metadata aligns on a baseline or dedicated column, not as floating chips.
- A page uses one major horizontal axis. Nested boxes are a last resort.

## Spacing

Use a 4 px base with semantic steps:

| Token | Value | Use |
|---|---:|---|
| `space-1` | 4 | icon/text optical fixes |
| `space-2` | 8 | compact internal gaps |
| `space-3` | 12 | control groups |
| `space-4` | 16 | card internals |
| `space-6` | 24 | related section spacing |
| `space-8` | 32 | section separation |
| `space-12` | 48 | page rhythm |
| `space-16` | 64 | major editorial break |

Avoid 1 px visual noise between every region. Use space before borders.

## Typography

Use one sans-serif family with excellent UI and tabular numeral support; one
optional serif display face may be used for major page titles only. Monospace
is reserved for exact identifiers, paths, revisions, and commands in technical
detail.

| Role | Desktop | Behavior |
|---|---|---|
| Display | 40/44, medium | One page title only |
| H1 | 30/36, semibold | Major object title |
| H2 | 22/28, semibold | Visible content groups |
| H3 | 16/22, semibold | Card or detail title |
| Body | 15/22, regular | Default copy |
| Small | 13/18, regular | Metadata |
| Label | 12/16, medium, tracked | Section label; use sparingly |
| Technical | 12/18 mono | Exact details only |

Never use all-caps tracked labels for every property. Labels must not compete
with content.

## Color and attention

The neutral palette carries structure. Semantic color carries state.

| Color | Reserved meaning | Never use for |
|---|---|---|
| Red | Human intervention, destructive action, failed delivery/release | branding, selection, ordinary errors already recovering |
| Amber | Degraded, blocked with remedy, repair in progress, approaching limit | routine waiting, medium priority |
| Blue | Active work, selected navigation, informational action | every link and chip |
| Green | Objectively verified or delivered | “automation is on,” generic success decoration |
| Neutral | Context, history, inactive, unknown | hiding important degraded state |
| Violet | Optional brand/accent, plan boundary | runtime severity |

### Scarcity rule

At most one red focal region may dominate a viewport. Multiple problems are
grouped into one attention summary with ranked items.

### State encoding

Color is never the only signal. Pair it with icon, title, and explicit state
text. Do not use tiny colored dots without labels for consequential state.

## Layering and borders

- Base page: one flat canvas.
- Primary section: whitespace and heading, usually no container.
- Selectable item: subtle surface or one border.
- Side sheet/dialog: one elevated layer.
- Technical disclosure: inset neutral surface.
- Avoid cards inside cards inside a bordered page panel.
- Border radius 8–12 px; pills only for compact filters or status.

## Core components

### Outcome row

The default repeating unit:

- title in product language;
- one-line state explanation;
- meaningful artifact/phase/date;
- optional compact status;
- one contextual action;
- opens detail on row click.

### Attention card

Contains:

- what happened;
- why Tusker cannot continue;
- affected outcome;
- one recommended action;
- secondary “See evidence”;
- deadline/expiry only if real.

It never contains raw stack traces or a menu of control-plane verbs.

### Delivery strip

One horizontal phase sequence:

`Planned → Building → Checking → Integrating → Delivered`

Show completed, current, and blocked phase. Do not render every task node here.

### Requirement card

- observable outcome;
- acceptance summary;
- proof method;
- artifact;
- exclusions/decision link when relevant.

### Disclosure

Use consistent levels:

1. **Show details** — more product context.
2. **Show task DAG / proof** — engineering object detail.
3. **Exact technical details** — identifiers, command, refs, receipts, raw
   bounded evidence.

### Settings row

- human title;
- one-sentence consequence;
- current value/control;
- provenance badge only when useful: Project, App default, Discovered, Policy;
- reset only for inherited overrides;
- validation and impact preview inline.

## Controls

- One filled primary action per region.
- Secondary actions are outline or text.
- Destructive actions are not permanently red; they become red in the
  confirmation boundary.
- Toggles are used only for immediate binary settings. Multi-level authority
  uses a segmented choice or select with consequences.
- Save automatically for low-risk local preferences.
- Stage and confirm policy/authority changes.
- Disabled controls explain why and what unlocks them.

## Status language

Every status component contains a verb or clear adjective:

- Building
- Checking the work
- Waiting for your decision
- Repairing automatically
- Runner unavailable
- Scheduled for 02:00
- Delivered yesterday

Avoid “active,” “pending,” “parked,” and “healthy” without an object.

## Tables versus cards

Use rows/cards when users compare meaning; use tables when users compare exact
fields across many items.

- Today: rows/cards.
- Plan requirements: cards or editorial list.
- DAG: graph with synchronized task list.
- Delivery history: table at high density, cards at compact width.
- Settings: aligned rows.
- Diagnostics: tables for runners, leases, capabilities, and audit receipts.

## Loading, freshness, and offline

- Preserve the last good projection while refreshing.
- Show a small freshness note only when older than the state’s expected update
  interval.
- Skeleton only on first load; no full-page spinner after data exists.
- Offline adds a calm top banner and disables mutations, but keeps cached
  knowledge and delivery history readable.
- A stale action revalidates on submit and returns to the changed object rather
  than dumping an error page.

## Motion

- 120–180 ms for local affordances.
- 180–240 ms for drawers/sheets.
- No celebratory animation for routine automation.
- New attention may gently highlight once; it must not pulse indefinitely.
- Respect reduced motion.

## Design QA checklist

- Can the page be understood in grayscale?
- Is there one dominant action?
- Are there more than three visible groups?
- Did an internal noun leak into the default path?
- Can an empty group be removed?
- Does red mean the human must care now?
- Does the layout still work with a 70-character task title?
- Is technical truth reachable in no more than two disclosures?

See [[11-responsive-accessibility-and-content]] for accessibility and copy
rules, and [[03-shell-and-today]] for application of this system.
