# Agent Feedback

- context: authoring task contracts by hand-editing body sections
- friction: Hand-editing Acceptance/Verification into a freshly created task invalidates state_rev, so the next control command fails with CAS_CONFLICT until tusker reconcile is run manually.
- product-idea: Either auto-refresh state_rev on body-only edits during control ops, or add 'tusker new task --contract <file>' / editor flow so authoring does not require a repair step.
- impact: Every human- or agent-authored contract hits one dead roundtrip plus a confusing error before work can start.
- related: CLN-T-0001
