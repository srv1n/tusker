---
subject: full-height-workspace
title: Full-height workspace with layered side navigation
keywords: [project rail, section rail, contextual sidebar, full height, reading width, responsive navigation, mobile, waves, board, docs]
part_of: work-area-redesign
describes: [internal/serve/ui/src/routes/__root.tsx, internal/serve/ui/src/features/workbench, internal/serve/ui/src/features/knowledge]
status: canonical
created: 2026-09-11
read_when: "Implementing or reviewing the project rail, section rail, contextual panes, or full-height Docs/Waves/Board layout."
skip_when: "Changing task execution, project registration semantics, document saving protocols, or graph algorithms."
sources: [work-area-redesign.md, documents-experience.md, project-registration-and-visibility.md]
updates: [docs/system/serve-ui.md]
decisions_locked: true
---

# Full-height workspace

## 1. Outcome and authority

People switch projects and sections from the left while Docs, Waves and Board start at the top of the workspace, below one compact feature toolbar. Prose keeps a comfortable line length. Boards and graphs use the remaining width and height. Navigation stays visible without consuming several rows above the content.

The September 11 conversation selects layered side navigation and requests detailed smaller-agent tickets. This document supersedes the horizontal-strip placement and Work/Documents top-row requirements in work-area-redesign section 4. It also supersedes the placement portions of the older September 9 shell packet and compact-project-navigation plan. Existing project visibility, identity, execution safety, editor saving and truthful state contracts remain authoritative in their owning specifications.

Locked decisions: two slim desktop rails; an optional third contextual pane; one feature toolbar; independent scrolling; explicit pane collapse; direct visible-project switching; responsive collapse; bounded prose and an unconstrained canvas. Dimensions, breakpoints and state shapes below are settled implementation defaults, not verbatim user requirements. Implement them without another design interview; document evidence if an accessibility or existing-contract conflict requires a change.

This is specification and inert task authoring. No application implementation, daemon launch, dispatch, install, commit or deployment is authorized by creating this handoff. Workers execute only after assignment through the normal interactive task protocol.

## 2. Desktop geometry

All dimensions are CSS pixels at normal text scale. Measure from the web content viewport; the macOS native title bar is outside this budget and stays unchanged.

| Surface | Default | Responsibility |
| --- | --- | --- |
| Project rail | 160px expanded; 56px collapsed | Visible logical projects, global search and app actions |
| Section rail | 160px expanded; 56px collapsed | Waves, Board, Docs; project settings and More at the bottom |
| Context pane | 280px; resize 240–360px | Documents tree or wave list; independent scroll |
| Feature toolbar | 56px minimum | Current item, contextual-pane toggle, feature actions |
| Content inset | 16px desktop; 12px phone | Reading or canvas content, without a hero header |
| Phone bottom navigation | 52px minimum plus safe-area inset | Waves, Board, Docs |

```text
project  section    context                   feature toolbar
┌──────┬──────────┬───────────────────────┬──────────────────────────┐
│ Ci   │ Waves    │ Filter…               │ overview       Actions   │
│ Tu   │ Board    │ ▾ docs/system         ├──────────────────────────┤
│ SR   │ Docs ◀   │   overview ◀          │ System overview          │
│ BE   │          │   architecture        │                          │
│      │          │                       │ Readable prose,          │
│      │          │                       │ full available height.   │
│ …    │ Settings │                       │                          │
└──────┴──────────┴───────────────────────┴──────────────────────────┘
```

