# Agent Feedback

- context: TestArmedWaveCrashRestartConverges (e2e/crashrecovery) failed twice today with 'condition not met within 1m0s' yet passed for a reviewer at 12s on the same tree earlier
- friction: Timing-sensitive e2e flake: converge deadline too tight under load; unrelated diffs get blamed
- product-idea: Widen or poll-scale the convergence deadline, or gate the test on an isolated runner
- impact: Central gates go red on noise; operators learn to ignore red, which kills the gate's authority
- related: e2e/crashrecovery/crash_recovery_test.go TestArmedWaveCrashRestartConverges
