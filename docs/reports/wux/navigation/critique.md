# WUX-T-0003 — design critique (static self-review, no screenshots)

Status: **substitute, not the required independent critique.** No rendered
screenshot could be captured on this host (browser execution and local
servers are blocked; see report.md), and this session has no isolated fresh
context to delegate to — so this is the author's own static review of the
component code against the critique rubric, scored honestly. A genuine
screenshot-only review by an independent context must still happen alongside
the owed screenshots.

## Against the rubric

- **Intended aesthetic**: quiet instrument panel — system sans, graphite
  text, flat surfaces, one restrained accent, tokens reused (`bg-panel`,
  `bg-hover`, `bg-active`, semantic fail/warn/info). No gradients, glows, or
  decorative containers. Matches the spec's Apple-native direction.
- **Composition risks I can see without rendering**:
  1. The ↑ ↓ reorder buttons sit in an `opacity-0 focus-within:opacity-100
     group-hover` span — on touch devices there is no hover, so reorder
     becomes drag-or-keyboard only. Acceptable per contract (keyboard path
     exists) but worth a touch affordance later.
  2. `animate-pulse-soft` on the loading row could read as activity theatre;
     it is a genuine state change indicator tied to a real read state, kept
     subtle — borderline, keep.
  3. Wave titles render as plain truncated buttons with no active-run dot;
     the parent project row carries the only activity pulse. Fine — avoids
     repeated status clutter the spec forbids.
- **Fine detail**: 13px/12px type scale, 32px targets, focus-visible rings,
  `role="status"` vs `role="alert"` correctly split (loading polite, error
  assertive), `aria-current="page"` on the selected wave. No fake hardware,
  no generated-looking decoration — nothing to penalize as overdone.
- **Top-studio bar gap**: without a render I cannot judge spacing rhythm,
  truncation balance at 390px, or dark-mode contrast. Those are exactly what
  the owed screenshot review must cover.

## Score: 6.5/10 (provisional, code-only)

Usable structure with honest states and no decoration to penalize, but
visual rhythm, narrow-screen density, and contrast are unverified — a real
render could move this ±2. Not a substitute for user acceptance.