- Root fills the viewport. The rails start at its top; there is no project strip, project tab row, decorative outer card padding or blank top spacer.
- The shell uses a Swiss 4px base grid with an 8px spacing module. Project and section items begin at the top 8px inset, their three-row utility footers are 144px, navigation targets are 40px, contextual rows are 32px and tree indentation advances in 16px steps. Control centerlines and selection bounds align to those modules; spacing and surface tone separate columns without divider rules.
- Project and section rails expand independently. Expanded rails show icons and labels; collapsed rails show the same 40px targets with icons or project initials. App Settings/More and Project Settings/More occupy matching bottom rows, with each rail's expand/minimize control in the aligned final row. Notifications float at the workspace top-right instead of consuming either rail.
- Each feature has one toolbar. Root must not add another toolbar above it. A shared toolbar component supplies mobile project access inside the same row as feature controls.
- Context-pane filter/header occupies that pane only. It does not push down the main content.
- In normal desktop states, the content viewport starts at y <= 48px; the first heading, wave row, board column heading or graph node starts at y <= 80px. Capture both bounding boxes; an empty canvas at y=44 with its first node at y=300 fails.
- Genuine fault/safety/save-conflict banners and intentionally expanded Details are allowed to consume space. Record their height separately; never hide a real error to pass the geometry budget. Persistent selection labels/status fit in the toolbar; long status text is expandable.
- Toolbars grow when enlarged text requires it. The 80px first-content budget applies to default-scale desktop healthy states, not to clipping text at 200% zoom.
- Keep one main landmark. Each rail has a distinct accessible navigation label; contextual panes have named complementary landmarks.

## 3. Project rail and section rail

### Projects

Use the existing logical project collection and projectVisibleInNavigation filter. One logical repository yields one rail item even with multiple checkouts. Hidden projects stay recoverable through existing App Settings / All Projects; this rail must not silently reveal them or change visibility/automation.

Each visible project is one 40px minimum target inside the 48px rail. Reuse an existing icon if the data model already provides one. Otherwise show initials: first characters of up to two name tokens (space, underscore and hyphen delimit tokens), or the first two characters of a single token. Empty names use a neutral folder glyph. For collisions, give the colliding items stable small ordinal disambiguators ordered by stable logical ID; recency and attention updates never renumber them. Full project name is always the accessible name and is available on hover and keyboard focus. Identical full names also expose their repository/checkout path in the expanded list. Color is supplementary; no icon upload or icon backend is part of this work.

Preserve pins, explicit ordering controls and session-stable positions. Existing fresh-mount recency policy may continue. Ordinary switching never moves targets, nor does a status refresh. Adding/removing/hiding a project may alter the set; preserve surviving order. Scroll the rail vertically with native wheel/touch/scrollbar behavior. Focus reveals an offscreen target. Do not continuously snap to the selected project while the person browses the list.

An explicit Expand projects button opens a full-name overlay anchored to the rail. Opening by hover alone is prohibited. The overlay lists the same visible projects in the same order, supports selection, pin/reorder actions and checkout selection through existing semantics, and closes on selection, Escape or outside click. A link to All Projects exposes hidden-project recovery. No separate hidden-project store or mandatory search picker. The overlay never resizes the content or remounts the editor.

Place global Search, Attention and App actions in a fixed utility group at the bottom; the project list scrolls above it. App actions retains Add project, Refresh projects, App settings and All Projects access. Keep the existing search shortcut. On very short viewports, utilities remain reachable and the list scrolls rather than overflowing the page. Attention badges are overlays, capped visually at 99+ with the exact count in the accessible name.

### Sections and route selection

The second rail uses visible words: Waves, Board, Docs. Project settings and More (Plan, Trains, Diagnostics) stay reachable at the bottom. Secondary destinations must not be mislabeled as Waves just to keep a primary item highlighted. Show current state by route ownership with aria-current; task detail belongs to Board, wave detail to Waves, knowledge reader/graph to Docs. Global settings has no active project section and does not erase the last project route.

| Action | Destination |
| --- | --- |
| Project item | Its last valid route in the selected/remembered checkout; fallback to that project's waves overview |
| Waves label | Waves overview; detail remains available in the context list |
| Board label | Board with its remembered board/list mode and filters |
| Docs label | Last valid document/knowledge graph route for the active checkout; fallback to knowledge root |
| Project settings | Existing /p/$projectId/settings |
| Global settings | Existing global settings route, retaining prior project state |

