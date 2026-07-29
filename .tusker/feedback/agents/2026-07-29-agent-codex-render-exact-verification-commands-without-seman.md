# Agent Feedback

- context: Authoring and importing the execution-observability V2 delivery plan.
- friction: Delivery import escaped regex alternation pipes inside verification table cells, changing exact Go test semantics; the frozen scope then required a replacement scope. Imported high/critical tasks also omit the Knowledge delta section even though the V2 schema has no knowledge-delta field, so otherwise valid imports immediately warn.
- product-idea: Render exact verification commands without semantic escaping, round-trip them through preflight tests, and add a V2 knowledge-delta field or generate an explicit None expected section under a documented policy.
- impact: A post-import audit was required, eleven never-started tasks were superseded, a second disarmed wave was created, and the canonical tasks retain unavoidable validation warnings.
- related: docs/plans/24-execution-observability-v2.yaml, W-0006, W-0007, ORC-T-0066 through ORC-T-0076
- dedupe-key: delivery-v2-proof-roundtrip-knowledge-delta
