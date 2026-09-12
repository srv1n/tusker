# 03. Give Docs a full-height reader and collapsible contextual tree

Source key: `docs`. Canonical executable contract: `.tusker/specs/full-height-workspace.plan.yaml`. This packet is rendered from that plan; update the plan and regenerate the packet together. Tracker ID: `WUX-T-0022`; wave: `W-0021` (held/disarmed).

People start reading directly below the document toolbar and can reclaim tree width without losing their place or draft. The current KnowledgeShell permanently docks its desktop tree, and KnowledgeReader adds a padded metadata/body stack. Adopt WorkspaceSurface while preserving the actual CAS editor and document graph.

## Implementation contract
Read .tusker/specs/full-height-workspace.md sections 4–6 and 9–12.

1. Adapt KnowledgeShell and SectionToolbar callers across KnowledgeReader/List/Graph to WorkspaceSurface. Reuse KnowledgeTree, preserving folder/filter/selected-ancestor behavior. Remove conflicting duplicate pane/dialog ownership. Dock/collapse/overlay are presentation of the same tree; root project/section rails must not be repeated here.
2. Keep treeStore as authority for folder/filter state. Old railOpen describes a narrow drawer, so never infer desktop docking from it; absent new Docs context preference uses the feature default and explicit false remains closed. Persist context open/width through shared state and maintain ephemeral overlay state separately. Wire scroll capture/restore after layout; anchors override restoration and background data refresh must not reposition the reader.
3. Put subject, Save/dirty state, Details and Files/Graph into one feature toolbar. Collapse frontmatter using HeaderCard's existing details controls without duplicating the document heading. Set normal top inset to 16px and prose width target to 72ch. Wide editor blocks use existing capability or local overflow; preserve serialization, sanitized rendering, Mermaid fallback, backlinks and supersession notices.
4. Preserve editor identity while resizing/collapsing/changing breakpoints. Trace useDocgraphEditor and navigation guard behavior before changing any route callback. Test dirty text, pending save, failed save and conflict while navigation controls open/close. Modify useDocgraphEditor only for a demonstrated draft-loss defect introduced/exposed by this navigation flow, retaining the same CAS protocol; no autosave rewrite.
5. Implement the docs case module using the shell runner contract. Include at least two projects/checkouts, two documents, long prose, a wide code/table block, deep anchor and failed/conflicted save responses. Interact with the real editor and tree. Record before/after and collapsed/expanded images, geometry and exact editor/scroll observations.

## Start and boundaries
Read AGENTS.md, the exact spec sections below, and all callers of the owned components. Obtain the supported interactive Tusker claim only after assignment; do not dispatch workers or start a daemon. Inventory the dirty baseline and current active claims before editing. Preserve other owners' changes. No backend/native app/package/global-theme edits, resets, installs, commits or deployment are part of this ticket. Use configured work/review levels; model suggestions are manual assignment advice only. Run the focused checks after the implementation settles; the final ticket owns the integrated build and dist. A runnable check with zero cases is a failure. No source-string assertion or screenshot alone proves an interaction.

## Contacts and surrounding work
Architect/origin is Sarav in the assigning conversation. No routable Tusker execution contact was verified; the installed CLI has no agent/contact command. Return a conflict with acceptance ID, facts, decision needed and recommendation to the assigning session; if unavailable, leave that dependent work blocked for the operator while continuing independent work. Never fabricate an execution address or message another session without authorization. See spec section 10 for WUX-T-0013/0015/0016/0017/0019 and graph/inspector/board overlap. Older horizontal-strip acceptance is superseded. Resolve active owned-file claims before editing; historical ticket status does not prove code absent or complete.

## Assignment and integration
Suggested manual worker: Terra. Required upstream outputs: shell. Dependencies are hard because consumers need those exports and the same owned source baseline. Docs depends on shell and owns only knowledge feature paths plus its cases/regressions. It can run beside Waves. WUX-T-0013 has overlapping reader/tree work; inspect active ownership and retain current polish. If shared surface needs a fix, report the exact props/geometry defect to the shell owner instead of editing it concurrently. knowledge.css is a proposed feature stylesheet; use the existing feature stylesheet if already present and record the exact owned substitution before editing.

## Readiness review
This packet fixes the starting seams, invariant behavior, upstream boundary and scenario checks. Read it without relying on the design chat. No unresolved design question blocks the specified work; active ownership and prerequisite completion are checked at assignment. The report must distinguish expected behavior from observed PASS/FAIL, link each acceptance ID to evidence, and name any limitation.

## Owned paths

- `internal/serve/ui/src/features/knowledge/KnowledgeShell.tsx`
- `internal/serve/ui/src/features/knowledge/KnowledgeReader.tsx`
- `internal/serve/ui/src/features/knowledge/KnowledgeList.tsx`
- `internal/serve/ui/src/features/knowledge/KnowledgeGraph.tsx`
- `internal/serve/ui/src/features/knowledge/KnowledgeTree.tsx`
- `internal/serve/ui/src/features/knowledge/treeStore.ts`
- `internal/serve/ui/src/features/knowledge/HeaderCard.tsx`
- `internal/serve/ui/src/features/knowledge/knowledge.css`
- `internal/serve/ui/src/features/knowledge/useDocgraphEditor.ts`
- `internal/serve/ui/test/documents-polish.test.ts`
- `internal/serve/ui/test/documents-polish.browser.mjs`
- `internal/serve/ui/test/full-height-workspace/docs.mjs`
- `docs/reports/full-height-workspace/docs`

## Acceptance

- **A1** — Docs reader/list/graph use one toolbar and one contextual tree; healthy desktop first content starts at y<=80px, prose width is constrained, and wide blocks never overflow the whole page.
- **A2** — Tree collapse/reopen/resize preserve filter, folder expansion, selected ancestors, document scroll and focus; independent tree scrolling does not scroll content.
- **A3** — Document/checkout switching and back navigation restore valid last Docs route/scroll while deep anchors win; invalid/deleted destinations recover without cross-project state leakage.
- **A4** — Opening navigation, resizing or changing breakpoints never remounts the editor or loses dirty content/selection; actual navigation preserves save/error/conflict protection and Cmd/Ctrl+S.
- **A5** — Files/Graph, metadata editing, Mermaid fallback, backlinks, supersession and error states remain accessible with the existing saving/rendering authority.

## Verification recipes

Covers **A1,A2,A3,A4,A5**. Start Vite from internal/serve/ui with bun run dev -- --host 127.0.0.1 --port 5195 --strictPort. Reuse an already-installed Playwright via TUSKER_PLAYWRIGHT_MODULE. These are fixture-browser checks; no real service mutation. Prefix shell commands with rtk as instructed by AGENTS.md. Assert first heading and scroll viewport boxes, 72ch computed rule, local overflow, editor value/selection and actual save requests; failure/conflict cases must exercise cancelled/retained navigation.

```sh
TUSKER_WORKSPACE_BASE_URL=http://127.0.0.1:5195 node internal/serve/ui/test/full-height-workspace.browser.mjs --case docs
```

Covers **A2,A3,A4,A5**. Supplemental current document regressions. Existing source-text assertions do not replace browser checks.

```sh
cd internal/serve/ui && bun test test/documents-polish.test.ts test/knowledge-wikilink.test.ts
```

## Required result

Write `docs/reports/full-height-workspace/docs/report.md` with acceptance ID, setup, expected/observed result, PASS/FAIL, exact command/case count and evidence links. Record unavailable live/installed surfaces separately. Submit through normal task lifecycle; implementation submission does not itself mean independently reviewed completion.