Explicit deep links and browser back/forward win over stored routes. Navigation must never run work, hide a project, save a document or mutate automation as a side effect. Checkout identity follows existing projectContainsCheckout/route-owner semantics.

## 4. Context pane and shared interface

Docs uses the actual file tree. Wave detail uses a selectable wave list; wave overview already is a list and defaults to no context pane. Board defaults to no context pane because the current product has no saved-view authority. Existing usable filters belong in its toolbar or a disclosure; do not invent empty saved views to fill a third column. Settings and other routes can omit the context pane.

The context pane is open by default for Docs and wave detail on wide desktop, closed by default for Board/overview. Its explicit toggle is first in the feature toolbar. Closing restores its entire width to content; reopening restores saved width. Persist desktop open/width by actual checkout route ID and section. Resizing works by pointer and keyboard (separator with accessible name/value, ArrowLeft/Right step 16px, Home 200px, End 360px). Clamp stored dimensions, retain a sensible content width and use overlay mode when docking is unavailable. No new resizing library.

Overlay panes do not push or reflow content. Give them a visible close button, focus containment, background inertness, Escape dismissal and focus restoration to the opener (or replacement toolbar trigger after route change). Close after successful item selection; cancelled navigation keeps the current content and editor safe. Only one navigation overlay can be open at once. Task inspector is separate feature UI: opening navigation dismisses the inspector through its existing close path before moving focus, and opening the inspector closes navigation. This affects temporary presentation only; selected task/return state remains available.

Use one production layout component, proposed at features/workbench/navigation/WorkspaceSurface.tsx, for Docs/Waves/Board. This is a shared boundary used by multiple features, not a general docking framework. The shell ticket owns its first implementation; later responsive changes serialize after feature integration.

Required export contract (names are reserved; normal React typing details remain local):

```ts
type WorkspaceSection = "waves" | "board" | "docs";
type WorkspaceSurfaceProps = {
  projectId: string; // actual route/checkout ID
  section: WorkspaceSection;
  label: string; // accessible main content label
  toolbarStart: React.ReactNode;
  toolbarEnd?: React.ReactNode;
  context?: { label: string; content: React.ReactNode; defaultOpen: boolean };
  children: React.ReactNode; // fills remaining area; feature owns its scroller
};
// WorkspaceSurface owns context toggle, pane geometry and one toolbar.
// It inserts the mobile project trigger inside that toolbar.
```

Keep navigation data/persistence owned once in the mounted root, refactoring ProjectStrip's existing logic only as needed for rail and mobile trigger consumers. Provide access through an ordinary React context if required; it projects existing authority and is not a second project registry. Avoid a global header registration effect or portal lifecycle. Features pass their toolbar/context content directly to WorkspaceSurface. Legacy/global pages use one compact shell fallback toolbar on phone only; they must not receive a second header when a feature uses WorkspaceSurface. Determine this from the existing route map, not DOM discovery.

## 5. State and restoration

Extend navigationState.ts and its existing guarded storage read/write path. Preserve old records including opaque viewStateByProject values; merge a namespaced workspace field rather than overwriting unrelated view state. Preserve old expanded-project fields for migration even though they no longer drive the UI. Unknown future fields are retained where existing opaque state permits. Invalid or blocked storage falls back safely without preventing use.

The state ticket exposes typed get/update helpers over existing NavigationState for checkout-scoped workspace state. A setter merges a partial section patch with other sections and opaque state. Treat the following as the cross-ticket data contract:

```ts
type WorkspaceViewState = {
  lastDocsPath?: string; // validated same-checkout knowledge path
  docs?: { contextOpen?: boolean; contextWidth?: number;
    scrollBySubject?: Record<string, { top: number; left: number }> };
  waves?: { contextOpen?: boolean; contextWidth?: number;
    overviewQuery?: string; showCompleted?: boolean; overviewScrollTop?: number;
    byId?: Record<string, { view?: "flow" | "results"; scrollTop?: number;
      scrollLeft?: number; selectedTaskId?: string }> };
  board?: { mode?: "board" | "list"; scrollTop?: number;
    scrollLeft?: number; selectedTaskId?: string; selectedTags?: string[] };
};
```

