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
