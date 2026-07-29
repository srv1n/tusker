---
schema: tusker.design-note/v1
kind: interface
status: proposed
authority: normative
date: 2026-07-28
parent: "[[00-index]]"
related:
  - "[[04-plan-and-authorization]]"
  - "[[09-api-and-state-contracts]]"
  - "[[11-responsive-accessibility-and-content]]"
tags:
  - tusker/ux
  - tusker/knowledge
  - obsidian
---

# Knowledge and editing

## Outcome

Knowledge is where the user understands and edits canonical intent, decisions,
specs, and durable operating truth. It is not a filesystem browser with a
prettier icon set.

## Knowledge index

### Default groups

1. **Canonical product knowledge**
2. **Specifications and decisions**
3. **Recently changed**

Filters:

- type;
- domain;
- status/authority;
- delivery;
- tag;
- orphaned/broken links.

Filesystem tree is available as a secondary “Browse files” mode.

### Knowledge row

- title;
- capsule/summary;
- kind and authority;
- related project outcome/delivery;
- changed time;
- backlink count;
- warnings only when actionable.

## Document reader

### Header

- title;
- capsule;
- status and authority;
- edit action when writable;
- overflow: open file, copy link, history, exact path.

### Body

- rendered Markdown;
- stable heading anchors;
- callouts preserved;
- wikilinks resolve inside Tusker;
- external links are identified;
- task/evidence records render as product objects when referenced.

### Context rail

1. **Backlinks** — documents that refer here, grouped by meaning.
2. **Related** — frontmatter relationships.
3. **Implemented by / delivered in** — when traceability exists.
4. **History** — collapsed.

The rail collapses below wide desktop.

## Obsidian-style links

- `[[01-product-model-and-information-architecture|Document title]]` illustrates
  a labeled canonical subject; ambiguous subjects require disambiguation.
- Aliases render user-friendly labels.
- Backlinks are computed, not manually duplicated.
- Broken links are visible to authors and Diagnostics, not shown as alarming
  runtime failures to readers.
- Hover/keyboard preview shows capsule, authority, and status.
- Renames require a link-impact preview and may update references atomically.

## Graph explorer

The graph is a secondary exploration tool, never primary navigation.

- default scope is the current document neighborhood;
- global graph requires an explicit action;
- nodes encode kind/authority, not workflow severity;
- filters for domain, kind, status, and delivery;
- selecting a node opens a preview before navigation;
- inaccessible/generated/runtime records are excluded by corpus policy.

## Editor

### Default mode

Split or toggle between Write and Preview. Preserve Markdown, frontmatter, and
wikilinks.

### Metadata

Common fields use structured controls:

- title;
- capsule;
- kind;
- status;
- authority;
- parent;
- related;
- tags.

Raw frontmatter is available in Advanced. Unknown fields are preserved.

### Save contract

- optimistic editing against a known revision;
- save sends the base revision;
- success returns a fresh rendered document and warnings;
- validation defects are inline at the field/body;
- disk conflict never overwrites silently.

### Conflict resolution

Show:

- “Changed on disk while you were editing”;
- your draft;
- current disk version;
- structured metadata differences;
- options: Reload, Copy my draft, Compare, or manually merge.

No one-click overwrite unless an explicit advanced policy permits it.

## Knowledge from deliveries

Delivery detail lists changed canonical documents. A delivery is not complete
when its contract requires durable knowledge and that update is absent.

Plan review links governing specs and decisions. It never embeds an untracked
copy that can drift.

## Corpus boundaries

Default corpus includes reviewed product/engineering Markdown selected by
project policy. Exclude:

- `.tusker/events`;
- generated projections;
- attempts and raw logs;
- scratch;
- transient evidence unless explicitly promoted;
- secrets and credential files;
- vendored/build output.

## Empty, stale, and error states

| State | Behavior |
|---|---|
| No corpus | Explain configured roots; offer Settings |
| Indexing | Preserve last index; show changed count |
| Broken link | Inline author warning; suggested matches |
| Ambiguous link | Disambiguation list |
| Read-only document | Explain source/policy |
| Save conflict | Dedicated compare flow |
| Invalid frontmatter | Render body if safe; surface defects to author |
| Offline | Cached reader works; editing disabled |

## APIs

Current document and graph endpoints are cataloged in
[[09-api-and-state-contracts]]. Target additions should support search,
backlinks, revisions, rename impact, and validation without exposing arbitrary
filesystem access.

## Acceptance

- Users navigate by meaning before path.
- Every document exposes backlinks and authority.
- Editing preserves unknown metadata and refuses stale overwrite.
- Runtime/log stores do not pollute canonical knowledge.
- The graph remains an optional exploration surface.
- Delivery and planning link to canonical knowledge rather than copied prose.