Validate plain objects, known enums, finite nonnegative coordinates and bounded dimensions. Keep the last 20 visited documents and 20 waves per checkout to bound local preferences; order by actual visits, not render count. Do not store document bodies, drafts, task data, run truth or auth tokens. Context display preferences persist across reload; route/view/scroll restore across project and section switches and browser back. Restore scroll once content is loaded and laid out, clamp to current scroll range, then stop restoring so live updates cannot yank the view. Explicit document anchors beat saved scroll. Restore inspector selection only if the selected task still belongs to the current project and view.

Cross-checkout path restoration must never leak a document or task selection. Deleted document/wave IDs use the owning feature's existing not-found or root recovery and clear stale preference entries. A hidden project can remain open through an explicit valid deep link; it stays absent from the visible rail. Hiding/removing a project must not silently mutate stored work. Preserve existing registration and route-recovery semantics.

treeStore remains owner of folder collapse and tree filtering. New context open/width authority is workspace state: retain folder/filter state, but do not interpret the old narrow-drawer railOpen value as a desktop docking preference. When no workspace context preference exists, use the feature default, then stop writing the old drawer-open field. Mobile/overlay open state is ephemeral and always closed on fresh entry; it never overwrites the saved desktop open preference. Do not use a persisted drawer-open boolean to trap keyboard focus on a later desktop visit.

Editor saving continues through useDocgraphEditor and its existing CAS protocol. Pane collapse, resizing, breakpoints and project-overlay opening must not remount DocBodyEditor, reset its selection or lose dirty text. On actual navigation retain the existing draft/save/conflict protection. If the current route boundary allows dirty text loss, report the defect and implement the smallest editor-local guard needed for this navigation path; no parallel autosave protocol. Browser checks must cover save in flight, failure and conflict, not just clean documents.

## 6. Docs integration

KnowledgeShell adapts to WorkspaceSurface; reuse KnowledgeTree and treeStore. Remove duplicate desktop and overlay tree mounts where possible so they cannot own conflicting focus/state. KnowledgeReader, KnowledgeList and KnowledgeGraph consume the same layout boundary.

One toolbar contains context toggle, subject/current document, Save/dirty state, Details and Files/Graph controls. Long subject truncates visually with full accessible text. On narrow widths move secondary Details/Files/Graph actions into an explicit menu while keeping Save status and the context trigger reachable. Preserve current autosave, Cmd/Ctrl+S, safe Markdown/Mermaid, backlinks and supersession/error notices.

Render prose with a target max-width of 72ch at its text font, width 100% below that, and 1.5–1.65 line-height using current tokens. Do not shrink fonts to hit geometry. There is one visible document heading; metadata Details does not add a second title card. Normal top body inset is 16px. Images/diagrams/tables/code can use the wider content pane through existing renderer/editor capabilities; when a block cannot safely break out of the editor column, give that block local horizontal scrolling. Do not change Markdown serialization or add a second read-only renderer to obtain full-width blocks.

The document scroll container fills the remaining height. Tree scrolling never scrolls the document or root. Expanding a navigation overlay never shifts the reading column; docking/collapsing deliberately recenters it without losing scroll or focus. Preserve source/links and graph functionality; redesigning the knowledge graph algorithm is out of scope.

## 7. Waves integration

WorkOverview retains the quiet grouped list, search/completed behavior and truthful status from the existing contract. Remove the shared Shell's top hero padding and width cap from graph and board surfaces. Wave overview may retain a readable list width, but begins immediately under its toolbar. Its search/filter controls live in that toolbar.

