# Docs Pages

Use this when durable project knowledge belongs in Tusker instead of a task body.

Durable docs are global markdown pages under `tusker/docs/**` with `schema: tusker.doc/v7 when supported, or the repository's installed docs schema`. They are addressed by `node` and routed through `_config/docs-map.yaml`.

```bash
tusker new doc --title "<Spec title>" \
  --node spec/<slug> \
  --kind canon \
  --audience developer \
  --domains schema
```

Connect executable work to docs with task `doc_nodes`. Keep implementation work in tasks; keep durable knowledge in docs pages.
