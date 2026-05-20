# Tusker v3 — task-first, docs-aware

This version makes four opinionated changes:

1. `story` becomes `task`.
2. The markdown file is the human/agent contract. Runtime noise moves out of front matter.
3. Documentation is routed by a controlled docs map, not by arbitrary tags.
4. A task is not complete until its docs impact is resolved or explicitly waived.

## Work hierarchy

```text
Epic
├── Task
├── Bug
└── Doc
```

- **Epic** = workstream, success metrics, canon declaration.
- **Task** = executable contract for a change.
- **Bug** = executable contract for defect repair.
- **Doc** = durable knowledge page worth keeping.

## Front matter rule

Keep only stable routing metadata in front matter.

Put volatile machine state in a sidecar file:

```text
.tusker/state/<ID>.json
```

That sidecar can hold:
- run status
- agent/tool info
- timestamps and transitions
- PR links
- evidence manifest
- attestation/signoff records
- retry counts
- daemon bookkeeping

Do **not** split the actual contract into two files. One markdown file should still hold the human-readable spec and the agent packet.

## Docs routing rule

Use two fields, not one:

- `domains`: broad browsing categories for humans
- `doc_nodes`: exact documentation pages or sections from `docs-map.yaml`

Example:

```yaml
domains: [memory, harness]
doc_nodes:
  - memory/overview
  - memory/retrieval/pipeline
```

That gives you:
- human grouping
- precise automation targets
- controlled vocabulary
- no tag soup