Wave detail has a contextual list of waves from useWaves with title, one truthful state, active selection, filter and a Waves overview link. Use real wave IDs and the existing query cache; avoid fetching every wave's full task details for this list. Sort by the same stable source/grouping policy as the overview. Show loading, empty and unavailable distinctly. Active wave remains visible even when a filter excludes it, as a clearly marked current item outside filtered matches.

The toolbar contains wave ID/title, Flow/Results controls and the existing Start action when applicable. Put full authored outcome in Details. Title is one line at desktop default scale with full text accessible through Details; never replace authored intent with invented execution prose. Long execution/review status is a compact disclosure in the toolbar; remove the permanent Execution plan banner above the graph. Keep warnings and real execution failures accessible and visible as appropriate.

WaveFlow becomes a flex child with its own remaining-height scroll region. Preserve the CURRENT top-down layout, prerequisite direction, node sizing/state truth, selection and task inspector. This layout task does not reinstate older horizontal-flow or pan/zoom requirements; the current controlled viewport prop is not proof that pan/zoom is implemented. Restore actual graph scroll offsets now. Do not add a graph engine or redesign nodes. The first node is within 80px of the workspace top in the default healthy detail state. Large graphs scroll inside the canvas; small graphs start near its top, not vertically centered.

Flow/Results stays explicitly selected during live updates; an explicit URL view wins on entry. A completed wave's initial default remains Results when no stored/explicit choice wins. Results uses a readable text width with wider evidence where supported. Opening/closing the inspector preserves graph scroll; switching projects restores wave/view/selection safely. Current command authority, action readiness and errors are preserved; no live Start click is required for visual qualification.

## 8. Board integration

WorkBoard uses WorkspaceSurface with no default context pane. Board/list mode and existing available filters fit in its single toolbar. TaskBoard receives controlled toolbar actions or a small explicit toolbar slot so it does not render a second mode/filter header. Reuse actual task grouping, card actions and inspector.

Board content uses all remaining width and height; remove the WorkExperience 1180px wrapper restriction. Preserve the existing responsive column grouping rather than inventing new statuses or a drag workflow. List mode may constrain text cells while its scroll region remains full height. Keep content scrolling in one feature region; avoid outer-page scroll plus duplicate inner vertical scrollbars. Horizontal overflow, when intrinsic to board columns, is local to that region.

Do not add saved views or a tag backend. Current capability-gated tags remain capability-gated. Mode, supported filters, scroll and valid selection survive project changes. A missing selected task closes its inspector safely without erasing the rest of the view. The board ticket follows the wave ticket because both edit WorkExperience.tsx; Docs may proceed in parallel with Waves.

## 9. Responsive and accessibility behavior

| Width in CSS pixels | Rails | Context | Main navigation |
| --- | --- | --- | --- |
| >=1200 | Project + section visible | Docked if user preference is open | One-click project/section selection |
| 768–1199 | Project + section visible | Overlay; default closed on entry | Same rails |
| <768 | Both rails hidden | Overlay; default closed on entry | Project button inside feature toolbar; Waves/Board/Docs bottom bar |

These are CSS viewport breakpoints, including browser zoom. On phone, project selection is two taps (open, choose); sections stay one tap. Project actions, project settings and More remain in clearly labeled picker/actions surfaces; app and project settings retain distinct scope labels. Do not stack project and section strips at the top. The bottom bar reserves layout space and safe-area padding so it never covers the last line, graph controls or editor caret. Context and project drawers use min(320px, viewport width minus 32px) and fit between safe areas.

Crossing a breakpoint closes ephemeral navigation overlays, releases any focus trap and preserves saved desktop preference, route, editor instance, selection and scroll. Restore focus to an equivalent visible trigger only if focus was in a now-hidden control. Backgrounded tabs and live updates never steal focus. Hidden rails must be absent from the tab order/accessibility tree. Avoid hover-only access and motion-dependent cues; respect reduced motion and existing light/dark tokens.

