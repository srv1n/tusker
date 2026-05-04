---
title: "Docs Pages"
description: "Use this when durable project knowledge belongs in Tusker instead of a task body."
tusker:
  audience: "user"
  publish_path: "user/reference/doc-pages"
  publish_section_title: "Reference"
  route: "/user/reference/doc-pages/"
  source_kind: "repo_doc"
  source_path: "skill/references/DOC_PAGES.md"
  summary: "Use this when durable project knowledge belongs in Tusker instead of a task body."
  tags:
    - "reference"
  updated: "2026-04-29"
---

# Docs Pages

Use this when durable project knowledge belongs in Tusker instead of a task body.

V5 docs are global markdown pages under `tusker/docs/**` with `schema: tusker.doc/v5`. They are addressed by `node` and routed through `_config/docs-map.yaml`.

```bash
tusker new doc --title "<Spec title>" \
  --node spec/<slug> \
  --kind canon \
  --audience developer \
  --domains schema
```

Connect executable work to docs with task `doc_nodes`. Keep implementation work in tasks; keep durable knowledge in docs pages.
