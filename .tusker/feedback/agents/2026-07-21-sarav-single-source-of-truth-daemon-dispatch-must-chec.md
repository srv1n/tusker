# Agent Feedback

- context: Daemon dispatched KNW-T-0001/0002 after interactive promote to ready, though project tusker.yaml has automation:false
- friction: Split-brain automation config: daemon registry enabled:true overrides project automation:false and dispatches anyway; operator never opted in
- product-idea: Single source of truth: daemon dispatch must check project-level automation setting; registry enable should mean poll/observe only
- impact: Unwanted dispatches to a broken codex runner produced failed runs and Needs-me noise on every ready task
- related: KNW-T-0001, KNW-T-0002, daemon registry, tusker.yaml