Desktop project targets are >=40px; phone interactive targets are >=44px. Use visible focus indicators and accessible expanded/current names. Resize separators are keyboard operable. At 200% zoom labels/actions may wrap or enter overflow, but remain usable without whole-page horizontal scrolling. A right task inspector can overlay remaining content using its existing breakpoint; it must not create a fourth permanently docked column.

## 10. Source map and surrounding ownership

Source inspected September 11; inspect the dirty baseline again before assignment. Existing code is evidence of current seams, not a claim that earlier tickets are complete.

| Current path under internal/serve/ui | Existing responsibility |
| --- | --- |
| src/routes/__root.tsx | Root project strip, project top tabs, safety banners, embedded /panel exemption |
| src/features/workbench/navigation/ProjectStrip.tsx | Project query, visibility, grouping, saved checkout, restore, pinning, utilities and dialogs |
| src/features/workbench/navigation/navigationState.ts | Guarded v2 storage under v1 key, stable order, route validation, opaque view state |
| src/features/workbench/integration/WorkExperience.tsx | Shared padded/max-width Shell; WorkOverview, WorkWave, WorkBoard, inspector and queries |
| src/features/workbench/flow/WaveFlow.tsx | Current top-down graph, status banner, intrinsic canvas and warnings |
| src/features/workbench/board/TaskBoard.tsx | Controlled board/list, filters, grouping/cards |
| src/features/knowledge/KnowledgeShell.tsx | Fixed desktop tree, narrow drawer/focus handling and SectionToolbar |
| src/features/knowledge/KnowledgeReader.tsx | Existing CAS editor, toolbar, 46rem article, notices, details and links |
| src/features/knowledge/treeStore.ts | Tree collapse/filter and old drawer-open storage |
| src/router.tsx | Route ownership and deep-link destinations |
| test/project-strip.browser.mjs, test/documents-polish.browser.mjs, test/real-work.browser.mjs | Existing browser loading/fixture patterns; reuse dependencies, not source-string proof |

Related tasks: WUX-T-0015 state, WUX-T-0016 strip and WUX-T-0017 qualification remain older held contracts; reuse their current implementation, not their superseded layout acceptance. WUX-T-0013 Documents polish is ready with partial proof and overlaps the reader; WUX-T-0005 graph and WUX-T-0006 inspector are ready. WUX-T-0008/0009 overlap board/integration; WUX-T-0019 visibility is held and touches project navigation. This plan neither marks them complete nor cancels them. They are surrounding work, not blanket prerequisites: the assignment owner checks active claims and assigns this plan exclusive ownership of its paths before starting. If an old task still owns a file, finish/release that claim or postpone this ticket; do not overwrite it. Preserve visibility/CAS/graph contracts while changing placement.

Only the desktop shell owner and later responsive owner edit root/router/shared layout. Waves then Board serialize changes to WorkExperience. Qualification owns built dist and current system docs. Backend, native app, package manifests and global visual tokens stay outside implementation scope. No branch cleanup or broad refactor is part of the plan.

## 11. Proof contract

The seven task packets in docs/plans/full-height-workspace/ are the human handoff. Their executable acceptance is imported through full-height-workspace.plan.yaml. Commands in those packets are planned checks, not results. Use configured work/review levels; suggested Luna/Terra assignments are advice for manual delegation, not changes to profiles or permission policy.

One proposed routed browser entry, test/full-height-workspace.browser.mjs, owns this plan's fixture setup and case dispatch. The shell ticket creates it with --case support; feature owners add separate case modules in test/full-height-workspace/ to avoid editing the runner in parallel. Reuse installed Playwright loading from existing scripts and real routed components. TUSKER_WORKSPACE_BASE_URL defaults to http://127.0.0.1:5195. --case shell, docs, waves, board, responsive and all are the contract; unknown or zero matched cases fail. State tests live in test/workspace-state.test.ts. No new test framework or package installation.

