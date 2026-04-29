---
title: "Specs as D-notes"
description: "Deprecated guidance."
tusker:
  audience: "user"
  publish_path: "user/reference/specs-as-d-notes"
  publish_section_title: "Reference"
  route: "/user/reference/specs-as-d-notes/"
  source_kind: "repo_doc"
  source_path: "skill/references/SPECS_AS_D_NOTES.md"
  summary: "Deprecated guidance."
  tags:
    - "reference"
  updated: "2026-04-22"
---

# Specs as D-notes

Deprecated guidance.

Use [CANON_LOCATIONS.md](/user/reference/canon-locations/) instead.

The old rule "specs live as D-notes" was too absolute. Tusker now supports three canon patterns per epic:

- epic `## Design`
- canonical D-note
- repo file via `spec_source`

If you are creating a developer doc, declare intent explicitly:

```bash
tusker new-doc --epic <ACR> --title "<Spec title>" --audience developer --canon-for <ACR>
# or
tusker new-doc --epic <ACR> --title "<Companion doc>" --audience developer --companion-to <ACR-S-0001>
```
