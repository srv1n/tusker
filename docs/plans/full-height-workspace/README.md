# Full-height workspace handoff

Specification and tickets only. No application code was edited, no application build/tests were run, no implementation agents were launched, and no wave was armed or dispatched.

## Start here

1. Read [the detailed specification](../../../.tusker/specs/full-height-workspace.md).
2. Assign one ticket below, in dependency order. Each packet is self-contained: intent, current source seams, locked behavior, implementation steps, non-goals, owned paths, contacts, acceptance IDs and exact scenario checks.
3. Workers obtain normal interactive task ownership and preserve the dirty baseline. The generated packet is available through `tusker packet <ID> --for agent`; `--force` inspects held work and does not authorize execution.

Tracked wave: **W-0021**, disarmed. Tasks **WUX-T-0020–WUX-T-0026** were imported inertly. Canonical executable contracts are in [full-height-workspace.plan.yaml](../../../.tusker/specs/full-height-workspace.plan.yaml); these readable packets are rendered from that plan. Preserve scope/source keys when amending them through the CLI.

## Tickets

| Order | Task | Packet | Depends on | Suggested manual worker |
| --- | --- | --- | --- | --- |
| 1 | WUX-T-0020 | [State and restoration](01-state.md) | None | Terra |
| 2 | WUX-T-0021 | [Desktop rails and shared surface](02-shell.md) | 0020 | Terra |
| 3 | WUX-T-0022 | [Docs reader and contextual tree](03-docs.md) | 0021 | Terra |
| 4 | WUX-T-0023 | [Waves list and full-height graph](04-waves.md) | 0021 | Terra |
| 5 | WUX-T-0024 | [Board canvas and view restoration](05-board.md) | 0023 | Luna or Terra |
| 6 | WUX-T-0025 | [Tablet/phone and keyboard behavior](06-responsive.md) | 0022 + 0024 | Terra |
| 7 | WUX-T-0026 | [Built acceptance and current docs](07-qualification.md) | 0025 | Terra |

```text
State → Shell ┬→ Docs ─────────────┐
              └→ Waves → Board ──┴→ Responsive → Qualification
```

Docs and Waves have independent ownership. Waves and Board both edit WorkExperience.tsx and must be sequential. Shared shell files move from Shell to Responsive only after feature integration. Current Tusker capacity is one active run per project; the plan preserves that setting and uses concurrency 1. The graph identifies independence, not permission to bypass capacity or launch workers.

All tickets use the existing configured standard work/review levels. Suggested models do not change any profile. Terra is the safer default for state, routing and editor boundaries; Luna is suitable for the bounded Board ticket once the shared surface exists.

## Settled design

- 48px project rail, 88px labeled section rail, optional 240px contextual pane (resize 200–360px).
- One 44px minimum feature toolbar; normal desktop first useful content starts within 80px of the web viewport top.
- Project initials work without custom icons, with collision disambiguation and full-name access.
- Prose targets 72ch; boards/graphs use the remaining canvas.
- Context pane docks at >=1200px, overlays at 768–1199px; below 768px project access moves into the toolbar and section navigation to the bottom.
- Preserve actual checkout identity, visibility, pins/order, state restoration, CAS saving, current top-down graph behavior and execution authority.
- No saved-view backend, graph engine, native window changes or new packages.

The spec gives exact route selection, focus, scroll, dirty-editor, migration, empty/error and breakpoint behavior. Numeric choices are implementation defaults from this planning pass. They do not claim measured implementation results.

## Supersession and surrounding work

work-area-redesign section 4 now points to this specification, and its decision record explains the September 11 change. The older horizontal-strip placement contract is superseded. Older tasks remain unchanged as history; this plan does not cancel them or claim their work complete.

Before assignment inspect overlapping ownership: WUX-T-0013 Documents polish, WUX-T-0005 graph, WUX-T-0006 inspector, WUX-T-0008/0009 board/integration, WUX-T-0015/0016/0017 prior navigation, and WUX-T-0019 project visibility. Finish/release an active conflicting claim before editing. Existing code, visibility rules and safe-save behavior are the starting baseline.

Architect/origin fallback is Sarav in the assigning conversation. No registered Tusker execution contact was verified, so no fake execution address appears in the tickets. Workers bring precise design/ownership questions back to their assigning session.

## Planning validation — September 11

- PASS: scoped `tusker docs check .tusker/specs/full-height-workspace.md --json`.
- PASS: delivery doctor, import dry-run and inert import; all seven requirement/acceptance contracts mapped.
- PASS: all seven generated packets inspected for acceptance, checks, dependencies, source ownership and contact/readiness guidance.
- PASS: `tusker docs map` regenerated the documentation index/graph.
- Wave preflight: taskContracts/specDag/artifacts/workflow/runner/skill/workspaceIsolation checks pass. Execution readiness remains false: managed daemon not alive/reconciling, project not registered/automation-enabled/healthy as required by that preflight, and unattended approval policy would pause. These are reported runtime predicates, not reasons to enable automation during planning. Wave remains disarmed.
- Full-vault `tusker validate`: FAIL on unrelated existing proof/spec-contract records (including ORC-T-0084/0085/0086 missing done-task evidence and SRV-T-0007 missing spec_refs). This new spec also has the expected pending current-doc update warning; WUX-T-0026 owns that update after implementation. No unrelated records were repaired.
- The initial `docs new` scaffold was refused because an unrelated project-registration decision lacked decides_for. The hand-authored spec subsequently passed its own scoped check; no generated tracker state was edited by hand.

These are authoring checks only. Implementation checks in the tickets remain pending. Fixture/source/built-preview/live/installed-app proof are deliberately separate. The final qualification ticket owns application build and browser verification after implementation, not this planning session.

## Copyable assignment

> Implement only WUX-T-0020 using docs/plans/full-height-workspace/01-state.md and its exact referenced spec sections. Obtain normal interactive ownership, preserve the dirty baseline, stay within owned paths, run the mapped checks after the change settles, and return acceptance-linked results. Do not start a daemon or nested worker. Return locked-design or ownership conflicts to this assigning session.

For later tickets replace the ID and packet path using the table, and first confirm their dependencies are delivered.