Fixture requests must be explicit: fail on unexpected API calls; use the actual response shapes, never a generic [] fallback that masks missing integration. Match known SSE behavior deliberately and label it as fixture. Save screenshots and machine-readable geometry/case results under docs/reports/full-height-workspace/<case>/. Tests exercise actual clicks, focus, scroll, routes, editor state and geometry; source grep or HTTP success cannot satisfy UI acceptance.

Required screen matrix: 1920×1080, 1440×1000, 1200×900, 1199×900, 1024×768, 768×900, 767×900, 390×844 and 320×640; normal and enlarged text, light/dark and reduced motion representative cases. Shell checks 0, 1, 13 and 50 projects, duplicate initials/full names, long names, hidden projects, multiple checkouts, loading and failed project reads. Docs checks long prose, wide block, deep anchor, tree collapse/resize, dirty editor, in-flight save, failure/conflict and project return. Waves checks a long-title chain, branching graph, unavailable dependencies, Results, inspector and live state update without scroll reset. Board checks populated/empty/error views, all reachable columns, mode persistence and inspector. Responsive checks focus traps, safe areas, keyboard, overlay mutual exclusion and resize with dirty editor.

Fixture qualification and built-preview qualification are required implementation proof. Live service and installed macOS observations are separate supplemental evidence: use only an already-running authorized surface, make read-only navigation checks, and record unavailable surfaces explicitly. Do not install/restart the app or daemon or execute a real wave for proof. No mandatory new human gate. The normal independent task review applies; subjective user acceptance remains unclaimed until observed.

For final qualification, run focused tests, then bun run build once after integration settles (the build already includes tsc). Start the built preview at 127.0.0.1:5196, run the same browser cases against it, then update docs/system/serve-ui.md only for verified shipped-source behavior. Keep dist generation serialized; never delete another owner's output to make a build appear clean. A failed requirement stays open; unrelated baseline failures are named with exact file/error and proof limits.

## 12. Contacts, escalation and handoff readiness

Architect and origin: Sarav, in the assigning Codex conversation. The planning environment's installed Tusker capability manifest exposes no agent/contact/message command, and `tusker agent --help` returns Unknown command. No registered execution/contact binding was verified; architect/origin/peers metadata is intentionally unset rather than fabricated. Workers return questions/results to Sarav through their assigning session; if that session is unavailable, mark the dependent work blocked and leave the exact question in the normal task report for the operator. No Slack/email/other-task message is authorized by this document.

Escalate a locked-design conflict, unavailable prerequisite export, or active shared-file ownership conflict with ticket and acceptance ID, observed facts, required decision and recommendation. Continue independent owned work. Routine CSS, typing and extraction choices remain with the worker. The shell ticket owns shared interfaces; the final qualification ticket owns integration acceptance. Peer relationships are the explicit dependency edges, not invented execution addresses.

Readiness review: a fresh worker starts at its packet, reads only the named spec sections and existing source seams, obtains an interactive claim and inventories the dirty baseline. Contracts fix geometry, section routing, migration, responsive behavior, source ownership and scenario-to-proof mapping. No new product decision blocks authoring. Remaining operational prerequisites are exclusive file ownership at assignment and successful upstream delivery; no live contact is assumed. Tracker import/preflight and corpus validation results are recorded in the handoff index, separately from this semantic readiness assessment.

<!-- tusker:delivery-import:f20e387140ce1ab6:begin -->

## Work streams

- `[[WUX-T-0024]]` implements delivery source `board`.
- `[[WUX-T-0022]]` implements delivery source `docs`.
- `[[WUX-T-0026]]` implements delivery source `qualification`.
- `[[WUX-T-0025]]` implements delivery source `responsive`.
- `[[WUX-T-0021]]` implements delivery source `shell`.
- `[[WUX-T-0020]]` implements delivery source `state`.
- `[[WUX-T-0023]]` implements delivery source `waves`.

- `[[W-0021]]` is the imported delivery wave.

<!-- tusker:delivery-import:f20e387140ce1ab6:end -->
