# Agent Feedback

- context: RUN-T-0010→0011 dependency chain stalled overnight because dependency satisfaction requires status=done, and done requires human acceptance for high risk; machine work was complete and gate-green hours earlier
- friction: Hard done-gate on dependencies serializes the pipeline on human review latency even when the operator's policy is batch review; reviewer had to close out-of-band to unblock
- product-idea: Soft dependencies: allow a dependency edge to unblock dispatch when the dep reaches review with proof_status=satisfied (machine-complete), while still enforcing close-order (child cannot close before parent is done); make edge hardness configurable per dependency, default hard for high/critical deps
- impact: Keeps the factory flowing overnight without weakening close policy; review stays a close-gate, not a dispatch-gate
- related: RUN-T-0010, RUN-T-0011
