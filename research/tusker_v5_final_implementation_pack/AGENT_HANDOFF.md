# Agent handoff

Read in order:

1. `SPEC/TUSKER_V5_FINAL_SPEC.md`
2. `SPEC/IMPLEMENTATION_SEQUENCE.md`
3. `SPEC/MIGRATION_PLAN.md`
4. `_config/docs-map.yaml`
5. The assigned task file.

Rules:

- Do not create per-task sidecar JSON.
- Do not put Markdoc syntax in task files.
- Do not invent domains or doc_nodes.
- Do not mark tasks done if doc_nodes are unresolved.
- Worker fills Evidence packet.
- Verifier fills Verification log.
- If docs are touched, update according to the target doc's mode.
- If video is attached, require transcript and companion metadata.
