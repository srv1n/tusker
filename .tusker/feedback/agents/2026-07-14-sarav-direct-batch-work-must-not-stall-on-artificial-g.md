# Agent Feedback

- context: Cross-project agent sessions where the operator explicitly names tickets or asks for unattended batch execution.
- friction: Direct sessions confuse task schedulability with permission to act; risk categories create human acceptance waits for objective engineering work; batch output can lack ticket-level attribution and proof.
- product-idea: Separate direct-session authorization from daemon eligibility; reserve human stops for missing capability, external authority, or unresolved intent; allow independent reviewers to close all risk tiers unless an explicit authority gate exists; make wave draining persist across ticket boundaries with one patch and focused proof per ticket plus broad validation at the wave boundary.
- impact: The operator repeats authorization, review latency blocks downstream work, overnight throughput collapses, and morning output is hard to validate or land safely.
- related: DEL, docs/specs/11-spec-to-wave-autonomous-delivery.md, LIF-T-0013, AGX-T-0007, RUN-T-0016, OPS-T-0001, OPS-T-0002, OPS-T-0003
- dedupe-key: direct-batch-artificial-gates
